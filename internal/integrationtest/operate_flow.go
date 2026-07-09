package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

type mcpAuthRecorder struct {
	mu   sync.Mutex
	last string
}

func newMCPAuthRecorder() (*httptest.Server, *mcpAuthRecorder) {
	rec := &mcpAuthRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.last = r.Header.Get("Authorization")
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	return srv, rec
}

func (r *mcpAuthRecorder) lastAuth() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// RunOperateInProductionFlow exercises CMA_operate_in_production: vault +
// credential + session vault_ids + MCP proxy auth + harness turn.
func RunOperateInProductionFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.OperateSimulatingClient,
) {
	t.Helper()

	mockMCP, authRec := newMCPAuthRecorder()
	defer mockMCP.Close()
	mcpURL := mockMCP.URL

	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	vaultAID, vaultBID := createOperateVaultPair(
		t, client, base, mcpURL,
	)
	agentID := createOperateAgent(t, client, base, mcpURL)
	sessionID := createOperateSession(
		t, client, base, agentID, []string{vaultAID},
	)

	sess := getSession(t, client, base, sessionID)
	vaultIDs, ok := sess["vault_ids"].([]any)
	if !ok || len(vaultIDs) != 1 || vaultIDs[0] != vaultAID {
		t.Fatalf("vault_ids=%v want [%s]", sess["vault_ids"], vaultAID)
	}

	callMcpProxy(t, client, base, sessionID, "github")
	if authRec.lastAuth() != "Bearer token-a" {
		t.Fatalf(
			"vault A auth=%q want Bearer token-a",
			authRec.lastAuth(),
		)
	}

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID
	postOperateMessage(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.OperateMcpMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 1 {
		t.Fatalf("harness turns=%d want 1", sim.TurnCount())
	}

	sessionB := createOperateSession(
		t, client, base, agentID, []string{vaultBID},
	)
	callMcpProxy(t, client, base, sessionB, "github")
	if authRec.lastAuth() != "Bearer token-b" {
		t.Fatalf(
			"vault B auth=%q want Bearer token-b",
			authRec.lastAuth(),
		)
	}
}

func createOperateVaultPair(
	t *testing.T,
	client *http.Client,
	base, mcpURL string,
) (vaultAID, vaultBID string) {
	t.Helper()
	vaultAID = createOperateVaultWithCredential(
		t, client, base, "operate-vault-a", mcpURL, "token-a",
	)
	vaultBID = createOperateVaultWithCredential(
		t, client, base, "operate-vault-b", mcpURL, "token-b",
	)
	return vaultAID, vaultBID
}

func createOperateVaultWithCredential(
	t *testing.T,
	client *http.Client,
	base, name, mcpURL, token string,
) string {
	t.Helper()
	resp := doJSON(
		t, client, http.MethodPost, base+"/v1/vaults",
		[]byte(`{"name":"`+name+`"}`),
	)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("vault create status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var vault map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&vault); err != nil {
		t.Fatal(err)
	}
	vaultID, _ := vault["id"].(string)
	if vaultID == "" {
		t.Fatal("missing vault id")
	}

	credBody, err := json.Marshal(map[string]any{
		"display_name": "GitHub MCP",
		"auth": map[string]any{
			"type":           "static_bearer",
			"mcp_server_url": mcpURL,
			"token":          token,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	credResp := doJSON(
		t, client, http.MethodPost,
		base+"/v1/vaults/"+vaultID+"/credentials",
		credBody,
	)
	defer credResp.Body.Close()
	if credResp.StatusCode != http.StatusCreated {
		t.Fatalf(
			"credential status=%d body=%s",
			credResp.StatusCode, readBody(credResp),
		)
	}
	return vaultID
}

func createOperateAgent(
	t *testing.T,
	client *http.Client,
	base, mcpURL string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"name":  "operate-production",
		"model": "claude-sonnet-4-20250514",
		"system_prompt": "Use GitHub MCP to list repositories for the user.",
		"mcp_servers": []any{
			map[string]any{
				"name": "github",
				"type": "url",
				"url":  mcpURL,
			},
		},
		"tools": []any{
			map[string]any{
				"type":            "mcp_toolset",
				"mcp_server_name": "github",
				"default_config": map[string]any{
					"permission_policy": map[string]any{
						"type": "always_allow",
					},
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

func createOperateSession(
	t *testing.T,
	client *http.Client,
	base, agentID string,
	vaultIDs []string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent":      agentID,
		"title":      "Operate in production",
		"vault_ids":  vaultIDs,
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

func getSession(
	t *testing.T,
	client *http.Client,
	base, sessionID string,
) map[string]any {
	t.Helper()
	resp, err := client.Get(base + "/v1/sessions/" + sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var sess map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func callMcpProxy(
	t *testing.T,
	client *http.Client,
	base, sessionID, serverName string,
) {
	t.Helper()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	resp := doJSON(
		t, client, http.MethodPost,
		base+"/v1/mcp-proxy/"+sessionID+"/"+serverName,
		body,
	)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcp-proxy status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postOperateMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	body := []byte(`{
		"events":[{
			"type":"user.message",
			"content":[{
				"type":"text",
				"text":"List my GitHub repositories using the github MCP toolset."
			}]
		}]
	}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}
