package runtime

import (
	"sync"

	"github.com/open-ma/oma-building/internal/store"
)

// Registry holds one RuntimeRoom per runtime id.
type Registry struct {
	mu       sync.Mutex
	rooms    map[string]*Room
	runtimes *store.RuntimeRepo
}

// NewRegistry returns an empty runtime room registry.
func NewRegistry(runtimes *store.RuntimeRepo) *Registry {
	return &Registry{
		rooms:    make(map[string]*Room),
		runtimes: runtimes,
	}
}

// Room returns the room for runtimeID, creating it if needed.
func (reg *Registry) Room(runtimeID, userID string) *Room {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	room, ok := reg.rooms[runtimeID]
	if !ok {
		room = newRoom(reg.runtimes, runtimeID, userID)
		reg.rooms[runtimeID] = room
	}
	return room
}
