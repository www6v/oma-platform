package session

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/open-ma/oma-building/internal/stream"
)

// Registry runs session turns asynchronously with per-session serialization.
type Registry struct {
	mu    sync.Mutex
	lanes map[string]*sessionLane
}

// NewRegistry returns an empty session registry.
func NewRegistry() *Registry {
	return &Registry{lanes: make(map[string]*sessionLane)}
}

// Remove tears down the in-memory session lane (best-effort).
func (r *Registry) Remove(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lane, ok := r.lanes[sessionID]
	if !ok {
		return
	}
	if lane.machine != nil {
		lane.machine.CancelActiveTurn()
	}
	delete(r.lanes, sessionID)
}

// Register stores a machine for a session id and starts its turn worker.
func (r *Registry) Register(sessionID string, machine *Machine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lane, ok := r.lanes[sessionID]; ok {
		lane.machine = machine
		return
	}
	r.lanes[sessionID] = newSessionLane(machine)
}

// EnqueueUserMessage appends the user event and runs the turn in background.
func (r *Registry) EnqueueUserMessage(
	ctx context.Context,
	sessionID string,
	userEvent json.RawMessage,
	onDone func(error),
) error {
	return r.EnqueueEvents(
		ctx, sessionID, []json.RawMessage{userEvent}, true, false, onDone,
	)
}

// EnqueueEvents appends client events and optionally runs a harness turn.
// When handleInterrupt is true, cancels any active turn, drains queued turns,
// and emits session.status_idle when state changed (OMA user.interrupt).
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

	lane.appendMu.Lock()
	stored, err := lane.machine.Events.AppendEvents(ctx, sessionID, events)
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

	if handleInterrupt {
		lane.handleInterrupt(ctx)
		runTurn = false
	}

	if !runTurn {
		return nil
	}

	lane.scheduleTurn(onDone)
	return nil
}

func (r *Registry) lane(sessionID string) (*sessionLane, error) {
	r.mu.Lock()
	lane, ok := r.lanes[sessionID]
	r.mu.Unlock()
	if !ok {
		return nil, ErrNotRegistered
	}
	return lane, nil
}

type sessionLane struct {
	machine  *Machine
	appendMu sync.Mutex
	turnCh   chan turnJob
}

type turnJob struct {
	onDone func(error)
}

func newSessionLane(machine *Machine) *sessionLane {
	lane := &sessionLane{
		machine: machine,
		turnCh:  make(chan turnJob, 32),
	}
	go lane.runTurnWorker()
	return lane
}

func (lane *sessionLane) scheduleTurn(onDone func(error)) {
	lane.turnCh <- turnJob{onDone: onDone}
}

func (lane *sessionLane) handleInterrupt(ctx context.Context) {
	hadActive := lane.machine.CancelActiveTurn()
	drained := lane.drainPendingTurns()
	if !hadActive && drained == 0 {
		return
	}
	_ = lane.machine.PublishStatusIdle(ctx)
}

func (lane *sessionLane) drainPendingTurns() int {
	n := 0
	for {
		select {
		case job := <-lane.turnCh:
			n++
			if job.onDone != nil {
				job.onDone(nil)
			}
		default:
			return n
		}
	}
}

func (lane *sessionLane) runTurnWorker() {
	for job := range lane.turnCh {
		err := lane.machine.RunTurn(context.Background())
		if job.onDone != nil {
			job.onDone(err)
		}
	}
}

// ErrNotRegistered means the session has no registered machine.
var ErrNotRegistered = errNotRegistered{}

type errNotRegistered struct{}

func (errNotRegistered) Error() string { return "session not registered" }
