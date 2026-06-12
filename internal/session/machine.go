package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

const turnAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// Broadcaster publishes persisted events to live subscribers.
type Broadcaster interface {
	Publish(sessionID string, ev stream.Event)
}

// Machine drives one harness turn for a session.
type Machine struct {
	TenantID    string
	SessionID   string
	Sessions    *store.SessionRepo
	Events      *store.EventRepo
	Pending     *store.PendingRepo
	Hub         Broadcaster
	Workdirs    *workdir.Manager
	Harness      harness.Client
	Models       *modelresolve.Resolver
	McpProxyBase string
	McpProxyAPIKey string
	appendLocker sync.Locker
	activeTurn   string
	activeTurnM  sync.Mutex
	cancelTurn   context.CancelFunc
	cancelTurnM  sync.Mutex
}

// SetAppendLocker serializes event appends with EnqueueEvents (per-session).
func (m *Machine) SetAppendLocker(locker sync.Locker) {
	m.appendLocker = locker
}

// RunTurn executes a harness turn using persisted session history.
func (m *Machine) RunTurn(ctx context.Context) error {
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	m.activeTurnM.Lock()
	if m.activeTurn != "" {
		m.activeTurnM.Unlock()
		return fmt.Errorf("turn already active")
	}
	turnID := randomTurnID()
	m.activeTurn = turnID
	m.setCancelTurn(cancel)
	m.activeTurnM.Unlock()

	defer func() {
		m.activeTurnM.Lock()
		m.activeTurn = ""
		m.clearCancelTurn()
		m.activeTurnM.Unlock()
	}()

	if err := m.Sessions.BeginTurn(ctx, m.TenantID, m.SessionID, turnID); err != nil {
		return err
	}
	defer func() {
		_ = m.Sessions.EndTurn(context.Background(), m.TenantID, m.SessionID, turnID)
	}()

	sess, err := m.Sessions.Get(ctx, m.TenantID, m.SessionID)
	if err != nil || sess == nil {
		return store.ErrNotFound
	}

	workdirPath, err := m.Workdirs.Ensure(ctx, m.TenantID, m.SessionID)
	if err != nil {
		return err
	}

	history, err := m.Events.ListEvents(ctx, m.SessionID, 0, 10000, true)
	if err != nil {
		return err
	}
	eventPayloads := make([]json.RawMessage, 0, len(history))
	for _, ev := range history {
		eventPayloads = append(eventPayloads, ev.Payload)
	}

	var agent harness.AgentSnapshot
	if err := json.Unmarshal(sess.AgentSnapshot, &agent); err != nil {
		return fmt.Errorf("parse agent snapshot: %w", err)
	}

	modelCfg, err := m.resolveModel(ctx, agent.Model)
	if err != nil {
		return m.failTurn(ctx, turnID, err)
	}

	var auxCfg *harness.ModelConfig
	if agent.AuxModel != "" {
		cfg, auxErr := m.resolveModel(ctx, agent.AuxModel)
		if auxErr == nil {
			auxCfg = &cfg
		}
	}

	envSnap := sess.EnvironmentSnapshot
	if len(envSnap) == 0 {
		envSnap = json.RawMessage(`{}`)
	}

	lifecycleStart, err := json.Marshal(map[string]any{
		"type":    "session.lifecycle",
		"phase":   "turn_start",
		"turn_id": turnID,
	})
	if err != nil {
		return err
	}
	if err := m.publishEvents(ctx, []json.RawMessage{lifecycleStart}); err != nil {
		return err
	}

	streamErr := harness.RunTurnStreaming(
		turnCtx,
		m.Harness,
		harness.TurnRequest{
			SessionID:      m.SessionID,
			TenantID:       m.TenantID,
			Agent:          agent,
			Model:          modelCfg,
			AuxModel:       auxCfg,
			Environment:    envSnap,
			Events:         eventPayloads,
			Workdir:        workdirPath,
			McpProxyBase:   m.McpProxyBase,
			McpProxyAPIKey: m.McpProxyAPIKey,
		},
		func(ev json.RawMessage) error {
			return m.publishEvents(ctx, []json.RawMessage{ev})
		},
	)
	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) {
			return m.finishInterruptedTurn(ctx, turnID)
		}
		return m.failTurn(ctx, turnID, streamErr)
	}

	lifecycleEnd, err := json.Marshal(map[string]any{
		"type":    "session.lifecycle",
		"phase":   "turn_end",
		"turn_id": turnID,
	})
	if err != nil {
		return err
	}
	return m.publishEvents(ctx, []json.RawMessage{lifecycleEnd})
}

// CancelActiveTurn aborts the in-flight harness turn, if any.
func (m *Machine) CancelActiveTurn() bool {
	m.cancelTurnM.Lock()
	cancel := m.cancelTurn
	m.cancelTurnM.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// PublishStatusIdle appends a session.status_idle marker (OMA-aligned).
func (m *Machine) PublishStatusIdle(ctx context.Context) error {
	idleEvent, err := json.Marshal(map[string]any{
		"type":        "session.status_idle",
		"stop_reason": map[string]string{"type": "end_turn"},
	})
	if err != nil {
		return err
	}
	return m.publishEvents(ctx, []json.RawMessage{idleEvent})
}

func (m *Machine) setCancelTurn(cancel context.CancelFunc) {
	m.cancelTurnM.Lock()
	m.cancelTurn = cancel
	m.cancelTurnM.Unlock()
}

func (m *Machine) clearCancelTurn() {
	m.cancelTurnM.Lock()
	m.cancelTurn = nil
	m.cancelTurnM.Unlock()
}

func (m *Machine) finishInterruptedTurn(
	ctx context.Context,
	turnID string,
) error {
	lifecycleEnd, err := json.Marshal(map[string]any{
		"type":    "session.lifecycle",
		"phase":   "turn_end",
		"turn_id": turnID,
	})
	if err != nil {
		return err
	}
	return m.publishEvents(ctx, []json.RawMessage{lifecycleEnd})
}

func (m *Machine) failTurn(
	ctx context.Context,
	turnID string,
	cause error,
) error {
	errEvent, err := json.Marshal(map[string]any{
		"type":    "session.error",
		"error":   "harness_turn_failed",
		"message": cause.Error(),
		"turn_id": turnID,
	})
	if err != nil {
		return cause
	}
	lifecycleEnd, err := json.Marshal(map[string]any{
		"type":    "session.lifecycle",
		"phase":   "turn_end",
		"turn_id": turnID,
	})
	if err != nil {
		return cause
	}
	if pubErr := m.publishEvents(ctx, []json.RawMessage{errEvent, lifecycleEnd}); pubErr != nil {
		return cause
	}
	return nil
}

func (m *Machine) publishEvents(
	ctx context.Context,
	events []json.RawMessage,
) error {
	if len(events) == 0 {
		return nil
	}
	if m.appendLocker != nil {
		m.appendLocker.Lock()
		defer m.appendLocker.Unlock()
	}
	stored, err := m.Events.AppendEvents(ctx, m.SessionID, events)
	if err != nil {
		return err
	}
	for _, ev := range stored {
		m.Hub.Publish(m.SessionID, stream.Event{
			Seq:     ev.Seq,
			Payload: ev.Payload,
		})
	}
	return nil
}

func (m *Machine) resolveModel(
	ctx context.Context,
	agentModel string,
) (harness.ModelConfig, error) {
	if m.Models == nil {
		return harness.ModelConfig{Model: agentModel}, nil
	}
	return m.Models.Resolve(ctx, m.TenantID, agentModel)
}

func randomTurnID() string {
	out := make([]byte, 16)
	max := big.NewInt(int64(len(turnAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		out[i] = turnAlphabet[idx.Int64()]
	}
	return string(out)
}
