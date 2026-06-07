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
