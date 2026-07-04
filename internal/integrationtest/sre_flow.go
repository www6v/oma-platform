package integrationtest

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

const (
	sreLogMount      = "logs/checkout-svc.log"
	sreManifestMount = "infra/k8s/checkout-deploy.yaml"
	sreRunbookMount  = "runbooks/oom.md"
)

//go:embed testdata/sre/alert.json
var SreAlertFixture []byte

//go:embed testdata/sre/logs/checkout-svc.log
var SreLogFixture []byte

//go:embed testdata/sre/infra/k8s/checkout-deploy.yaml
var SreManifestFixture []byte

//go:embed testdata/sre/runbooks/oom.md
var SreRunbookFixture []byte

// RunSreIncidentResponderFlow exercises sre_incident_responder: skill inject,
// three file resources, PagerDuty alert message, and HITL open_pr → approval.
func RunSreIncidentResponderFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.SreSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	skillID, skillVersion := createHarnessSkill(t, client, base)
	logFileID := uploadGateFile(
		t, client, base, sreLogMount, SreLogFixture, "text/plain",
	)
	manifestFileID := uploadGateFile(
		t, client, base, sreManifestMount, SreManifestFixture, "text/yaml",
	)
	runbookFileID := uploadGateFile(
		t, client, base, sreRunbookMount, SreRunbookFixture, "text/markdown",
	)

	agentID := createSreAgent(t, client, base, skillID, skillVersion)
	sessionID := createSreSession(
		t, client, base, agentID,
		logFileID, manifestFileID, runbookFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postSreAlertMessage(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.SreInvestigateMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 1 {
		t.Fatalf("after turn 1 harness turns=%d want 1", sim.TurnCount())
	}

	idle := findLastStatusIdle(t, client, eventsURL)
	stopReason, _ := idle["stop_reason"].(map[string]any)
	if stopReason["type"] != "requires_action" {
		t.Fatalf("stop_reason.type=%v want requires_action", stopReason["type"])
	}
	ids := stopReasonEventIDs(stopReason)
	if len(ids) != 1 || ids[0] != demo.SreCustomToolOpenPRID {
		t.Fatalf("event_ids=%v want [%s]", ids, demo.SreCustomToolOpenPRID)
	}

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request")
	}
	if len(last.Skills) == 0 {
		t.Fatal("expected resolved skills on harness request")
	}
	if len(last.Resources) < 3 {
		t.Fatalf("expected >=3 resources, got %d", len(last.Resources))
	}

	postGateCustomToolResult(
		t, client, eventsURL,
		demo.SreCustomToolOpenPRID,
		`{"pr_number":1,"url":"https://github.test/pr/1"}`,
	)
	assertPromotedToolResult(t, client, eventsURL, demo.SreCustomToolOpenPRID)
	waitForEventMarker(
		t, client, eventsURL, demo.SrePROpenMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 2 {
		t.Fatalf("after turn 2 harness turns=%d want 2", sim.TurnCount())
	}

	idle = findLastStatusIdle(t, client, eventsURL)
	stopReason, _ = idle["stop_reason"].(map[string]any)
	if stopReason["type"] != "requires_action" {
		t.Fatalf("after open_pr stop_reason.type=%v want requires_action", stopReason["type"])
	}
	ids = stopReasonEventIDs(stopReason)
	if len(ids) != 1 || ids[0] != demo.SreCustomToolApprovalID {
		t.Fatalf("approval event_ids=%v want [%s]", ids, demo.SreCustomToolApprovalID)
	}

	postGateCustomToolResult(
		t, client, eventsURL,
		demo.SreCustomToolApprovalID,
		`{"decision":"approved"}`,
	)
	assertPromotedToolResult(t, client, eventsURL, demo.SreCustomToolApprovalID)
	waitForEventMarker(
		t, client, eventsURL, demo.SreCompleteMarker, 5*time.Second,
	)
	waitForEndTurnIdle(t, client, eventsURL, sessionURL, 5*time.Second)

	if sim.TurnCount() != 3 {
		t.Fatalf("after HITL resume harness turns=%d want 3", sim.TurnCount())
	}
}

func createSreAgent(
	t *testing.T,
	client *http.Client,
	base, skillID, skillVersion string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"name":  "cookbook-sre",
		"model": "claude-sonnet-4-20250514",
		"system_prompt": "On-call SRE. Read logs, infra, and runbooks. " +
			"Use open_pull_request, request_approval, merge_pull_request.",
		"skills": []any{
			map[string]any{
				"type":     "custom",
				"skill_id": skillID,
				"version":  skillVersion,
			},
		},
		"tools": []any{
			map[string]any{
				"type": "agent_toolset_20260401",
				"default_config": map[string]any{
					"enabled": true,
					"permission_policy": map[string]any{
						"type": "always_allow",
					},
				},
			},
			map[string]any{
				"type":        "custom",
				"name":        "open_pull_request",
				"description": "Open a PR with an infra fix",
				"input_schema": map[string]any{
					"type": "object",
				},
			},
			map[string]any{
				"type":        "custom",
				"name":        "request_approval",
				"description": "Request human approval before merge",
				"input_schema": map[string]any{
					"type": "object",
				},
			},
			map[string]any{
				"type":        "custom",
				"name":        "merge_pull_request",
				"description": "Merge an approved PR",
				"input_schema": map[string]any{
					"type": "object",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/agents", payload)
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

func createSreSession(
	t *testing.T,
	client *http.Client,
	base, agentID string,
	logFileID, manifestFileID, runbookFileID string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent": agentID,
		"title": "SRE incident checkout-svc OOM",
		"resources": []any{
			map[string]any{
				"type":       "file",
				"file_id":    logFileID,
				"mount_path": sreLogMount,
			},
			map[string]any{
				"type":       "file",
				"file_id":    manifestFileID,
				"mount_path": sreManifestMount,
			},
			map[string]any{
				"type":       "file",
				"file_id":    runbookFileID,
				"mount_path": sreRunbookMount,
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

func postSreAlertMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"events": []any{
			map[string]any{
				"type": "user.message",
				"content": []any{
					map[string]string{
						"type": "text",
						"text": "PagerDuty alert:\n" + string(SreAlertFixture),
					},
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
