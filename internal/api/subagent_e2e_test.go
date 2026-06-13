package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	subAgentWorkerReply  = "subagent-worker-ok"
	subAgentPrimaryReply = "subagent-coordinator-ok"
	subAgentE2EThreadID  = "sthr_e2e_worker"
)

// TestE2ESubAgentCriticalPath verifies coordinator → harness sub_agents wiring →
// delegation events → threads API → trajectory.
func TestE2ESubAgentCriticalPath(t *testing.T) {
	sim := &harness.SubAgentSimulatingClient{
		WorkerReply:  subAgentWorkerReply,
		PrimaryReply: subAgentPrimaryReply,
	}
	handler, _ := testRouterHarness(t, sim)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	workerID, workerVersion := createSubAgentWorker(t, client, server.URL)
	coordinatorID := createSubAgentCoordinator(
		t, client, server.URL, workerID, workerVersion,
	)
	sessionID := createSmokeSession(t, client, server.URL, coordinatorID)

	eventsURL := server.URL + "/v1/sessions/" + sessionID + "/events"
	postJSON(t, client, eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"delegate scan"}]}]}`,
		http.StatusAccepted,
	)

	waitForAgentReplyText(
		t, client, eventsURL, subAgentPrimaryReply, 5*time.Second,
	)

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request")
	}
	if last.SessionID != sessionID {
		t.Fatalf("harness session=%q want %q", last.SessionID, sessionID)
	}
	if len(last.SubAgents) != 1 {
		t.Fatalf("sub_agents=%d want 1", len(last.SubAgents))
	}
	workerSnap, ok := last.SubAgents[workerID]
	if !ok {
		t.Fatalf("missing sub_agent %q keys=%v", workerID, mapKeys(last.SubAgents))
	}
	if workerSnap.Name != "subagent-worker" {
		t.Fatalf("worker name=%q", workerSnap.Name)
	}

	assertSubAgentEvents(t, client, eventsURL)
	assertSessionThreads(t, client, server.URL, sessionID, workerID)
	assertSubAgentTrajectory(t, client, server.URL, sessionID, coordinatorID)
}

func createSubAgentWorker(
	t *testing.T,
	client *http.Client,
	baseURL string,
) (id string, version int) {
	t.Helper()
	body := `{
		"name":"subagent-worker",
		"model":"faux/test",
		"system_prompt":"You are a worker sub-agent."
	}`
	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/v1/agents",
		bytes.NewBufferString(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create worker status=%d", resp.StatusCode)
	}
	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	id, _ = agent["id"].(string)
	if id == "" {
		t.Fatalf("worker id=%v", agent["id"])
	}
	version = int(agent["version"].(float64))
	return id, version
}

func createSubAgentCoordinator(
	t *testing.T,
	client *http.Client,
	baseURL, workerID string,
	workerVersion int,
) string {
	t.Helper()
	body := fmt.Sprintf(`{
		"name":"subagent-coordinator",
		"model":"faux/test",
		"system_prompt":"Delegate specialized work to workers.",
		"callable_agents":[
			{"type":"agent","id":%q,"version":%d}
		]
	}`, workerID, workerVersion)
	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/v1/agents",
		bytes.NewBufferString(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create coordinator status=%d body=%s", resp.StatusCode, readBodyPreview(resp))
	}
	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	id, ok := agent["id"].(string)
	if !ok || id == "" {
		t.Fatalf("coordinator id=%v", agent["id"])
	}
	return id
}

func assertSubAgentEvents(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	resp, err := client.Get(eventsURL + "?order=asc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list events status=%d", resp.StatusCode)
	}
	payloads := decodeAMAEventPayloads(t, resp.Body)

	sawThreadCreated := false
	sawWorkerReply := false
	sawPrimaryReply := false
	for _, payload := range payloads {
		typ := eventType(payload)
		switch typ {
		case "session.thread_created":
			var created struct {
				SessionThreadID string `json:"session_thread_id"`
				AgentID         string `json:"agent_id"`
			}
			if json.Unmarshal(payload, &created) != nil {
				continue
			}
			if created.SessionThreadID == subAgentE2EThreadID {
				sawThreadCreated = true
			}
		case "agent.message":
			var msg struct {
				SessionThreadID string `json:"session_thread_id"`
				Content         []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(payload, &msg) != nil {
				continue
			}
			text := firstTextBlock(msg.Content)
			if msg.SessionThreadID == subAgentE2EThreadID &&
				text == subAgentWorkerReply {
				sawWorkerReply = true
			}
			if msg.SessionThreadID == "" && text == subAgentPrimaryReply {
				sawPrimaryReply = true
			}
		}
	}
	if !sawThreadCreated {
		t.Fatal("missing session.thread_created for sub-agent")
	}
	if !sawWorkerReply {
		t.Fatal("missing worker agent.message on sub thread")
	}
	if !sawPrimaryReply {
		t.Fatal("missing coordinator agent.message on primary thread")
	}
}

func assertSessionThreads(
	t *testing.T,
	client *http.Client,
	baseURL, sessionID, workerID string,
) {
	t.Helper()
	resp, err := client.Get(baseURL + "/v1/sessions/" + sessionID + "/threads")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("threads status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("threads data=%v", body["data"])
	}
	primary := data[0].(map[string]any)
	if primary["id"] != "sthr_primary" {
		t.Fatalf("primary id=%v", primary["id"])
	}
	sub := data[1].(map[string]any)
	if sub["id"] != subAgentE2EThreadID {
		t.Fatalf("sub id=%v", sub["id"])
	}
	if sub["agent_id"] != workerID {
		t.Fatalf("sub agent_id=%v want %q", sub["agent_id"], workerID)
	}
}

func assertSubAgentTrajectory(
	t *testing.T,
	client *http.Client,
	baseURL, sessionID, coordinatorID string,
) {
	t.Helper()
	resp, err := client.Get(baseURL + "/v1/sessions/" + sessionID + "/trajectory")
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

	summary, ok := traj["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary=%T", traj["summary"])
	}
	if int(summary["num_threads"].(float64)) < 1 {
		t.Fatalf("num_threads=%v want >= 1", summary["num_threads"])
	}

	events, ok := traj["events"].([]any)
	if !ok {
		t.Fatalf("events=%T", traj["events"])
	}
	sawThreadCreated := false
	for _, item := range events {
		ev, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if ev["type"] != "session.thread_created" {
			continue
		}
		data, ok := ev["data"].(map[string]any)
		if !ok {
			continue
		}
		if data["session_thread_id"] == subAgentE2EThreadID {
			sawThreadCreated = true
		}
	}
	if !sawThreadCreated {
		t.Fatal("trajectory missing session.thread_created")
	}

	agentCfg, ok := traj["agent_config"].(map[string]any)
	if !ok || agentCfg["id"] != coordinatorID {
		t.Fatalf("agent_config=%v want id=%s", traj["agent_config"], coordinatorID)
	}
}

func mapKeys(m map[string]harness.AgentSnapshot) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func firstTextBlock(
	content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	},
) string {
	for _, block := range content {
		if block.Type == "text" {
			return block.Text
		}
	}
	return ""
}

func readBodyPreview(resp *http.Response) string {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if buf.Len() > 256 {
		return buf.String()[:256] + "..."
	}
	return buf.String()
}
