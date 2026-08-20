package session

import (
	"context"
	"encoding/json"

	"github.com/open-ma/oma-building/internal/stream"
)

// EnqueueEvents appends client events and optionally runs a harness turn.
// Queue-input user.* events land in pending_events first; drain promotes
// them into session_events immediately before each harness turn.
func (r *Registry) EnqueueEvents(
	ctx context.Context,
	sessionID string,
	events []json.RawMessage,
	runTurn bool,
	handleInterrupt bool,
	onDone func(error),
) error {
	lane, err := r.lane(sessionID)
	if err != nil {
		return err
	}

	var pendingEvents []json.RawMessage
	var directEvents []json.RawMessage
	interruptThread := defaultThreadID

	for _, ev := range events {
		var meta struct {
			Type            string `json:"type"`
			SessionThreadID string `json:"session_thread_id"`
		}
		if err := json.Unmarshal(ev, &meta); err != nil {
			return err
		}
		if meta.Type == "user.interrupt" {
			if meta.SessionThreadID != "" {
				interruptThread = meta.SessionThreadID
			}
			directEvents = append(directEvents, ev)
			continue
		}
		if IsPendingQueueEventType(meta.Type) {
			pendingEvents = append(pendingEvents, ev)
			continue
		}
		directEvents = append(directEvents, ev)
	}

	cancelledCount := 0
	if handleInterrupt {
		cancelled, err := lane.machine.CancelPendingQueue(ctx, interruptThread)
		if err != nil {
			return err
		}
		cancelledCount = len(cancelled)
	}

	for _, ev := range pendingEvents {
		if _, err := lane.machine.EnqueuePending(ctx, ev); err != nil {
			return err
		}
	}

	if len(directEvents) > 0 {
		prepared := make([]json.RawMessage, 0, len(directEvents))
		for _, ev := range directEvents {
			var meta struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(ev, &meta); err != nil {
				return err
			}
			if meta.Type == "user.define_outcome" {
				echoed, err := PrepareDefineOutcome(ev)
				if err != nil {
					return err
				}
				if err := lane.machine.ActivateOutcomeFromEvent(ctx, echoed); err != nil {
					return err
				}
				ev = echoed
			}
			prepared = append(prepared, ev)
		}
		directEvents = prepared
		lane.appendMu.Lock()
		stored, err := lane.machine.Events.AppendEvents(
			ctx, sessionID, directEvents,
		)
		if err != nil {
			lane.appendMu.Unlock()
			return err
		}
		for _, ev := range stored {
			lane.machine.Hub.Publish(sessionID, stream.Event{
				Seq:     ev.Seq,
				Payload: ev.Payload,
			})
		}
		lane.appendMu.Unlock()
	}

	if handleInterrupt {
		lane.handleInterrupt(ctx, cancelledCount > 0)
		return nil
	}

	// Determine the thread ID: pending events take priority, otherwise
	// fall back to default for direct-only flows.
	threadID := defaultThreadID
	if len(pendingEvents) > 0 {
		threadID = threadIDFromPendingEvents(pendingEvents)
	}

	if runTurn {
		// When idle, promote synchronously so queue-input events (including
		// schedule wakeups fired from the background worker) appear in the
		// session log before the async turn worker runs.
		if !lane.machine.IsTurnActive() {
			if len(pendingEvents) > 0 {
				if _, err := lane.promoteAllPending(ctx, threadID); err != nil {
					return err
				}
			}
		}
		lane.scheduleTurn(threadID, onDone)
		return nil
	}
	if len(pendingEvents) > 0 {
		lane.schedulePromote(threadID, onDone)
	}
	return nil
}

func threadIDFromPendingEvents(events []json.RawMessage) string {
	for _, ev := range events {
		tid := threadIDFromPayload(ev)
		if tid != "" {
			return tid
		}
	}
	return defaultThreadID
}
