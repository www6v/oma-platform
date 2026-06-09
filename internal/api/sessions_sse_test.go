package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

type sseEvent struct {
	Seq     int
	Payload json.RawMessage
}

func TestSessionEventsStreamLiveTurn(t *testing.T) {
	recording := &harness.RecordingClient{
		FakeClient: harness.FakeClient{Text: "sse hello"},
	}
	handler, _ := testRouterHarness(t, recording)
	server := httptest.NewServer(handler)
	defer server.Close()

	sid := createAgentSession(t, server.Client(), server.URL)

	streamURL := server.URL + "/v1/sessions/" + sid + "/events/stream"
	collected := make(chan []sseEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		events, err := readSSEUntil(
			streamURL,
			5*time.Second,
			func(events []sseEvent) bool {
				return hasEventTypes(
					events,
					"user.message",
					"session.lifecycle",
					"agent.message",
				) && lifecyclePhase(events, "turn_end")
			},
		)
		if err != nil {
			errCh <- err
			return
		}
		collected <- events
	}()

	time.Sleep(50 * time.Millisecond)

	postJSON(t, server.Client(), server.URL+"/v1/sessions/"+sid+"/events",
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}`,
		http.StatusAccepted)

	select {
	case err := <-errCh:
		t.Fatalf("sse read: %v", err)
	case events := <-collected:
		assertSSETurnFlow(t, events)
	case <-time.After(6 * time.Second):
		t.Fatal("timeout waiting for SSE turn events")
	}
}

func TestSessionEventsStreamReplay(t *testing.T) {
	handler, _ := testRouterHarness(t, &harness.FakeClient{Text: "replay ok"})
	server := httptest.NewServer(handler)
	defer server.Close()

	sid := createAgentSession(t, server.Client(), server.URL)

	postJSON(t, server.Client(), server.URL+"/v1/sessions/"+sid+"/events",
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}`,
		http.StatusAccepted)

	eventsURL := server.URL + "/v1/sessions/" + sid + "/events"
	waitForAgentMessage(t, server.Client(), eventsURL)

	listPayloads := getSessionEventPayloads(t, server.Client(), eventsURL)
	if len(listPayloads) == 0 {
		t.Fatal("expected persisted events")
	}

	replayURL := server.URL + "/v1/sessions/" + sid + "/events/stream?replay=1"
	replayed, err := readSSEUntil(
		replayURL,
		3*time.Second,
		func(events []sseEvent) bool {
			return len(events) >= len(listPayloads)
		},
	)
	if err != nil {
		t.Fatalf("replay stream: %v", err)
	}
	if len(replayed) < len(listPayloads) {
		t.Fatalf("replayed=%d persisted=%d", len(replayed), len(listPayloads))
	}
	for i, want := range listPayloads {
		if string(replayed[i].Payload) != string(want) {
			t.Fatalf("replay payload[%d] mismatch", i)
		}
		if replayed[i].Seq != i+1 {
			t.Fatalf("replay seq[%d]=%d want %d", i, replayed[i].Seq, i+1)
		}
	}
}

func createAgentSession(
	t *testing.T,
	client *http.Client,
	baseURL string,
) string {
	t.Helper()
	agentBody := postJSON(t, client, baseURL+"/v1/agents",
		`{"name":"sse","model":"claude-sonnet-4-20250514"}`,
		http.StatusCreated)
	var agent map[string]any
	if err := json.Unmarshal(agentBody, &agent); err != nil {
		t.Fatal(err)
	}

	sessBody := postJSON(t, client, baseURL+"/v1/sessions",
		`{"agent":"`+agent["id"].(string)+`"}`,
		http.StatusCreated)
	var sess map[string]any
	if err := json.Unmarshal(sessBody, &sess); err != nil {
		t.Fatal(err)
	}
	return sess["id"].(string)
}

func postJSON(
	t *testing.T,
	client *http.Client,
	url string,
	body string,
	wantStatus int,
) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status=%d body=%s", url, resp.StatusCode, respBody)
	}
	return respBody
}

func waitForAgentMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, payload := range getSessionEventPayloads(t, client, eventsURL) {
			if eventType(payload) == "agent.message" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for agent.message")
}

func getSessionEventPayloads(
	t *testing.T,
	client *http.Client,
	eventsURL string,
) []json.RawMessage {
	t.Helper()
	resp, err := client.Get(eventsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list events status=%d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			Data json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	out := make([]json.RawMessage, 0, len(body.Data))
	for _, item := range body.Data {
		out = append(out, item.Data)
	}
	return out
}

func readSSEUntil(
	url string,
	timeout time.Duration,
	done func([]sseEvent) bool,
) ([]sseEvent, error) {
	client := &http.Client{Timeout: timeout + 2*time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		return nil, fmt.Errorf("content-type=%q", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	deadline := time.Now().Add(timeout)
	var events []sseEvent
	var cur sseEvent
	haveID := false
	haveData := false

	flush := func() {
		if haveData {
			events = append(events, cur)
			cur = sseEvent{}
			haveID = false
			haveData = false
		}
	}

	type scanResult struct {
		line string
		err  error
	}
	lines := make(chan scanResult, 1)
	go func() {
		for scanner.Scan() {
			lines <- scanResult{line: scanner.Text()}
		}
		lines <- scanResult{err: scanner.Err()}
	}()

	for {
		if time.Now().After(deadline) {
			return events, fmt.Errorf("timeout after %d events", len(events))
		}

		var line string
		select {
		case res := <-lines:
			if res.err != nil {
				return events, res.err
			}
			line = res.line
		case <-time.After(50 * time.Millisecond):
			if done(events) {
				return events, nil
			}
			continue
		}

		if line == "" {
			flush()
			if done(events) {
				return events, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "id: ") {
			if _, err := fmt.Sscanf(line, "id: %d", &cur.Seq); err != nil {
				return events, err
			}
			haveID = true
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			cur.Payload = json.RawMessage(strings.TrimPrefix(line, "data: "))
			haveData = true
			if !haveID {
				return events, fmt.Errorf("data without id")
			}
		}
	}
}

func eventType(payload json.RawMessage) string {
	var meta struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(payload, &meta)
	return meta.Type
}

func lifecyclePhase(events []sseEvent, phase string) bool {
	for _, ev := range events {
		if eventType(ev.Payload) != "session.lifecycle" {
			continue
		}
		var meta struct {
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(ev.Payload, &meta)
		if meta.Phase == phase {
			return true
		}
	}
	return false
}

func hasEventTypes(events []sseEvent, types ...string) bool {
	seen := make(map[string]bool, len(types))
	for _, ev := range events {
		seen[eventType(ev.Payload)] = true
	}
	for _, typ := range types {
		if !seen[typ] {
			return false
		}
	}
	return true
}

func assertSSETurnFlow(t *testing.T, events []sseEvent) {
	t.Helper()
	if !hasEventTypes(events, "user.message", "agent.message") {
		t.Fatalf("missing core types: %s", summarizeEventTypes(events))
	}
	if !lifecyclePhase(events, "turn_start") {
		t.Fatalf("missing turn_start: %s", summarizeEventTypes(events))
	}
	if !lifecyclePhase(events, "turn_end") {
		t.Fatalf("missing turn_end: %s", summarizeEventTypes(events))
	}

	userIdx, agentIdx := -1, -1
	for i, ev := range events {
		switch eventType(ev.Payload) {
		case "user.message":
			userIdx = i
		case "agent.message":
			agentIdx = i
		}
	}
	if userIdx < 0 || agentIdx < 0 || userIdx >= agentIdx {
		t.Fatalf("event order invalid: %s", summarizeEventTypes(events))
	}

	prev := 0
	for _, ev := range events {
		if ev.Seq <= prev {
			t.Fatalf("non-monotonic seq: prev=%d cur=%d", prev, ev.Seq)
		}
		prev = ev.Seq
	}
}

func summarizeEventTypes(events []sseEvent) string {
	types := make([]string, 0, len(events))
	for _, ev := range events {
		types = append(types, eventType(ev.Payload))
	}
	return strings.Join(types, ",")
}
