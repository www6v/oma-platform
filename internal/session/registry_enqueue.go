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

	if len(pendingEvents) == 0 {
		return nil
	}
	if runTurn {
		lane.scheduleTurn(onDone)
		return nil
	}
	lane.schedulePromote(onDone)
	return nil
}
