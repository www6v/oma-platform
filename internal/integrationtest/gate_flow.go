package integrationtest

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	gatePolicyMount   = "policy.yaml"
	gateReceiptsMount = "receipts.jsonl"
)

//go:embed testdata/gate/policy.yaml
var GatePolicyFixture []byte

//go:embed testdata/gate/receipts.jsonl
var GateReceiptsFixture []byte

// RunGateCookbookHitlFlow exercises gate HITL GT2/GT3: one turn emits
// agent.custom_tool_use without tool_result → session.status_idle with
// requires_action and event_ids.
func RunGateCookbookHitlFlow(
	t *testing.T,
	handler http.Handler,
	sim *harness.GateSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	policyFileID := uploadGateFile(
		t, client, base, gatePolicyMount, GatePolicyFixture, "text/yaml",
	)
	receiptsFileID := uploadGateFile(
		t, client, base, gateReceiptsMount, GateReceiptsFixture, "application/x-ndjson",
	)
	agentID := createGateAgent(t, client, base)
	sessionID := createGateSession(
		t, client, base, agentID, policyFileID, receiptsFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postGateMessage(
		t, client, eventsURL,
		"Process receipts r01 and r02 using decide or escalate once each.",
	)
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlTurn1Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 1 {
		t.Fatalf("after turn 1 harness turns=%d want 1", sim.TurnCount())
	}

	idle := findLastStatusIdle(t, client, eventsURL)
	stopReason, ok := idle["stop_reason"].(map[string]any)
	if !ok {
		t.Fatalf("status_idle missing stop_reason: %v", idle)
	}
	if stopReason["type"] != "requires_action" {
		t.Fatalf("stop_reason.type=%v want requires_action", stopReason["type"])
	}
	if stopReason["action_type"] != "custom_tool_result" {
		t.Fatalf(
			"stop_reason.action_type=%v want custom_tool_result",
			stopReason["action_type"],
		)
	}
	ids := stopReasonEventIDs(stopReason)
	if len(ids) != 2 {
		t.Fatalf("event_ids=%v want 2 pending custom tools", ids)
	}
	want := map[string]struct{}{
		harness.GateCustomToolDecideID:   {},
		harness.GateCustomToolEscalateID: {},
	}
	for _, id := range ids {
		if _, ok := want[id]; !ok {
			t.Fatalf("unexpected event_id %q in %v", id, ids)
		}
	}

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request")
	}
	if last.SessionID != sessionID {
		t.Fatalf("harness session=%q want %q", last.SessionID, sessionID)
	}
	if len(last.Resources) < 2 {
		t.Fatalf("expected >=2 resources on turn, got %d", len(last.Resources))
	}
}

// RunGateCookbookHitlResumeFlow runs the full HITL loop: requires_action,
// two user.custom_tool_result replies, synthesized agent.tool_result, and
// final end_turn after all pending custom tools are answered.
func RunGateCookbookHitlResumeFlow(
	t *testing.T,
	handler http.Handler,
	sim *harness.GateSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	policyFileID := uploadGateFile(
		t, client, base, gatePolicyMount, GatePolicyFixture, "text/yaml",
	)
	receiptsFileID := uploadGateFile(
		t, client, base, gateReceiptsMount, GateReceiptsFixture, "application/x-ndjson",
	)
	agentID := createGateAgent(t, client, base)
	sessionID := createGateSession(
		t, client, base, agentID, policyFileID, receiptsFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postGateMessage(
		t, client, eventsURL,
		"Process receipts r01 and r02 using decide or escalate once each.",
	)
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlTurn1Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 1 {
		t.Fatalf("after turn 1 harness turns=%d want 1", sim.TurnCount())
	}

	postGateCustomToolResult(
		t, client, eventsURL,
		harness.GateCustomToolDecideID,
		`{"action":"approve","receipt_id":"r01"}`,
	)
	assertPromotedToolResult(t, client, eventsURL, harness.GateCustomToolDecideID)
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlResumeMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	idle := findLastStatusIdle(t, client, eventsURL)
	stopReason, _ := idle["stop_reason"].(map[string]any)
	if stopReason["type"] != "requires_action" {
		t.Fatalf("after first result stop_reason.type=%v want requires_action", stopReason["type"])
	}
	if len(stopReasonEventIDs(stopReason)) != 1 {
		t.Fatalf("after first result event_ids=%v want 1 pending", stopReasonEventIDs(stopReason))
	}

	postGateCustomToolResult(
		t, client, eventsURL,
		harness.GateCustomToolEscalateID,
		`{"question":"category unclear","receipt_id":"r02"}`,
	)
	assertPromotedToolResult(t, client, eventsURL, harness.GateCustomToolEscalateID)
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlCompleteMarker, 5*time.Second,
	)
	waitForEndTurnIdle(t, client, eventsURL, sessionURL, 5*time.Second)

	if sim.TurnCount() != 3 {
		t.Fatalf("after HITL resume harness turns=%d want 3", sim.TurnCount())
	}
	_ = agentID
}

// RunGateCookbookHitlSlidingWindowFlow asserts GT5 server sliding window: when
// six custom tools are pending, requires_action exposes only the first five ids.
func RunGateCookbookHitlSlidingWindowFlow(
	t *testing.T,
	handler http.Handler,
	sim *harness.GateSimulatingClient,
) {
	t.Helper()
	sim.Turn1PendingCount = 6

	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	policyFileID := uploadGateFile(
		t, client, base, gatePolicyMount, GatePolicyFixture, "text/yaml",
	)
	receiptsFileID := uploadGateFile(
		t, client, base, gateReceiptsMount, GateReceiptsFixture, "application/x-ndjson",
	)
	agentID := createGateAgent(t, client, base)
	sessionID := createGateSession(
		t, client, base, agentID, policyFileID, receiptsFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postGateMessage(t, client, eventsURL, "Process six receipts.")
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlTurn1Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	idle := findLastStatusIdle(t, client, eventsURL)
	stopReason, ok := idle["stop_reason"].(map[string]any)
	if !ok {
		t.Fatalf("status_idle missing stop_reason: %v", idle)
	}
	ids := stopReasonEventIDs(stopReason)
	if len(ids) != harness.MaxPendingCustomToolEventIDs {
		t.Fatalf(
			"event_ids=%v want %d entries",
			ids,
			harness.MaxPendingCustomToolEventIDs,
		)
	}
	want := []string{
		"ctu_gate_00", "ctu_gate_01", "ctu_gate_02", "ctu_gate_03", "ctu_gate_04",
	}
	for idx, id := range want {
		if ids[idx] != id {
			t.Fatalf("event_ids[%d]=%q want %q (full=%v)", idx, ids[idx], id, ids)
		}
	}
}

// RunGateDuplicateCustomToolResultFlow posts the same custom_tool_result twice
// (GT5 server tolerance) and still completes the HITL loop.
func RunGateDuplicateCustomToolResultFlow(
	t *testing.T,
	handler http.Handler,
	sim *harness.GateSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	policyFileID := uploadGateFile(
		t, client, base, gatePolicyMount, GatePolicyFixture, "text/yaml",
	)
	receiptsFileID := uploadGateFile(
		t, client, base, gateReceiptsMount, GateReceiptsFixture, "application/x-ndjson",
	)
	agentID := createGateAgent(t, client, base)
	sessionID := createGateSession(
		t, client, base, agentID, policyFileID, receiptsFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postGateMessage(
		t, client, eventsURL,
		"Process receipts r01 and r02 using decide or escalate once each.",
	)
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlTurn1Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	resultBody := `{"action":"approve","receipt_id":"r01"}`
	postGateCustomToolResult(
		t, client, eventsURL, harness.GateCustomToolDecideID, resultBody,
	)
	postGateCustomToolResultExpect(
		t, client, eventsURL, harness.GateCustomToolDecideID, resultBody,
		http.StatusAccepted,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	postGateCustomToolResult(
		t, client, eventsURL, harness.GateCustomToolEscalateID,
		`{"question":"category unclear","receipt_id":"r02"}`,
	)
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlCompleteMarker, 5*time.Second,
	)
	waitForEndTurnIdle(t, client, eventsURL, sessionURL, 5*time.Second)

	userResults := countEventPayloads(
		t, client, eventsURL, "user.custom_tool_result",
	)
	if userResults < 3 {
		t.Fatalf("user.custom_tool_result count=%d want >=3 (duplicate allowed)", userResults)
	}
	toolResults := countToolResultsForID(
		t, client, eventsURL, harness.GateCustomToolDecideID,
	)
	if toolResults < 2 {
		t.Fatalf(
			"agent.tool_result for %q count=%d want >=2 after duplicate reply",
			harness.GateCustomToolDecideID,
			toolResults,
		)
	}
}

// RunGateCustomToolResultIsError promotes is_error results and synthesizes
// agent.tool_result with the same flag.
func RunGateCustomToolResultIsErrorFlow(
	t *testing.T,
	handler http.Handler,
	sim *harness.GateSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	policyFileID := uploadGateFile(
		t, client, base, gatePolicyMount, GatePolicyFixture, "text/yaml",
	)
	receiptsFileID := uploadGateFile(
		t, client, base, gateReceiptsMount, GateReceiptsFixture, "application/x-ndjson",
	)
	agentID := createGateAgent(t, client, base)
	sessionID := createGateSession(
		t, client, base, agentID, policyFileID, receiptsFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postGateMessage(
		t, client, eventsURL,
		"Process receipts r01 and r02 using decide or escalate once each.",
	)
	waitForEventMarker(
		t, client, eventsURL, harness.GateHitlTurn1Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	postGateCustomToolResultIsError(
		t, client, eventsURL, harness.GateCustomToolDecideID, "reviewer rejected",
	)
	assertPromotedToolResultIsError(
		t, client, eventsURL, harness.GateCustomToolDecideID,
	)
	_ = sim
}

func uploadGateFile(
	t *testing.T,
	client *http.Client,
	base, filename string,
	content []byte,
	mediaType string,
) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"filename":     filename,
		"content":      string(content),
		"media_type":   mediaType,
		"encoding":     "utf8",
		"downloadable": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/files", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload %s status=%d body=%s", filename, resp.StatusCode, readBody(resp))
	}
	var file map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		t.Fatal(err)
	}
	id, _ := file["id"].(string)
	if id == "" {
		t.Fatalf("upload %s id=%v", filename, file["id"])
	}
	return id
}

func createGateAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"cookbook-gate",
		"model":"claude-sonnet-4-20250514",
		"system_prompt":"Expense gate with decide and escalate custom tools.",
		"tools":[
			{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}},
			{"type":"custom","name":"decide","description":"approve/reject","input_schema":{"type":"object"}},
			{"type":"custom","name":"escalate","description":"human review","input_schema":{"type":"object"}}
		]
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

func createGateSession(
	t *testing.T,
	client *http.Client,
	base, agentID, policyFileID, receiptsFileID string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent": agentID,
		"title": "Expense gate HITL",
		"resources": []any{
			map[string]any{
				"type":       "file",
				"file_id":    policyFileID,
				"mount_path": gatePolicyMount,
			},
			map[string]any{
				"type":       "file",
				"file_id":    receiptsFileID,
				"mount_path": gateReceiptsMount,
			},
		},
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

func postGateMessage(
	t *testing.T,
	client *http.Client,
	eventsURL, text string,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"events": []any{
			map[string]any{
				"type": "user.message",
				"content": []any{
					map[string]string{"type": "text", "text": text},
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

func postGateCustomToolResult(
	t *testing.T,
	client *http.Client,
	eventsURL, customToolUseID, resultText string,
) {
	postGateCustomToolResultExpect(
		t, client, eventsURL, customToolUseID, resultText, http.StatusAccepted,
	)
}

func postGateCustomToolResultExpect(
	t *testing.T,
	client *http.Client,
	eventsURL, customToolUseID, resultText string,
	wantStatus int,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"events": []any{
			map[string]any{
				"type":                "user.custom_tool_result",
				"custom_tool_use_id": customToolUseID,
				"content": []any{
					map[string]string{"type": "text", "text": resultText},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, eventsURL, payload)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf(
			"custom_tool_result status=%d want %d body=%s",
			resp.StatusCode,
			wantStatus,
			readBody(resp),
		)
	}
}

func postGateCustomToolResultIsError(
	t *testing.T,
	client *http.Client,
	eventsURL, customToolUseID, resultText string,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"events": []any{
			map[string]any{
				"type":                "user.custom_tool_result",
				"custom_tool_use_id": customToolUseID,
				"is_error":            true,
				"content": []any{
					map[string]string{"type": "text", "text": resultText},
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
		t.Fatalf("custom_tool_result status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func countEventPayloads(
	t *testing.T,
	client *http.Client,
	eventsURL, eventType string,
) int {
	t.Helper()
	resp, err := client.Get(eventsURL + "?order=asc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payloads := decodeEventPayloads(t, resp.Body)
	count := 0
	for _, raw := range payloads {
		var meta map[string]any
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		if meta["type"] == eventType {
			count++
		}
	}
	return count
}

func countToolResultsForID(
	t *testing.T,
	client *http.Client,
	eventsURL, toolUseID string,
) int {
	t.Helper()
	resp, err := client.Get(eventsURL + "?order=asc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payloads := decodeEventPayloads(t, resp.Body)
	count := 0
	for _, raw := range payloads {
		var meta map[string]any
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		if meta["type"] == "agent.tool_result" && meta["tool_use_id"] == toolUseID {
			count++
		}
	}
	return count
}

func assertPromotedToolResultIsError(
	t *testing.T,
	client *http.Client,
	eventsURL, toolUseID string,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(eventsURL + "?order=asc")
		if err != nil {
			t.Fatal(err)
		}
		payloads := decodeEventPayloads(t, resp.Body)
		resp.Body.Close()
		for _, raw := range payloads {
			var meta map[string]any
			if err := json.Unmarshal(raw, &meta); err != nil {
				continue
			}
			if meta["type"] != "agent.tool_result" {
				continue
			}
			if meta["tool_use_id"] != toolUseID {
				continue
			}
			if meta["is_error"] != true {
				t.Fatalf("tool_result is_error=%v want true", meta["is_error"])
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("missing is_error agent.tool_result for %q", toolUseID)
}

func assertPromotedToolResult(
	t *testing.T,
	client *http.Client,
	eventsURL, toolUseID string,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(eventsURL + "?order=asc")
		if err != nil {
			t.Fatal(err)
		}
		payloads := decodeEventPayloads(t, resp.Body)
		resp.Body.Close()
		for _, raw := range payloads {
			var meta map[string]any
			if err := json.Unmarshal(raw, &meta); err != nil {
				continue
			}
			if meta["type"] != "agent.tool_result" {
				continue
			}
			if meta["tool_use_id"] == toolUseID {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("missing synthesized agent.tool_result for %q", toolUseID)
}

func waitForEndTurnIdle(
	t *testing.T,
	client *http.Client,
	eventsURL, sessionURL string,
	timeout time.Duration,
) {
	t.Helper()
	waitForSessionIdle(t, client, sessionURL, timeout)
	idle := findLastStatusIdle(t, client, eventsURL)
	stopReason, ok := idle["stop_reason"].(map[string]any)
	if !ok {
		t.Fatalf("status_idle missing stop_reason: %v", idle)
	}
	if stopReason["type"] != "end_turn" {
		t.Fatalf("stop_reason.type=%v want end_turn", stopReason["type"])
	}
}

func findLastStatusIdle(
	t *testing.T,
	client *http.Client,
	eventsURL string,
) map[string]any {
	t.Helper()
	resp, err := client.Get(eventsURL + "?order=asc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	payloads := decodeEventPayloads(t, resp.Body)
	var last map[string]any
	for _, raw := range payloads {
		var meta map[string]any
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		if meta["type"] == "session.status_idle" {
			last = meta
		}
	}
	if last == nil {
		t.Fatal("missing session.status_idle event")
	}
	return last
}

func stopReasonEventIDs(stopReason map[string]any) []string {
	raw, ok := stopReason["event_ids"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
