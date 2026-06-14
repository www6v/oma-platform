package oauthflow

import (
	"sync"
	"time"
)

const defaultStateTTL = 10 * time.Minute

// PendingState is server-side OAuth state (PKCE + vault context).
type PendingState struct {
	TenantID           string
	VaultID            string
	CredentialID       string
	McpServerURL       string
	CodeVerifier       string
	ClientID           string
	ClientSecret       string
	TokenEndpoint      string
	AuthorizationServer string
	RedirectURI        string
	ResourceURI        string
}

// StateStore holds in-flight OAuth states (parity with KV oauth_state:*).
type StateStore struct {
	mu      sync.Mutex
	entries map[string]stateEntry
	ttl     time.Duration
}

type stateEntry struct {
	state   PendingState
	expires time.Time
}

// NewStateStore returns a memory store with a 10-minute TTL.
func NewStateStore() *StateStore {
	return &StateStore{
		entries: make(map[string]stateEntry),
		ttl:     defaultStateTTL,
	}
}

// Put stores state under token.
func (s *StateStore) Put(token string, state PendingState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[token] = stateEntry{
		state:   state,
		expires: time.Now().Add(s.ttl),
	}
}

// Get loads state when still valid.
func (s *StateStore) Get(token string) (PendingState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[token]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(s.entries, token)
		}
		return PendingState{}, false
	}
	return entry.state, true
}

// Delete removes a state token.
func (s *StateStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, token)
}
