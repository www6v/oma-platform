package stream_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/stream"
)

func TestSubscriberReceivesBroadcast(t *testing.T) {
	hub := stream.NewHub()
	ch, unsub := hub.Subscribe("sess_test")
	defer unsub()

	payload, _ := json.Marshal(map[string]string{"type": "agent.message"})
	hub.Publish("sess_test", stream.Event{Seq: 1, Payload: payload})

	select {
	case ev := <-ch:
		if ev.Seq != 1 {
			t.Fatalf("seq=%d", ev.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}
