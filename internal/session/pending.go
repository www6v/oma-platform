package session

import (
	"context"
	"encoding/json"
	"time"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
)

const defaultThreadID = "sthr_primary"

// IsPendingQueueEventType reports AMA queue-input client events.
func IsPendingQueueEventType(eventType string) bool {
	switch eventType {
	case "user.message", "user.tool_confirmation", "user.custom_tool_result":
		return true
	default:
		return false
	}
}

func threadIDFromPayload(payload json.RawMessage) string {
	var meta struct {
		SessionThreadID string `json:"session_thread_id"`
	}
	_ = json.Unmarshal(payload, &meta)
	if meta.SessionThreadID == "" {
		return defaultThreadID
	}
	return meta.SessionThreadID
}

func (m *Machine) broadcastSystemEvent(payload json.RawMessage) {
	if m.Hub == nil {
		return
	}
	m.Hub.Publish(m.SessionID, stream.Event{Payload: payload})
}

func (m *Machine) broadcastPendingFrame(row *store.PendingRow) {
	frame, err := json.Marshal(map[string]any{
		"type":                "system.user_message_pending",
		"event_id":            row.EventID,
		"pending_seq":         row.PendingSeq,
		"enqueued_at":         row.EnqueuedAt,
		"session_thread_id":   row.SessionThreadID,
		"event":               json.RawMessage(row.Data),
	})
	if err != nil {
		return
	}
	m.broadcastSystemEvent(frame)
}

func (m *Machine) broadcastPromotedFrame(
	row *store.PendingRow,
	promotedSeq int,
) {
	processedAt := time.Now().UTC().Format(time.RFC3339Nano)
	frame, err := json.Marshal(map[string]any{
		"type":              "system.user_message_promoted",
		"event_id":          row.EventID,
		"pending_seq":       row.PendingSeq,
		"seq":               promotedSeq,
		"processed_at":      processedAt,
		"session_thread_id": row.SessionThreadID,
	})
	if err != nil {
		return
	}
	m.broadcastSystemEvent(frame)
}

func (m *Machine) broadcastCancelledFrame(row *store.PendingRow) {
	frame, err := json.Marshal(map[string]any{
		"type":              "system.user_message_cancelled",
		"event_id":          row.EventID,
		"pending_seq":       row.PendingSeq,
		"session_thread_id": row.SessionThreadID,
	})
	if err != nil {
		return
	}
	m.broadcastSystemEvent(frame)
}

// EnqueuePending stores a queue-input event and emits pending SSE frame.
func (m *Machine) EnqueuePending(
	ctx context.Context,
	payload json.RawMessage,
) (*store.PendingRow, error) {
	if m.Pending == nil {
		return nil, errPendingUnavailable{}
	}
	threadID := threadIDFromPayload(payload)
	row, err := m.Pending.Enqueue(ctx, m.SessionID, threadID, payload)
	if err != nil {
		return nil, err
	}
	m.broadcastPendingFrame(row)
	return row, nil
}

// CancelPendingQueue marks queued rows cancelled and broadcasts frames.
func (m *Machine) CancelPendingQueue(
	ctx context.Context,
	threadID string,
) ([]store.PendingRow, error) {
	if m.Pending == nil {
		return nil, nil
	}
	now := time.Now().UnixMilli()
	rows, err := m.Pending.CancelAllForThread(
		ctx, m.SessionID, threadID, now,
	)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		m.broadcastCancelledFrame(&rows[i])
	}
	return rows, nil
}

// PromoteOnePending moves the next pending row into session_events.
func (m *Machine) PromoteOnePending(
	ctx context.Context,
	threadID string,
) (bool, error) {
	if m.Pending == nil {
		return false, errPendingUnavailable{}
	}
	if threadID == "" {
		threadID = defaultThreadID
	}

	if m.appendLocker != nil {
		m.appendLocker.Lock()
		defer m.appendLocker.Unlock()
	}

	row, err := m.Pending.Peek(ctx, m.SessionID, threadID)
	if err != nil {
		return false, err
	}
	if row == nil {
		return false, nil
	}

	stored, err := m.Events.AppendEvents(ctx, m.SessionID, []json.RawMessage{row.Data})
	if err != nil {
		return false, err
	}
	if err := m.Pending.Delete(ctx, m.SessionID, row.PendingSeq); err != nil {
		return false, err
	}

	promotedSeq := 0
	if len(stored) > 0 {
		promotedSeq = stored[0].Seq
		for _, ev := range stored {
			m.Hub.Publish(m.SessionID, stream.Event{
				Seq:     ev.Seq,
				Payload: ev.Payload,
			})
		}
	}
	m.broadcastPromotedFrame(row, promotedSeq)
	return true, nil
}

type errPendingUnavailable struct{}

func (errPendingUnavailable) Error() string {
	return "pending queue unavailable"
}
