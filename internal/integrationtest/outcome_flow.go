package integrationtest

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

//go:embed testdata/outcome/rubric.md
var OutcomeRubricFixture []byte

// RunOutcomeGraderCookbookFlow exercises define_outcome + grade-revise loop:
// turn 1 fails grader, revision turn passes, session ends satisfied.
func RunOutcomeGraderCookbookFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.OutcomeSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	agentID := createOutcomeAgent(t, client, base)
	sessionID := createOutcomeSession(t, client, base, agentID)
	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postOutcomeDefineAndMessage(
		t, client, eventsURL,
		"Write a one-line summary of quarterly revenue trends.",
	)
	waitForEventMarker(
		t, client, eventsURL, demo.OutcomePassMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 2 {
		t.Fatalf("harness turns=%d want 2 (initial + revision)", sim.TurnCount())
	}

	end := findLastOutcomeEvaluationEnd(t, client, eventsURL)
	if end["result"] != "satisfied" {
		t.Fatalf("outcome result=%v want satisfied", end["result"])
	}

	sess := getSessionJSON(t, client, sessionURL)
	evals, ok := sess["outcome_evaluations"].([]any)
	if !ok || len(evals) == 0 {
		t.Fatalf("outcome_evaluations=%v want non-empty", sess["outcome_evaluations"])
	}
	lastEval, ok := evals[len(evals)-1].(map[string]any)
	if !ok {
		t.Fatalf("outcome_evaluations row=%v", evals[len(evals)-1])
	}
	if lastEval["result"] != "satisfied" {
		t.Fatalf("aggregate result=%v want satisfied", lastEval["result"])
	}

	feedbackFound := false
	for _, payload := range listEventPayloads(t, client, eventsURL) {
		var meta struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(payload, &meta) != nil {
			continue
		}
		if meta.Type != "user.message" {
			continue
		}
		for _, block := range meta.Content {
			if block.Type == "text" &&
				strings.Contains(block.Text, "<outcome_feedback") &&
				strings.Contains(block.Text, "Address the feedback") {
				feedbackFound = true
				break
			}
		}
	}
	if !feedbackFound {
		t.Fatal("expected outcome_feedback user.message after needs_revision")
	}
}

// RunOutcomeGraderRubricFileFlow exercises OG4: rubric loaded from Files API.
func RunOutcomeGraderRubricFileFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.OutcomeSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	rubricFileID := uploadGateFile(
		t, client, base, "outcome-rubric.md",
		OutcomeRubricFixture, "text/markdown",
	)
	agentID := createOutcomeAgent(t, client, base)
	sessionID := createOutcomeSession(t, client, base, agentID)
	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postOutcomeDefineFileRubric(
		t, client, eventsURL, rubricFileID,
		"Write a one-line summary of quarterly revenue trends.",
	)

	defineFound := false
	for _, payload := range listEventPayloads(t, client, eventsURL) {
		var meta map[string]any
		if json.Unmarshal(payload, &meta) != nil {
			continue
		}
		if meta["type"] != "user.define_outcome" {
			continue
		}
		rubric, ok := meta["rubric"].(map[string]any)
		if !ok || rubric["type"] != "file" {
			t.Fatalf("define_outcome rubric=%v want file spec", meta["rubric"])
		}
		if rubric["file_id"] != rubricFileID {
			t.Fatalf("file_id=%v want %q", rubric["file_id"], rubricFileID)
		}
		defineFound = true
	}
	if !defineFound {
		t.Fatal("missing echoed user.define_outcome with file rubric")
	}

	waitForEventMarker(
		t, client, eventsURL, demo.OutcomePassMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 2 {
		t.Fatalf("harness turns=%d want 2", sim.TurnCount())
	}
	end := findLastOutcomeEvaluationEnd(t, client, eventsURL)
	if end["result"] != "satisfied" {
		t.Fatalf("outcome result=%v want satisfied", end["result"])
	}
}

func createOutcomeAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"cookbook-outcome-grader",
		"model":"claude-sonnet-4-20250514",
		"system_prompt":"You summarize business metrics concisely."
	}`)
	resp := doJSON(t, client, http.MethodPost, base+"/v1/agents", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("agent status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	id, _ := agent["id"].(string)
	if id == "" {
		t.Fatal("missing agent id")
	}
	return id
}

func createOutcomeSession(
	t *testing.T,
	client *http.Client,
	base, agentID string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent": agentID,
		"title": "Outcome grader cookbook",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/sessions", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("session status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var sess map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	sessionID, _ := sess["id"].(string)
	if sessionID == "" {
		t.Fatal("missing session id")
	}
	return sessionID
}

func postOutcomeDefineAndMessage(
	t *testing.T,
	client *http.Client,
	eventsURL, task string,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"events": []any{
			map[string]any{
				"type": "user.define_outcome",
				"description": "Summary must mention revenue and be one sentence.",
				"criteria": []string{
					"Mentions revenue",
					"Single concise sentence",
				},
				"max_iterations": 3,
			},
			map[string]any{
				"type": "user.message",
				"content": []any{
					map[string]string{"type": "text", "text": task},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, eventsURL, payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postOutcomeDefineFileRubric(
	t *testing.T,
	client *http.Client,
	eventsURL, rubricFileID, task string,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"events": []any{
			map[string]any{
				"type":        "user.define_outcome",
				"description": "One-sentence revenue summary for executives.",
				"rubric": map[string]any{
					"type":    "file",
					"file_id": rubricFileID,
				},
				"max_iterations": 3,
			},
			map[string]any{
				"type": "user.message",
				"content": []any{
					map[string]string{"type": "text", "text": task},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, eventsURL, payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func findLastOutcomeEvaluationEnd(
	t *testing.T,
	client *http.Client,
	eventsURL string,
) map[string]any {
	t.Helper()
	var last map[string]any
	for _, payload := range listEventPayloads(t, client, eventsURL) {
		var meta map[string]any
		if json.Unmarshal(payload, &meta) != nil {
			continue
		}
		if meta["type"] == "span.outcome_evaluation_end" {
			last = meta
		}
	}
	if last == nil {
		t.Fatal("missing span.outcome_evaluation_end")
	}
	return last
}

func getSessionJSON(
	t *testing.T,
	client *http.Client,
	sessionURL string,
) map[string]any {
	t.Helper()
	resp, err := client.Get(sessionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sess map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func listEventPayloads(
	t *testing.T,
	client *http.Client,
	eventsURL string,
) []json.RawMessage {
	t.Helper()
	resp, err := client.Get(eventsURL + "?order=asc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return decodeEventPayloads(t, resp.Body)
}
