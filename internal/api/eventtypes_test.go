package api

import "testing"

func TestIsInterruptEventType(t *testing.T) {
	if !isInterruptEventType("user.interrupt") {
		t.Fatal("expected user.interrupt")
	}
	if isInterruptEventType("user.message") {
		t.Fatal("user.message is not interrupt")
	}
}

func TestIsAllowedClientEventTypeSessionThreadCreated(t *testing.T) {
	if !isAllowedClientEventType("session.thread_created") {
		t.Fatal("session.thread_created must be allowed for wire-compat")
	}
}
