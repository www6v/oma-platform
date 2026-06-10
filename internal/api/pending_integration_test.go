package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestSessionPendingQueue(t *testing.T) {
	recording := &harness.RecordingClient{
		FakeClient: harness.FakeClient{Text: "pending-ok"},
	}
	stall := newStallHarness(recording)
	t.Cleanup(func() { stall.release() })

	handler, _ := testRouterHarness(t, stall)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	agentID := createSmokeAgent(t, client, server.URL)
	sessionID := createSmokeSession(t, client, server.URL, agentID)

	pendingURL := server.URL + "/v1/sessions/" + sessionID + "/pending"
	resp, err := client.Get(pendingURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pending status=%d", resp.StatusCode)
	}
	var emptyPending struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emptyPending); err != nil {
		t.Fatal(err)
	}
	if len(emptyPending.Data) != 0 {
		t.Fatalf("expected empty pending, got %d", len(emptyPending.Data))
	}

	eventsURL := server.URL + "/v1/sessions/" + sessionID + "/events"
	postJSON(t, client, eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"first"}]}]}`,
		http.StatusAccepted,
	)

	select {
	case <-stall.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for harness turn to start")
	}

	postJSON(t, client, eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"second"}]}]}`,
		http.StatusAccepted,
	)

	resp, err = client.Get(pendingURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var pending struct {
		Data []struct {
			Type    string `json:"type"`
			EventID string `json:"event_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pending); err != nil {
		t.Fatal(err)
	}
	if len(pending.Data) != 1 {
		t.Fatalf("expected 1 pending row, got %d", len(pending.Data))
	}
	if pending.Data[0].Type != "user.message" {
		t.Fatalf("type=%q", pending.Data[0].Type)
	}
	if pending.Data[0].EventID == "" {
		t.Fatal("expected event_id on pending row")
	}

	stall.release()
	waitForHarnessTurns(t, recording, 2, 5*time.Second)

	resp, err = client.Get(pendingURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var after struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if len(after.Data) != 0 {
		t.Fatalf("expected drained pending queue, got %d", len(after.Data))
	}

	trajURL := server.URL + "/v1/sessions/" + sessionID + "/trajectory"
	resp, err = client.Get(trajURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var traj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&traj); err != nil {
		t.Fatal(err)
	}
	summary := traj["summary"].(map[string]any)
	if int(summary["num_turns"].(float64)) < 1 {
		t.Fatalf("summary=%v", summary)
	}
	_ = agentID
}
