package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

func seedInternalTestUser(
	t *testing.T,
	db *sql.DB,
	userID, tenantID string,
) {
	t.Helper()
	now := time.Now().UnixMilli()
	_, err := db.Exec(`
		INSERT OR IGNORE INTO tenant (id, name, "createdAt", "updatedAt")
		VALUES (?, ?, ?, ?)`,
		tenantID, "Internal Test", now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT OR IGNORE INTO membership (user_id, tenant_id, role, created_at)
		VALUES (?, ?, 'owner', ?)`,
		userID, tenantID, now,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInternalCreateSessionAndEvents(t *testing.T) {
	handler, db := testRouterInternalWithDB(t)
	seedInternalTestUser(t, db, "usr-internal", "default")

	agentID := createTestAgent(t, handler)

	envs := store.NewEnvironmentRepo(db)
	env, err := envs.Get(context.Background(), "default", store.DefaultEnvironmentID)
	if err != nil || env == nil {
		t.Fatal("default environment missing")
	}

	createBody := map[string]any{
		"action":        "create",
		"userId":        "usr-internal",
		"agentId":       agentID,
		"environmentId": env.ID,
		"additionalSystemPrompt": "Signal protocol catalog",
		"mcpServers": []map[string]string{
			{"name": "slack", "url": "https://integrations.test/slack/mcp"},
		},
		"initialEvent": map[string]any{
			"type": "user.message",
			"content": []map[string]string{
				{"type": "text", "text": "hello from internal"},
			},
		},
	}
	raw, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/sessions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-internal-secret", testInternalSecret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	sessionID := created["sessionId"]
	if sessionID == "" {
		t.Fatal("missing sessionId")
	}

	getReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/internal/sessions/"+sessionID,
		nil,
	)
	getReq.Header.Set("x-internal-secret", testInternalSecret)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var sess map[string]any
	_ = json.Unmarshal(getRec.Body.Bytes(), &sess)
	agent := sess["agent"].(map[string]any)
	system, _ := agent["system"].(string)
	if system == "" {
		system, _ = agent["system_prompt"].(string)
	}
	if system == "" || !strings.Contains(system, "Signal protocol catalog") {
		t.Fatalf("expected augmented system prompt, agent=%v", agent)
	}

	eventBody := map[string]any{
		"userId": "usr-internal",
		"event": map[string]any{
			"type": "user.message",
			"content": []map[string]string{
				{"type": "text", "text": "follow up"},
			},
		},
	}
	eventRaw, _ := json.Marshal(eventBody)
	eventReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/internal/sessions/"+sessionID+"/events",
		bytes.NewReader(eventRaw),
	)
	eventReq.Header.Set("Content-Type", "application/json")
	eventReq.Header.Set("x-internal-secret", testInternalSecret)
	eventRec := httptest.NewRecorder()
	handler.ServeHTTP(eventRec, eventReq)
	if eventRec.Code != http.StatusOK {
		t.Fatalf("event status=%d body=%s", eventRec.Code, eventRec.Body.String())
	}
}

func TestInternalVaultCreateAndRotate(t *testing.T) {
	handler, db := testRouterInternalWithDB(t)
	seedInternalTestUser(t, db, "usr-vault", "default")

	createBody := map[string]any{
		"action":       "create_with_credential",
		"userId":       "usr-vault",
		"vaultName":    "linear-vault",
		"displayName":  "Linear MCP",
		"mcpServerUrl": "https://integrations.test/linear/mcp",
		"bearerToken":  "tok-initial",
		"provider":     "linear",
	}
	raw, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/vaults", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-internal-secret", testInternalSecret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create vault status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created["vaultId"] == "" || created["credentialId"] == "" {
		t.Fatalf("response=%v", created)
	}

	rotateBody := map[string]any{
		"action":   "rotate_bearer",
		"userId":   "usr-vault",
		"vaultId":  created["vaultId"],
		"newToken": "tok-rotated",
	}
	rotateRaw, _ := json.Marshal(rotateBody)
	rotateReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/internal/vaults/rotate",
		bytes.NewReader(rotateRaw),
	)
	rotateReq.Header.Set("Content-Type", "application/json")
	rotateReq.Header.Set("x-internal-secret", testInternalSecret)
	rotateRec := httptest.NewRecorder()
	handler.ServeHTTP(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rotateRec.Code, rotateRec.Body.String())
	}
}

func createTestAgent(t *testing.T, handler http.Handler) string {
	t.Helper()
	body := `{"name":"internal-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("missing agent id")
	}
	return id
}
