package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIntegrationsListEndpoints(t *testing.T) {
	handler := testRouter(t)

	paths := []string{
		"/v1/integrations/linear/installations",
		"/v1/integrations/github/publications?status=pending",
		"/v1/integrations/slack/agents/agt_test/publications",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if _, ok := resp["data"]; !ok {
				t.Fatalf("missing data: %v", resp)
			}
		})
	}
}

func TestLinearPublicationFirstFlow(t *testing.T) {
	handler := testRouter(t)

	createAgent := `{"name":"integrations-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents",
		bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	agentID := agent["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/environments?limit=5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var envs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envs)
	envID := envs["data"].([]any)[0].(map[string]any)["id"].(string)

	pubBody, _ := json.Marshal(map[string]any{
		"agentId":       agentID,
		"environmentId": envID,
		"personaName":   "Test Bot",
		"returnUrl":     "http://localhost/console/integrations",
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/linear/publications",
		bytes.NewReader(pubBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create publication status=%d body=%s", rec.Code, rec.Body.String())
	}
	var shell map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &shell); err != nil {
		t.Fatal(err)
	}
	pubID, ok := shell["publication_id"].(string)
	if !ok || pubID == "" {
		t.Fatalf("missing publication_id: %v", shell)
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/integrations/linear/publications?status=pending",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var pending map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pending)
	data := pending["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 pending publication, got %d", len(data))
	}

	credBody, _ := json.Marshal(map[string]any{
		"clientId":      "test-client",
		"clientSecret":  "test-secret",
		"webhookSecret": "lin_wh_test",
		"returnUrl":     "http://localhost/console/integrations",
	})
	req = httptest.NewRequest(
		http.MethodPatch,
		"/v1/integrations/linear/publications/"+pubID+"/credentials",
		bytes.NewReader(credBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("credentials status=%d body=%s", rec.Code, rec.Body.String())
	}

	ruleBody, _ := json.Marshal(map[string]any{
		"filter_label": "bot-ready",
		"name":         "Pickup bot-ready",
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/linear/publications/"+pubID+"/dispatch-rules",
		bytes.NewReader(ruleBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("dispatch rule status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/integrations/linear/publications/"+pubID+"/dispatch-rules",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list rules status=%d", rec.Code)
	}
	var rulesResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rulesResp)
	rules := rulesResp["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	req = httptest.NewRequest(
		http.MethodDelete,
		"/v1/integrations/linear/publications/"+pubID,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpublish status=%d", rec.Code)
	}
}

func TestIntegrationsInstallProxyStartA1(t *testing.T) {
	handler := testRouter(t)
	agentID, envID := integrationTestAgentEnv(t, handler)

	for _, tc := range []struct {
		name     string
		path     string
		wantKeys []string
	}{
		{
			name: "github",
			path: "/v1/integrations/github/start-a1",
			wantKeys: []string{
				"formToken", "publicationId", "appOmaId",
				"manifestStartUrl", "webhookUrl",
			},
		},
		{
			name: "slack",
			path: "/v1/integrations/slack/start-a1",
			wantKeys: []string{
				"formToken", "publicationId",
				"manifestLaunchUrl", "webhookUrl",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"agentId":       agentID,
				"environmentId": envID,
				"personaName":   "Install Bot",
				"returnUrl":     "http://localhost/console/integrations",
			})
			req := httptest.NewRequest(
				http.MethodPost, tc.path, bytes.NewReader(body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			for _, key := range tc.wantKeys {
				if resp[key] == nil || resp[key] == "" {
					t.Fatalf("missing %s in %v", key, resp)
				}
			}
		})
	}
}

func TestIntegrationsInstallProxyLinearLegacy410(t *testing.T) {
	handler := testRouter(t)
	paths := []string{
		"/v1/integrations/linear/start-a1",
		"/v1/integrations/linear/credentials",
		"/v1/integrations/linear/handoff-link",
		"/v1/integrations/linear/personal-token",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost, path,
				bytes.NewBufferString(`{"formToken":"x"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusGone {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestIntegrationsInstallHandoffLink(t *testing.T) {
	handler := testRouter(t)
	agentID, envID := integrationTestAgentEnv(t, handler)

	startBody, _ := json.Marshal(map[string]any{
		"agentId":       agentID,
		"environmentId": envID,
		"personaName":   "Handoff Bot",
		"returnUrl":     "http://localhost/console/integrations",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/slack/start-a1",
		bytes.NewReader(startBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start-a1 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &start)
	formToken := start["formToken"].(string)

	handoffBody, _ := json.Marshal(map[string]any{"formToken": formToken})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/slack/handoff-link",
		bytes.NewReader(handoffBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handoff status=%d body=%s", rec.Code, rec.Body.String())
	}
	var handoff map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &handoff)
	url, _ := handoff["url"].(string)
	if url == "" || handoff["expiresInDays"] == nil {
		t.Fatalf("unexpected handoff response: %v", handoff)
	}
}

func integrationTestAgentEnv(
	t *testing.T,
	handler http.Handler,
) (agentID, envID string) {
	t.Helper()
	createAgent := `{"name":"integrations-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents",
		bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	agentID = agent["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/environments?limit=5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var envs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envs)
	envID = envs["data"].([]any)[0].(map[string]any)["id"].(string)
	return agentID, envID
}
