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

func TestIntegrationsInstallProxyNotConfigured(t *testing.T) {
	handler := testRouter(t)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/slack/start-a1",
		bytes.NewBufferString(`{"agentId":"a","environmentId":"e"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
