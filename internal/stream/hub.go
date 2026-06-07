package stream

import (
	"encoding/json"
	"sync"
)

// Event is a broadcast payload with sequence metadata.
type Event struct {
	Seq     int
	Payload json.RawMessage
}

// Hub fans out session events to SSE subscribers.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

// NewHub returns an empty event hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan Event]struct{})}
}

// Subscribe registers a listener for sessionID.
func (h *Hub) Subscribe(sessionID string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	h.mu.Lock()
	if h.subs[sessionID] == nil {
		h.subs[sessionID] = make(map[chan Event]struct{})
	}
	h.subs[sessionID][ch] = struct{}{}
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		delete(h.subs[sessionID], ch)
		if len(h.subs[sessionID]) == 0 {
			delete(h.subs, sessionID)
		}
		h.mu.Unlock()
		close(ch)
	}
	return ch, unsub
}

// Publish delivers an event to all subscribers of sessionID.
func (h *Hub) Publish(sessionID string, ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[sessionID] {
		select {
		case ch <- ev:
		default:
		}
	}
}
