package session

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/open-ma/oma-building/internal/stream"
)

// Registry runs session turns asynchronously.
type Registry struct {
	mu       sync.Mutex
	machines map[string]*Machine
}

// NewRegistry returns an empty session registry.
func NewRegistry() *Registry {
	return &Registry{machines: make(map[string]*Machine)}
}

// Register stores a machine for a session id.
func (r *Registry) Register(sessionID string, machine *Machine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.machines[sessionID] = machine
}

// EnqueueUserMessage appends the user event and runs the turn in background.
func (r *Registry) EnqueueUserMessage(
	ctx context.Context,
	sessionID string,
	userEvent json.RawMessage,
	onDone func(error),
) error {
	r.mu.Lock()
	machine, ok := r.machines[sessionID]
	r.mu.Unlock()
	if !ok {
		return ErrNotRegistered
	}

	stored, err := machine.Events.AppendEvents(ctx, sessionID, []json.RawMessage{userEvent})
	if err != nil {
		return err
	}
	for _, ev := range stored {
		machine.Hub.Publish(sessionID, stream.Event{
			Seq:     ev.Seq,
			Payload: ev.Payload,
		})
	}

	go func() {
		err := machine.RunTurn(context.Background())
		if onDone != nil {
			onDone(err)
		}
	}()
	return nil
}

// ErrNotRegistered means the session has no registered machine.
var ErrNotRegistered = errNotRegistered{}

type errNotRegistered struct{}

func (errNotRegistered) Error() string { return "session not registered" }
