package integrationtest

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

const exploreDeployHistoryContent = "# DEPLOY HISTORY\n" +
	"2026-03-01: monolith -> microservices migration complete\n"

// RunExploreUnfamiliarCodebaseFlow exercises CMA_explore_unfamiliar_codebase:
// zip mount, exploration turns, mid-session resources.add/delete.
func RunExploreUnfamiliarCodebaseFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.ExploreSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	repoZip := makeUnfamiliarRepoZip(t)
	repoFileID := uploadExploreFile(
		t, client, base, "repo.zip", repoZip, "application/zip",
	)
	deployFileID := uploadExploreFile(
		t, client, base, "DEPLOY_HISTORY.md",
		[]byte(exploreDeployHistoryContent), "text/markdown",
	)

	agentID := createExploreAgent(t, client, base)
	envID := createExploreEnvironment(t, client, base)
	sessionID := createExploreSession(
		t, client, base, agentID, envID, repoFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	postExploreArchitectureMessage(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.ExploreArchitectureMarker, 5*time.Second,
	)
	waitForSessionIdle(
		t, client, base+"/v1/sessions/"+sessionID, 5*time.Second,
	)

	postExploreNotesMessage(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.ExploreNotesMarker, 5*time.Second,
	)
	waitForSessionIdle(
		t, client, base+"/v1/sessions/"+sessionID, 5*time.Second,
	)

	addedID := addExploreSessionResource(
		t, client, base, sessionID, deployFileID,
		demo.ExploreDeployHistoryMountPath,
	)
	if count := listExploreSessionResources(t, client, base, sessionID); count != 2 {
		t.Fatalf("resources after add=%d want 2", count)
	}

	postExploreDeployFollowUp(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.ExploreDeployMarker, 5*time.Second,
	)
	waitForSessionIdle(
		t, client, base+"/v1/sessions/"+sessionID, 5*time.Second,
	)

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request for deploy follow-up")
	}
	if _, found := fileResourceInTurn(last.Resources, demo.ExploreDeployHistoryMountPath); !found {
		t.Fatal("deploy history not mounted on turn 3 harness request")
	}

	deleteExploreSessionResource(t, client, base, sessionID, addedID)
	if count := listExploreSessionResources(t, client, base, sessionID); count != 1 {
		t.Fatalf("resources after delete=%d want 1", count)
	}
}

func makeUnfamiliarRepoZip(t *testing.T) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	writes := map[string]string{
		"ARCHITECTURE.md": `# Architecture (STALE, do not trust)

The app is a monolith with three layers:
- api/ for REST handlers
- core/ for business logic
- db/ for database access
`,
		"README.md": "# Widget Service\n\nSee ARCHITECTURE.md (possibly outdated).\n",
	}
	for svc := range map[string]struct{}{
		"auth": {}, "billing": {}, "notifications": {}, "widgets": {},
	} {
		writes["services/"+svc+"/main.py"] = "# " + svc + " service\n"
		writes["services/"+svc+"/models.py"] = "class " + svc + ": ...\n"
	}
	for path, body := range writes {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadExploreFile(
	t *testing.T,
	client *http.Client,
	base, filename string,
	content []byte,
	mediaType string,
) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"filename":     filename,
		"content":      base64.StdEncoding.EncodeToString(content),
		"media_type":   mediaType,
		"encoding":     "base64",
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
		t.Fatalf("missing file id for %s", filename)
	}
	return id
}

func createExploreAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"cookbook-explore",
		"model":"faux/test",
		"system_prompt":"Explore unfamiliar codebases; verify docs against code."
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

func createExploreEnvironment(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"cookbook-explore-env",
		"config":{"type":"cloud","networking":{"type":"limited"}}
	}`)
	resp := doJSON(t, client, http.MethodPost, base+"/v1/environments", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("env status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var env map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	id, _ := env["id"].(string)
	if id == "" {
		t.Fatal("missing environment id")
	}
	return id
}

func createExploreSession(
	t *testing.T,
	client *http.Client,
	base, agentID, envID, repoFileID string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent":          agentID,
		"environment_id": envID,
		"title":          "Onboard to repo",
		"resources": []any{
			map[string]any{
				"type":       "file",
				"file_id":    repoFileID,
				"mount_path": demo.ExploreRepoMountPath,
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
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatal("missing session id")
	}
	return id
}

func postExploreArchitectureMessage(
	t *testing.T,
	client *http.Client,
	eventsURL string,
) {
	t.Helper()
	body := []byte(`{
		"events":[{
			"type":"user.message",
			"content":[{
				"type":"text",
				"text":"Unzip /mnt/session/uploads/repo.zip to /tmp/repo/. Then: what is the actual architecture of this codebase? Be specific about directory structure. Check if the docs are accurate."
			}]
		}]
	}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("explore message status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postExploreNotesMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	body := []byte(`{
		"events":[{
			"type":"user.message",
			"content":[{"type":"text","text":"cat /tmp/NOTES.md"}]
		}]
	}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notes message status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postExploreDeployFollowUp(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	body := []byte(`{
		"events":[{
			"type":"user.message",
			"content":[{
				"type":"text",
				"text":"There's a DEPLOY_HISTORY.md in your workspace now. Read it and tell me whether it changes anything in your earlier answer."
			}]
		}]
	}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("deploy follow-up status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func addExploreSessionResource(
	t *testing.T,
	client *http.Client,
	base, sessionID, fileID, mountPath string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type":       "file",
		"file_id":    fileID,
		"mount_path": mountPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	url := base + "/v1/sessions/" + sessionID + "/resources"
	resp := doJSON(t, client, http.MethodPost, url, payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add resource status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	id, _ := res["id"].(string)
	if id == "" {
		t.Fatal("missing added resource id")
	}
	mp, _ := res["mount_path"].(string)
	if mp != mountPath {
		t.Fatalf("mount_path=%q want %q", mp, mountPath)
	}
	return id
}

func listExploreSessionResources(
	t *testing.T,
	client *http.Client,
	base, sessionID string,
) int {
	t.Helper()
	url := base + "/v1/sessions/" + sessionID + "/resources"
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list resources status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var listed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	data, _ := listed["data"].([]any)
	return len(data)
}

func deleteExploreSessionResource(
	t *testing.T,
	client *http.Client,
	base, sessionID, resourceID string,
) {
	t.Helper()
	url := base + "/v1/sessions/" + sessionID + "/resources/" + resourceID
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete resource status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func fileResourceInTurn(resources []json.RawMessage, mountPath string) (map[string]any, bool) {
	for _, raw := range resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		if res["type"] != "file" {
			continue
		}
		mp, _ := res["mount_path"].(string)
		if mp == mountPath {
			return res, true
		}
	}
	return nil, false
}
