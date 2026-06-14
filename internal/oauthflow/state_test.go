package oauthflow

import (
	"testing"
	"time"
)

func TestStateStorePutGetDelete(t *testing.T) {
	s := NewStateStore()
	s.Put("tok", PendingState{VaultID: "v1", TenantID: "t1"})
	got, ok := s.Get("tok")
	if !ok || got.VaultID != "v1" {
		t.Fatalf("Get: ok=%v vault=%s", ok, got.VaultID)
	}
	s.Delete("tok")
	_, ok = s.Get("tok")
	if ok {
		t.Fatal("expected deleted state")
	}
}

func TestStateStoreExpires(t *testing.T) {
	s := &StateStore{
		entries: make(map[string]stateEntry),
		ttl:     20 * time.Millisecond,
	}
	s.Put("tok", PendingState{VaultID: "v1"})
	time.Sleep(30 * time.Millisecond)
	_, ok := s.Get("tok")
	if ok {
		t.Fatal("expected expired state")
	}
}
