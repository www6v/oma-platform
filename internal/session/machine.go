package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/open-ma/oma-building/internal/harness"
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
	Hub         Broadcaster
	Workdirs    *workdir.Manager
	Harness     harness.Client
	activeTurn  string
	activeTurnM sync.Mutex
}

// RunTurn executes a harness turn using persisted session history.
func (m *Machine) RunTurn(ctx context.Context) error {
	m.activeTurnM.Lock()
	if m.activeTurn != "" {
		m.activeTurnM.Unlock()
		return fmt.Errorf("turn already active")
	}
	turnID := randomTurnID()
	m.activeTurn = turnID
	m.activeTurnM.Unlock()

	defer func() {
		m.activeTurnM.Lock()
		m.activeTurn = ""
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

	workdirPath, err := m.Workdirs.Ensure(ctx, m.SessionID)
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

	resp, err := m.Harness.RunTurn(ctx, harness.TurnRequest{
		SessionID: m.SessionID,
		Agent:     agent,
		Events:    eventPayloads,
		Workdir:   workdirPath,
	})
	if err != nil {
		return err
	}

	lifecycleStart, _ := json.Marshal(map[string]any{
		"type":    "session.lifecycle",
		"phase":   "turn_start",
		"turn_id": turnID,
	})
	lifecycleEnd, _ := json.Marshal(map[string]any{
		"type":    "session.lifecycle",
		"phase":   "turn_end",
		"turn_id": turnID,
	})

	outEvents := append(
		[]json.RawMessage{lifecycleStart},
		resp.Events...,
	)
	outEvents = append(outEvents, lifecycleEnd)

	stored, err := m.Events.AppendEvents(ctx, m.SessionID, outEvents)
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
