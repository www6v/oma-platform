package integrationtest

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

//go:embed all:testdata/orchestrate
var orchestrateFixtures embed.FS

const orchestrateFixtureRoot = "testdata/orchestrate"

// RunOrchestrateIssueToPRFlow exercises CMA_orchestrate_issue_to_pr:
// zip mount with mock gh, full chain turn, PR state verification turn.
func RunOrchestrateIssueToPRFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.OrchestrateSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	repoZip := makeOrchestrateRepoZip(t)
	repoFileID := uploadOrchestrateFile(
		t, client, base, "repo.zip", repoZip, "application/zip",
	)

	agentID := createOrchestrateAgent(t, client, base)
	envID := createOrchestrateEnvironment(t, client, base)
	sessionID := createOrchestrateSession(
		t, client, base, agentID, envID, repoFileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID

	postOrchestrateChainMessage(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.OrchestrateTurn1Marker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 1 {
		t.Fatalf("after turn 1 harness turns=%d want 1", sim.TurnCount())
	}

	postOrchestrateVerifyMessage(t, client, eventsURL)
	waitForEventMarker(
		t, client, eventsURL, demo.OrchestrateVerifyMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	if sim.TurnCount() != 2 {
		t.Fatalf("after turn 2 harness turns=%d want 2", sim.TurnCount())
	}

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request for verify turn")
	}
	if last.SessionID != sessionID {
		t.Fatalf("harness session=%q want %q", last.SessionID, sessionID)
	}
	if len(last.Resources) < 1 {
		t.Fatalf("expected repo.zip resource on verify turn, got %d", len(last.Resources))
	}
}

func makeOrchestrateRepoZip(t *testing.T) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	err := fs.WalkDir(
		orchestrateFixtures,
		orchestrateFixtureRoot,
		func(path string, info fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if info.Name() == "README.md" {
				return nil
			}
			rel, err := filepath.Rel(orchestrateFixtureRoot, path)
			if err != nil {
				return err
			}
			data, err := orchestrateFixtures.ReadFile(path)
			if err != nil {
				return err
			}
			w, err := zw.Create(filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			_, err = w.Write(data)
			return err
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadOrchestrateFile(
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

func createOrchestrateAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"cookbook-orchestrate",
		"model":"faux/test",
		"system_prompt":"Maintainer bot: read issues via gh-mock, fix code, shepherd PRs through CI and review."
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

func createOrchestrateEnvironment(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"cookbook-orchestrate-env",
		"config":{
			"type":"cloud",
			"networking":{"type":"limited","allow_package_managers":true},
			"packages":{"pip":["pytest"]}
		}
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

func createOrchestrateSession(
	t *testing.T,
	client *http.Client,
	base, agentID, envID, repoFileID string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent":          agentID,
		"environment_id": envID,
		"title":          "Issue #42 → PR",
		"resources": []any{
			map[string]any{
				"type":       "file",
				"file_id":    repoFileID,
				"mount_path": demo.OrchestrateRepoMountPath,
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

func postOrchestrateChainMessage(
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
				"text":"Unpack /mnt/session/uploads/repo.zip into /mnt/user and ship a fix for issue #42 end-to-end. Read the ./gh-mock script first to see what subcommands it supports; use those to view the issue, open a PR, run CI, handle review feedback, and merge. Show me the final PR state when you're done."
			}]
		}]
	}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("chain message status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postOrchestrateVerifyMessage(
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
				"text":"Print the contents of /mnt/user/.gh-state/pr_101.json so I can see the final state, CI status, and reviews."
			}]
		}]
	}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("verify message status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}
