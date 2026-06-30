package integrationtest

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	iterateCalcMount  = "calc.py"
	iterateTestMount  = "test_calc.py"
	iterateOutputName = harness.IterateOutputFilename
)

//go:embed testdata/iterate/calc.py
var IterateCalcFixture []byte

//go:embed testdata/iterate/test_calc.py
var IterateTestFixture []byte

// RunIterateCookbookFlow exercises iterate cookbook steps 3–6 (MT1):
// upload fixtures → session with two resources → turn 1 → turn 2 verify →
// files.list(scope_id) for calc.py output. Asserts two harness turns on one session.
func RunIterateCookbookFlow(
	t *testing.T,
	handler http.Handler,
	sim *harness.IterateSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	if !bytes.Contains(IterateCalcFixture, []byte("BUG")) {
		t.Fatal("iterate calc fixture missing BUG marker")
	}
	if !bytes.Contains(IterateTestFixture, []byte("test_add")) {
		t.Fatal("iterate test fixture missing test_add")
	}

	calcFileID := uploadIterateFile(
		t, client, base, iterateCalcMount, IterateCalcFixture, "text/x-python",
	)
	testFileID := uploadIterateFile(
		t, client, base, iterateTestMount, IterateTestFixture, "text/x-python",
	)
	agentID := createIterateAgent(t, client, base)
	sessionID := createIterateSession(
		t, client, base, agentID, calcFileID, testFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postIterateMessage(
		t, client, eventsURL,
		"Fix calc.py until tests pass and write /mnt/session/outputs/calc.py",
	)
	waitForEventMarker(
		t, client, eventsURL, harness.IterateTurn1Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 1 {
		t.Fatalf("after turn 1 harness turns=%d want 1", sim.TurnCount())
	}

	postIterateMessage(
		t, client, eventsURL,
		"Re-run assertions and cat /mnt/session/outputs/calc.py",
	)
	waitForEventMarker(
		t, client, eventsURL, harness.IterateTurn2Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 2 {
		t.Fatalf("after turn 2 harness turns=%d want 2 (MT1)", sim.TurnCount())
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

	files := listSessionFiles(t, client, base, sessionID)
	assertSessionOutputListed(t, files, iterateOutputName, "session output")
	assertCookbookFileListed(t, files, iterateCalcMount, "scoped calc upload")
	assertCookbookFileListed(t, files, iterateTestMount, "scoped test upload")

	calcID := sessionOutputFileIDByName(t, files, iterateOutputName)
	content := downloadFileContent(t, client, base, calcID)
	if bytes.Contains(content, []byte("BUG")) {
		t.Fatalf("output calc.py still buggy: %q", truncate(content, 120))
	}
	if !bytes.Contains(content, []byte("division by zero")) {
		t.Fatalf("output calc.py missing zero check: %q", truncate(content, 120))
	}
}

func uploadIterateFile(
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

func createIterateAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"cookbook-iterate",
		"model":"claude-sonnet-4-20250514",
		"system_prompt":"Make failing tests pass."
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

func createIterateSession(
	t *testing.T,
	client *http.Client,
	base, agentID, calcFileID, testFileID string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent": agentID,
		"title": "Get the tests green",
		"resources": []any{
			map[string]any{
				"type":       "file",
				"file_id":    calcFileID,
				"mount_path": iterateCalcMount,
			},
			map[string]any{
				"type":       "file",
				"file_id":    testFileID,
				"mount_path": iterateTestMount,
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
	resources, ok := sess["resources"].([]any)
	if !ok || len(resources) != 2 {
		t.Fatalf("session resources=%v", sess["resources"])
	}
	return sessionID
}

func postIterateMessage(
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

func waitForEventMarker(
	t *testing.T,
	client *http.Client,
	eventsURL, marker string,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(eventsURL + "?order=asc")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(25 * time.Millisecond)
			continue
		}
		payloads := decodeEventPayloads(t, resp.Body)
		resp.Body.Close()
		for _, payload := range payloads {
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
			if meta.Type != "agent.message" {
				continue
			}
			for _, block := range meta.Content {
				if block.Type == "text" &&
					bytes.Contains([]byte(block.Text), []byte(marker)) {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for marker %q", marker)
}
