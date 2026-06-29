package integrationtest

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	cookbookMountPath  = "/mnt/session/uploads/sales_data.csv"
	cookbookReportName = harness.DataAnalystReportFilename
)

//go:embed testdata/sales_data.csv
var SalesDataCSV []byte

// RunDataAnalystCookbookFlow exercises cookbook steps 3–6 against a live router.
func RunDataAnalystCookbookFlow(
	t *testing.T,
	handler http.Handler,
	sim *harness.DataAnalystSimulatingClient,
	csvContent []byte,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	if len(csvContent) == 0 {
		csvContent = SalesDataCSV
	}
	if !bytes.Contains(csvContent, []byte("order_id")) {
		t.Fatal("cookbook CSV fixture missing order_id header")
	}

	fileID := uploadCookbookCSV(t, client, base, csvContent)
	agentID := createCookbookAgent(t, client, base)
	sessionID, scopedFileID := createCookbookSession(
		t, client, base, agentID, fileID,
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	postCookbookMessage(t, client, eventsURL)

	waitForCookbookReply(t, client, eventsURL, 5*time.Second)
	waitForSessionIdle(t, client, base+"/v1/sessions/"+sessionID, 5*time.Second)

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request")
	}
	if last.SessionID != sessionID {
		t.Fatalf("harness session=%q want %q", last.SessionID, sessionID)
	}
	if len(last.Resources) == 0 {
		t.Fatal("expected resolved resources on harness turn")
	}

	files := listSessionFiles(t, client, base, sessionID)
	assertCookbookFileListed(t, files, cookbookReportName, "session output")
	assertCookbookFileListed(t, files, "sales_data.csv", "scoped upload")

	reportID := fileIDByName(t, files, cookbookReportName)
	content := downloadFileContent(t, client, base, reportID)
	if len(content) < 1024 {
		t.Fatalf("report.html size=%d want >= 1KB", len(content))
	}

	scopedContent := downloadFileContent(t, client, base, scopedFileID)
	if !bytes.Contains(scopedContent, []byte("order_id")) {
		t.Fatalf("scoped CSV missing header: %q", truncate(scopedContent, 80))
	}
}

func uploadCookbookCSV(
	t *testing.T,
	client *http.Client,
	base string,
	csv []byte,
) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"filename":     "sales_data.csv",
		"content":      string(csv),
		"media_type":   "text/csv",
		"downloadable": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/files", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var file map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		t.Fatal(err)
	}
	id, _ := file["id"].(string)
	if id == "" {
		t.Fatalf("upload id=%v", file["id"])
	}
	return id
}

func createCookbookAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := []byte(`{
		"name":"data-analyst",
		"model":"claude-sonnet-4-20250514",
		"system_prompt":"Analyze CSV and write report.html to /mnt/session/outputs/"
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

func createCookbookSession(
	t *testing.T,
	client *http.Client,
	base, agentID, fileID string,
) (sessionID, scopedFileID string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"agent": agentID,
		"title": "Sales analysis",
		"resources": []any{
			map[string]any{
				"type":       "file",
				"file_id":    fileID,
				"mount_path": cookbookMountPath,
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
	sessionID, _ = sess["id"].(string)
	if sessionID == "" {
		t.Fatal("missing session id")
	}
	resources, ok := sess["resources"].([]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("session resources=%v", sess["resources"])
	}
	res := resources[0].(map[string]any)
	if res["mount_path"] != cookbookMountPath {
		t.Fatalf("mount_path=%v", res["mount_path"])
	}
	scopedFileID, _ = res["file_id"].(string)
	if scopedFileID == "" || scopedFileID == fileID {
		t.Fatalf("expected scoped file_id, got %q", scopedFileID)
	}
	return sessionID, scopedFileID
}

func postCookbookMessage(t *testing.T, client *http.Client, eventsURL string) {
	t.Helper()
	body := []byte(`{"events":[{"type":"user.message","content":[{"type":"text","text":"Analyze the sales CSV and write report.html"}]}]}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func waitForCookbookReply(
	t *testing.T,
	client *http.Client,
	eventsURL string,
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
					bytes.Contains([]byte(block.Text), []byte(harness.DataAnalystReportMarker)) {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %q reply", harness.DataAnalystReportMarker)
}

func waitForSessionIdle(
	t *testing.T,
	client *http.Client,
	url string,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		var sess map[string]any
		decodeErr := json.NewDecoder(resp.Body).Decode(&sess)
		resp.Body.Close()
		if decodeErr == nil && sess["status"] == "idle" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timeout waiting for session idle")
}

func listSessionFiles(
	t *testing.T,
	client *http.Client,
	base, sessionID string,
) []map[string]any {
	t.Helper()
	resp, err := client.Get(base + "/v1/files?scope_id=" + sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list files status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	raw, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("files data=%v", body["data"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if ok {
			out = append(out, row)
		}
	}
	return out
}

func assertCookbookFileListed(
	t *testing.T,
	files []map[string]any,
	filename, label string,
) {
	t.Helper()
	for _, row := range files {
		if row["filename"] == filename {
			return
		}
	}
	names := make([]string, 0, len(files))
	for _, row := range files {
		n, _ := row["filename"].(string)
		names = append(names, n)
	}
	t.Fatalf("%s %q not listed, got %v", label, filename, names)
}

func fileIDByName(t *testing.T, files []map[string]any, name string) string {
	t.Helper()
	for _, row := range files {
		if row["filename"] == name {
			id, _ := row["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	t.Fatalf("file id for %q not found", name)
	return ""
}

func downloadFileContent(
	t *testing.T,
	client *http.Client,
	base, fileID string,
) []byte {
	t.Helper()
	resp, err := client.Get(base + "/v1/files/" + fileID + "/content")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeEventPayloads(t *testing.T, body io.Reader) []json.RawMessage {
	t.Helper()
	var list struct {
		Data []struct {
			Data json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	out := make([]json.RawMessage, 0, len(list.Data))
	for _, item := range list.Data {
		out = append(out, item.Data)
	}
	return out
}

func doJSON(
	t *testing.T,
	client *http.Client,
	method, url string,
	body []byte,
) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readBody(resp *http.Response) string {
	data, _ := io.ReadAll(resp.Body)
	if len(data) > 512 {
		return string(data[:512]) + "..."
	}
	return string(data)
}

func truncate(data []byte, max int) string {
	if len(data) <= max {
		return string(data)
	}
	return string(data[:max]) + "..."
}
