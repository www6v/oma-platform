package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

const smokeReplyText = "smoke-e2e-ok"

// TestE2ESmokeCriticalPath exercises create agent → session → message → reply.
func TestE2ESmokeCriticalPath(t *testing.T) {
	recording := &harness.RecordingClient{
		FakeClient: harness.FakeClient{Text: smokeReplyText},
	}
	handler, _ := testRouterHarness(t, recording)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	assertHealthOK(t, client, server.URL)
	assertStatsOK(t, client, server.URL)

	agentID := createSmokeAgent(t, client, server.URL)
	sessionID := createSmokeSession(t, client, server.URL, agentID)

	eventsURL := server.URL + "/v1/sessions/" + sessionID + "/events"
	postJSON(t, client, eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"smoke"}]}]}`,
		http.StatusAccepted,
	)

	reply := waitForAgentReplyText(t, client, eventsURL, smokeReplyText, 5*time.Second)
	if reply != smokeReplyText {
		t.Fatalf("reply=%q want %q", reply, smokeReplyText)
	}

	last, ok := recording.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request")
	}
	if last.SessionID != sessionID {
		t.Fatalf("harness session=%q want %q", last.SessionID, sessionID)
	}
	if last.Agent.ID != agentID {
		t.Fatalf("harness agent=%q want %q", last.Agent.ID, agentID)
	}
}

// TestE2ESmokeP1APIs covers platform APIs without waiting for a harness turn.
func TestE2ESmokeP1APIs(t *testing.T) {
	handler := testRouter(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	assertHealthOK(t, client, server.URL)

	meResp, err := client.Get(server.URL + "/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("me status=%d", meResp.StatusCode)
	}

	stats := assertStatsOK(t, client, server.URL)
	if stats["agents"] < 0 {
		t.Fatalf("stats=%v", stats)
	}

	agentID := createSmokeAgent(t, client, server.URL)
	sessionID := createSmokeSession(t, client, server.URL, agentID)

	getResp, err := client.Get(server.URL + "/v1/sessions/" + sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get session status=%d", getResp.StatusCode)
	}
}

func assertHealthOK(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", resp.StatusCode)
	}
}

func assertStatsOK(
	t *testing.T,
	client *http.Client,
	baseURL string,
) map[string]float64 {
	t.Helper()
	resp, err := client.Get(baseURL + "/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status=%d", resp.StatusCode)
	}
	var stats map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"agents", "sessions", "environments", "model_cards", "api_keys",
	} {
		if _, ok := stats[key]; !ok {
			t.Fatalf("stats missing %s: %v", key, stats)
		}
	}
	return stats
}

func createSmokeAgent(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	body := `{"name":"smoke-e2e","model":"claude-sonnet-4-20250514","system_prompt":"hi"}`
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
		t.Fatalf("create agent status=%d", resp.StatusCode)
	}
	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	id, ok := agent["id"].(string)
	if !ok || id == "" {
		t.Fatalf("agent id=%v", agent["id"])
	}
	return id
}

func createSmokeSession(
	t *testing.T,
	client *http.Client,
	baseURL, agentID string,
) string {
	t.Helper()
	body := `{"agent":"` + agentID + `","title":"smoke-e2e"}`
	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/v1/sessions",
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
		t.Fatalf("create session status=%d", resp.StatusCode)
	}
	var sess map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	id, ok := sess["id"].(string)
	if !ok || id == "" {
		t.Fatalf("session id=%v", sess["id"])
	}
	return id
}

func waitForAgentReplyText(
	t *testing.T,
	client *http.Client,
	eventsURL, want string,
	timeout time.Duration,
) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		text, ok := findAgentReplyText(t, client, eventsURL, want)
		if ok {
			return text
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for agent.message text %q", want)
	return ""
}

func findAgentReplyText(
	t *testing.T,
	client *http.Client,
	eventsURL, want string,
) (string, bool) {
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
	for _, payload := range payloads {
		if eventType(payload) != "agent.message" {
			continue
		}
		var msg struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "text" && block.Text == want {
				return block.Text, true
			}
		}
	}
	return "", false
}

func decodeAMAEventPayloads(t *testing.T, body interface {
	Read([]byte) (int, error)
}) []json.RawMessage {
	t.Helper()
	var list struct {
		Data []struct {
			Data json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	out := make([]json.RawMessage, 0, len(list.Data))
	for _, item := range list.Data {
		out = append(out, item.Data)
	}
	return out
}
