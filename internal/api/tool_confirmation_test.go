package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestSessionToolConfirmationPendingAndTurn(t *testing.T) {
	recording := &harness.RecordingClient{
		FakeClient: harness.FakeClient{Text: "confirmed"},
	}
	handler, _ := testRouterHarness(t, recording)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	agentID := createSmokeAgent(t, client, server.URL)
	sessionID := createSmokeSession(t, client, server.URL, agentID)
	eventsURL := server.URL + "/v1/sessions/" + sessionID + "/events"
	pendingURL := server.URL + "/v1/sessions/" + sessionID + "/pending"

	postJSON(t, client, eventsURL,
		`{"events":[{"type":"user.tool_confirmation","tool_use_id":"toolu_test","result":"allow"}]}`,
		http.StatusAccepted,
	)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(eventsURL + "?limit=50")
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Data []struct {
				Type string `json:"type"`
			} `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		for _, ev := range page.Data {
			if ev.Type == "user.tool_confirmation" {
				goto promoted
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("tool_confirmation not promoted to event log")
promoted:

	resp, err := client.Get(pendingURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var pending struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pending); err != nil {
		t.Fatal(err)
	}
	if len(pending.Data) != 0 {
		t.Fatalf("expected empty pending after promote-only, got %d", len(pending.Data))
	}

	postJSON(t, client, eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"go"}]}]}`,
		http.StatusAccepted,
	)

	turnDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(turnDeadline) {
		resp, err = client.Get(eventsURL + "?limit=50")
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Data []struct {
				Type string `json:"type"`
			} `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		hasConfirm, hasAgent := false, false
		for _, ev := range page.Data {
			switch ev.Type {
			case "user.tool_confirmation":
				hasConfirm = true
			case "agent.message":
				hasAgent = true
			}
		}
		if hasConfirm && hasAgent {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("expected tool_confirmation promoted and harness turn completed")
	_ = agentID
}

func TestSessionToolConfirmationDenyStored(t *testing.T) {
	handler, _ := testRouterHarness(t, &harness.FakeClient{Text: "ok"})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	agentID := createSmokeAgent(t, client, server.URL)
	sessionID := createSmokeSession(t, client, server.URL, agentID)
	eventsURL := server.URL + "/v1/sessions/" + sessionID + "/events"

	postJSON(t, client, eventsURL, `{"events":[
		{"type":"user.tool_confirmation","tool_use_id":"toolu_deny","result":"deny","deny_message":"no"},
		{"type":"user.message","content":[{"type":"text","text":"next"}]}
	]}`, http.StatusAccepted)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(eventsURL + "?limit=50")
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Data []struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		for _, ev := range page.Data {
			if ev.Type != "user.tool_confirmation" {
				continue
			}
			var body struct {
				ToolUseID   string `json:"tool_use_id"`
				Result      string `json:"result"`
				DenyMessage string `json:"deny_message"`
			}
			if err := json.Unmarshal(ev.Data, &body); err != nil {
				continue
			}
			if body.ToolUseID == "toolu_deny" &&
				body.Result == "deny" &&
				body.DenyMessage == "no" {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("deny tool_confirmation not found in event log")
	_ = agentID
}
