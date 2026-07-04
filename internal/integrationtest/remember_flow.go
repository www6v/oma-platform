package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

const rememberStoreName = "user-preferences"

// RunRememberPreferencesCookbookFlow exercises CMA_remember_user_preferences:
// session 1 saves a preference to memory, session 2 recalls it.
func RunRememberPreferencesCookbookFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.RememberSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	storeID := createRememberMemoryStore(t, client, base)
	agentID := createRememberAgent(t, client, base)
	session1 := createRememberSession(
		t, client, base, agentID, storeID,
		"Save my formatting preference",
	)

	events1URL := base + "/v1/sessions/" + session1 + "/events"
	postRememberSaveMessage(t, client, events1URL)
	waitForEventMarker(
		t, client, events1URL, demo.RememberSaveMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, base+"/v1/sessions/"+session1, 5*time.Second)

	last1, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request for session 1")
	}
	if last1.SessionID != session1 {
		t.Fatalf("session1 harness=%q want %q", last1.SessionID, session1)
	}

	assertRememberMemoryPersisted(t, client, base, storeID)

	session2 := createRememberSession(
		t, client, base, agentID, storeID,
		"Recall formatting preference",
	)
	events2URL := base + "/v1/sessions/" + session2 + "/events"
	postRememberRecallMessage(t, client, events2URL)
	waitForEventMarker(
		t, client, events2URL, demo.RememberRecallMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, base+"/v1/sessions/"+session2, 5*time.Second)

	last2, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request for session 2")
	}
	if last2.SessionID != session2 {
		t.Fatalf("session2 harness=%q want %q", last2.SessionID, session2)
	}
	content, found := memoryContentInTurnResources(
		last2.Resources, demo.PreferenceMemoryPath,
	)
	if !found || !strings.Contains(content, "bullet points") {
		t.Fatalf("session2 memory=%q found=%v", content, found)
	}
}

func createRememberMemoryStore(
	t *testing.T,
	client *http.Client,
	base string,
) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"name":        rememberStoreName,
		"description": "User formatting and communication preferences",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/memory_stores", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create store status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var store map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&store); err != nil {
		t.Fatal(err)
	}
	id, _ := store["id"].(string)
	if id == "" {
		t.Fatal("missing memory store id")
	}
	return id
}

func createRememberAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"preference-assistant",
		"model":"faux/test",
		"system_prompt":"You help users save and recall preferences via /mnt/memory/."
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

func createRememberSession(
	t *testing.T,
	client *http.Client,
	base, agentID, storeID, title string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent": agentID,
		"title": title,
		"resources": []any{
			map[string]any{
				"type":            "memory_store",
				"memory_store_id": storeID,
				"access":          "read_write",
				"instructions": "When the user states a formatting preference, write it to " +
					"/mnt/memory/user-preferences/preferences/formatting.md",
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

func postRememberSaveMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	body := []byte(`{"events":[{"type":"user.message","content":[{"type":"text","text":"Please remember: I prefer bullet points and concise replies for all summaries."}]}]}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postRememberRecallMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	body := []byte(`{"events":[{"type":"user.message","content":[{"type":"text","text":"What formatting preference did I ask you to remember?"}]}]}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func assertRememberMemoryPersisted(
	t *testing.T,
	client *http.Client,
	base, storeID string,
) {
	t.Helper()
	resp, err := client.Get(
		base + "/v1/memory_stores/" + storeID + "/memories",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list memories status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("memories=%v want non-empty after session 1", body["data"])
	}
}

func memoryContentInTurnResources(
	resources []json.RawMessage,
	memPath string,
) (string, bool) {
	for _, raw := range resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		if res["type"] != "memory_store" {
			continue
		}
		items, ok := res["memories"].([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			path, _ := row["path"].(string)
			if path != memPath {
				continue
			}
			content, _ := row["content"].(string)
			if content != "" {
				return content, true
			}
		}
	}
	return "", false
}
