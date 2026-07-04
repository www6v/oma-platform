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
	Agents      *store.AgentRepo
	Teams       *store.TeamRepo
	Events      *store.EventRepo
	Pending     *store.PendingRepo
	Hub         Broadcaster
	Workdirs    *workdir.Manager
	Harness          harness.Client
	OutcomeEvaluator harness.OutcomeEvaluator
	Models           *modelresolve.Resolver
	Resources    *harness.ResourceResolver
	McpProxyBase        string
	McpProxyAPIKey      string
	PlatformBase        string
	InternalSecret      string
	OutboundProxyAddr   string
	OutboundProxyAPIKey string
	DatabasePath        string
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

// syncDeps copies handler-owned dependencies without resetting turn state.
func (m *Machine) syncDeps(src *Machine) {
	if src == nil {
		return
	}
	m.TenantID = src.TenantID
	m.SessionID = src.SessionID
	m.Sessions = src.Sessions
	m.Agents = src.Agents
	m.Teams = src.Teams
	m.Events = src.Events
	m.Pending = src.Pending
	m.Hub = src.Hub
	m.Workdirs = src.Workdirs
	m.Harness = src.Harness
	m.OutcomeEvaluator = src.OutcomeEvaluator
	m.Models = src.Models
	m.Resources = src.Resources
	m.McpProxyBase = src.McpProxyBase
	m.McpProxyAPIKey = src.McpProxyAPIKey
	m.PlatformBase = src.PlatformBase
	m.InternalSecret = src.InternalSecret
	m.OutboundProxyAddr = src.OutboundProxyAddr
	m.OutboundProxyAPIKey = src.OutboundProxyAPIKey
	m.DatabasePath = src.DatabasePath
}

// IsTurnActive reports whether a harness turn is currently running.
func (m *Machine) IsTurnActive() bool {
	m.activeTurnM.Lock()
	defer m.activeTurnM.Unlock()
	return m.activeTurn != ""
}

// RunTurn executes a harness turn using persisted session history.
// threadID selects the session thread (default sthr_primary).
func (m *Machine) RunTurn(ctx context.Context, threadID string) error {
	if threadID == "" {
		threadID = defaultThreadID
	}
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

	if err := m.runSingleHarnessTurn(turnCtx, threadID); err != nil {
		return err
	}
	if err := m.maybeRunOutcomeSupervisor(turnCtx, threadID); err != nil {
		return err
	}
	return m.publishStatusIdle(ctx, nil)
}

func (m *Machine) runSingleHarnessTurn(ctx context.Context, threadID string) error {
	if threadID == "" {
		threadID = defaultThreadID
	}
	turnID := randomTurnID()

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

	agent, err := m.resolveAgentForThread(ctx, sess, threadID)
	if err != nil {
		return m.failTurn(ctx, turnID, err)
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

	var resources []json.RawMessage
	if m.Resources != nil {
		resolved, resErr := m.Resources.ResolveForTurn(
			ctx, m.TenantID, envSnap, sess.Resources,
		)
		if resErr == nil {
			resources = resolved
		}
	}

	var subAgents map[string]harness.AgentSnapshot
	if threadID == defaultThreadID {
		subAgents, err = harness.ResolveSubAgents(
			ctx, m.TenantID, agent, m.Agents,
		)
		if err != nil {
			return m.failTurn(ctx, turnID, err)
		}
	}

	lifecycleStart, err := json.Marshal(map[string]any{
		"type":    "session.lifecycle",
		"phase":   "turn_start",
		"turn_id": turnID,
	})
	if err != nil {
		return err
	}
	runningEvent, err := json.Marshal(map[string]any{
		"type": "session.status_running",
	})
	if err != nil {
		return err
	}
	if err := m.publishEvents(ctx, []json.RawMessage{lifecycleStart, runningEvent}); err != nil {
		return err
	}

	onEvent, turnEvents := turnEventCollector(func(ev json.RawMessage) error {
		return m.publishEvents(ctx, []json.RawMessage{ev})
	})

	streamErr := harness.RunTurnStreaming(
		ctx,
		m.Harness,
		harness.TurnRequest{
			SessionID:           m.SessionID,
			SessionThreadID:   threadID,
			TenantID:          m.TenantID,
			Agent:             agent,
			SubAgents:         subAgents,
			Model:             modelCfg,
			AuxModel:          auxCfg,
			Environment:       envSnap,
			Resources:         resources,
			Events:            eventPayloads,
			Workdir:           workdirPath,
			McpProxyBase:      m.McpProxyBase,
			McpProxyAPIKey:    m.McpProxyAPIKey,
			PlatformBase:      m.PlatformBase,
			InternalSecret:    m.InternalSecret,
			OutboundProxyAddr: m.OutboundProxyAddr,
			OutboundProxyAPIKey: m.OutboundProxyAPIKey,
			DatabasePath:      m.DatabasePath,
		},
		onEvent,
	)
	if m.Workdirs != nil {
		if syncErr := m.Workdirs.SyncSessionOutputs(
			workdirPath, m.TenantID, m.SessionID,
		); syncErr != nil {
			_ = syncErr
		}
	}
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
	if err := m.publishEvents(ctx, []json.RawMessage{lifecycleEnd}); err != nil {
		return err
	}
	_ = turnEvents
	return nil
}

// turnEventCollector tracks events emitted during a harness stream.
func turnEventCollector(
	onEvent harness.EventHandler,
) (harness.EventHandler, *[]json.RawMessage) {
	turnEvents := make([]json.RawMessage, 0, 32)
	handler := func(ev json.RawMessage) error {
		turnEvents = append(turnEvents, append(json.RawMessage(nil), ev...))
		if onEvent == nil {
			return nil
		}
		return onEvent(ev)
	}
	return handler, &turnEvents
}

// PublishStatusIdle appends session.status_idle with stop_reason derived from
// pending custom tools across the full session history.
func (m *Machine) PublishStatusIdle(ctx context.Context) error {
	return m.publishStatusIdle(ctx, nil)
}

func (m *Machine) publishStatusIdle(
	ctx context.Context,
	turnEvents []json.RawMessage,
) error {
	eventPayloads := turnEvents
	if m.Events != nil {
		history, err := m.Events.ListEvents(ctx, m.SessionID, 0, 10000, true)
		if err != nil {
			return err
		}
		eventPayloads = make([]json.RawMessage, 0, len(history))
		for _, ev := range history {
			eventPayloads = append(eventPayloads, ev.Payload)
		}
	}
	stopReason := harness.BuildIdleStopReason(
		harness.PendingCustomToolIDs(eventPayloads),
	)
	if err := m.syncPendingToolCallsMetadata(ctx, eventPayloads, stopReason); err != nil {
		return err
	}
	idleEvent, err := json.Marshal(map[string]any{
		"type":        "session.status_idle",
		"stop_reason": stopReason,
	})
	if err != nil {
		return err
	}
	return m.publishEvents(ctx, []json.RawMessage{idleEvent})
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

func (m *Machine) resolveAgentForThread(
	ctx context.Context,
	sess *store.Session,
	threadID string,
) (harness.AgentSnapshot, error) {
	if threadID == "" || threadID == defaultThreadID {
		return harness.AgentSnapshotFromRaw(sess.AgentSnapshot)
	}
	if m.Teams == nil || m.Agents == nil {
		return harness.AgentSnapshot{}, fmt.Errorf(
			"teammate thread %s: team lookup unavailable", threadID,
		)
	}
	member, err := m.Teams.GetMemberByThreadID(
		ctx, m.SessionID, threadID,
	)
	if err != nil {
		return harness.AgentSnapshot{}, err
	}
	if member == nil {
		return harness.AgentSnapshot{}, fmt.Errorf(
			"teammate thread %s: member not found", threadID,
		)
	}
	agent, err := m.Agents.Get(ctx, m.TenantID, member.AgentID)
	if err != nil {
		return harness.AgentSnapshot{}, err
	}
	if agent == nil {
		return harness.AgentSnapshot{}, fmt.Errorf(
			"teammate agent %q not found", member.AgentID,
		)
	}
	return harness.AgentSnapshotFromConfig(agent.AgentConfig), nil
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
