package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestSessionTrajectoryAfterTurn(t *testing.T) {
	recording := &harness.RecordingClient{
		FakeClient: harness.FakeClient{Text: "trajectory-ok"},
	}
	handler, _ := testRouterHarness(t, recording)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	agentID := createSmokeAgent(t, client, server.URL)
	sessionID := createSmokeSession(t, client, server.URL, agentID)

	eventsURL := server.URL + "/v1/sessions/" + sessionID + "/events"
	postJSON(t, client, eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}`,
		http.StatusAccepted,
	)
	waitForAgentReplyText(t, client, eventsURL, "trajectory-ok", 5*time.Second)

	trajURL := server.URL + "/v1/sessions/" + sessionID + "/trajectory"
	resp, err := client.Get(trajURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trajectory status=%d", resp.StatusCode)
	}

	var traj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&traj); err != nil {
		t.Fatal(err)
	}
	if traj["schema_version"] != "oma.trajectory.v1" {
		t.Fatalf("schema_version=%v", traj["schema_version"])
	}

	events, ok := traj["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("events=%T %#v", traj["events"], traj["events"])
	}

	summary, ok := traj["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary=%T", traj["summary"])
	}
	if int(summary["num_events"].(float64)) != len(events) {
		t.Fatalf("summary.num_events=%v events=%d", summary["num_events"], len(events))
	}
	if int(summary["num_turns"].(float64)) < 1 {
		t.Fatalf("summary.num_turns=%v", summary["num_turns"])
	}

	agentCfg, ok := traj["agent_config"].(map[string]any)
	if !ok || agentCfg["id"] != agentID {
		t.Fatalf("agent_config=%v want id=%s", traj["agent_config"], agentID)
	}
}
