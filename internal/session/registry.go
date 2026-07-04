package session

import (
	"context"
	"encoding/json"
	"sync"
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
	lane, ok := r.lanes[sessionID]
	if ok {
		delete(r.lanes, sessionID)
	}
	r.mu.Unlock()
	if !ok {
		return
	}
	lane.shutdown()
}

// Shutdown stops all session turn workers and waits for them to exit.
func (r *Registry) Shutdown() {
	r.mu.Lock()
	lanes := make([]*sessionLane, 0, len(r.lanes))
	for _, lane := range r.lanes {
		lanes = append(lanes, lane)
	}
	r.lanes = make(map[string]*sessionLane)
	r.mu.Unlock()

	for _, lane := range lanes {
		lane.shutdown()
	}
}

func (lane *sessionLane) shutdown() {
	if lane.machine != nil {
		lane.machine.CancelActiveTurn()
	}
	close(lane.turnCh)
	lane.wg.Wait()
}

// Register stores a machine for a session id and starts its turn worker.
// Re-registering the same session refreshes dependencies on the live
// Machine without swapping the instance, so in-flight turn state is kept.
func (r *Registry) Register(sessionID string, machine *Machine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lane, ok := r.lanes[sessionID]; ok {
		lane.machine.syncDeps(machine)
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
	wg       sync.WaitGroup
}

type turnJob struct {
	threadID    string
	onDone      func(error)
	promoteOnly bool
}

func newSessionLane(machine *Machine) *sessionLane {
	lane := &sessionLane{
		machine: machine,
		turnCh:  make(chan turnJob, 32),
	}
	machine.SetAppendLocker(&lane.appendMu)
	lane.wg.Add(1)
	go func() {
		defer lane.wg.Done()
		lane.runTurnWorker()
	}()
	return lane
}

func (lane *sessionLane) scheduleTurn(threadID string, onDone func(error)) {
	if threadID == "" {
		threadID = defaultThreadID
	}
	lane.turnCh <- turnJob{threadID: threadID, onDone: onDone}
}

func (lane *sessionLane) schedulePromote(threadID string, onDone func(error)) {
	if threadID == "" {
		threadID = defaultThreadID
	}
	lane.turnCh <- turnJob{
		threadID:    threadID,
		onDone:      onDone,
		promoteOnly: true,
	}
}

func (lane *sessionLane) handleInterrupt(
	ctx context.Context,
	hadCancelledPending bool,
) {
	hadActive := lane.machine.CancelActiveTurn()
	drained := lane.drainPendingTurns()
	if !hadActive && drained == 0 && !hadCancelledPending {
		lane.machine.RecoverStuckRunningOnInterrupt(ctx)
		return
	}
	_ = lane.machine.PublishInterruptIdle(ctx)
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
		threadID := job.threadID
		if threadID == "" {
			threadID = defaultThreadID
		}
		var err error
		if job.promoteOnly {
			_, err = lane.promoteAllPending(
				context.Background(), threadID,
			)
		} else {
			_, err = lane.promoteAllPending(
				context.Background(), threadID,
			)
			if err == nil {
				err = lane.machine.RunTurn(
					context.Background(), threadID,
				)
			}
		}
		if job.onDone != nil {
			job.onDone(err)
		}
	}
}

func (lane *sessionLane) promoteAllPending(
	ctx context.Context,
	threadID string,
) (bool, error) {
	any := false
	for {
		promoted, err := lane.machine.PromoteOnePending(ctx, threadID)
		if err != nil {
			return any, err
		}
		if !promoted {
			return any, nil
		}
		any = true
	}
}

// ErrNotRegistered means the session has no registered machine.
var ErrNotRegistered = errNotRegistered{}

type errNotRegistered struct{}

func (errNotRegistered) Error() string { return "session not registered" }
