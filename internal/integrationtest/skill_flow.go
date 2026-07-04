package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

const skillRunbookBody = "# Incident runbooks\n\nConsult the team runbooks before proposing any infrastructure fix.\n"

// RunSkillHarnessInjectionFlow exercises skill resolve + mount + system prompt inject.
func RunSkillHarnessInjectionFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.SkillSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	skillID, skillVersion := createHarnessSkill(t, client, base)
	agentID := createSkillAgent(t, client, base, skillID, skillVersion)
	sessionID := createSkillSession(t, client, base, agentID)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	postSkillUserMessage(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.SkillHarnessMarker, 5*time.Second,
	)
	waitForSessionIdle(
		t, client, base+"/v1/sessions/"+sessionID, 5*time.Second,
	)

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request")
	}
	if len(last.Skills) == 0 {
		t.Fatal("expected resolved skills on harness request")
	}
}

func createHarnessSkill(
	t *testing.T,
	client *http.Client,
	base string,
) (string, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"name":          "incident-runbooks",
		"display_title": "Incident Runbooks",
		"description":   "Consult runbooks before infra changes",
		"files": []any{
			map[string]any{
				"filename": "SKILL.md",
				"content":  skillRunbookBody,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/skills", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create skill status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var skill map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&skill); err != nil {
		t.Fatal(err)
	}
	id, _ := skill["id"].(string)
	version, _ := skill["latest_version"].(string)
	if id == "" || version == "" {
		t.Fatalf("skill=%v", skill)
	}
	return id, version
}

func createSkillAgent(
	t *testing.T,
	client *http.Client,
	base, skillID, skillVersion string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"name":          "skill-harness-agent",
		"model":         "faux/test",
		"system_prompt": "You are an on-call agent. Follow attached skills.",
		"skills": []any{
			map[string]any{
				"type":     "custom",
				"skill_id": skillID,
				"version":  skillVersion,
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

func createSkillSession(
	t *testing.T,
	client *http.Client,
	base, agentID string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent": agentID,
		"title": "Skill harness probe",
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
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatal("missing session id")
	}
	return id
}

func postSkillUserMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	body := []byte(`{
		"events":[{
			"type":"user.message",
			"content":[{"type":"text","text":"Triage this alert using the runbook skill."}]
		}]
	}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("message status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}
