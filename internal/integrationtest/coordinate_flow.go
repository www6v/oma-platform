package integrationtest

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness/demo"
)

const (
	coordinateProductMount = "/mnt/user-data/product_one_pager.md"
	coordinatePricingMount = "/mnt/user-data/pricing_rules.md"
	coordinateCaseMount    = "/mnt/user-data/case_studies"
)

//go:embed testdata/coordinate/product_one_pager.md
var CoordinateProductOnePager []byte

//go:embed testdata/coordinate/pricing_rules.md
var CoordinatePricingRules []byte

//go:embed testdata/coordinate/regional_health.md
var CoordinateRegionalHealth []byte

//go:embed testdata/coordinate/metro_clinic.md
var CoordinateMetroClinic []byte

// RunCoordinateSpecialistTeamFlow exercises CMA_coordinate_specialist_team:
// three specialists wired via multiagent, session resources, thread events.
func RunCoordinateSpecialistTeamFlow(
	t *testing.T,
	handler http.Handler,
	sim *demo.CoordinateSimulatingClient,
) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	base := server.URL

	productID := uploadCoordinateFile(
		t, client, base, "product_one_pager.md",
		CoordinateProductOnePager, "text/markdown",
	)
	pricingID := uploadCoordinateFile(
		t, client, base, "pricing_rules.md",
		CoordinatePricingRules, "text/markdown",
	)
	regionalID := uploadCoordinateFile(
		t, client, base, "regional_health.md",
		CoordinateRegionalHealth, "text/markdown",
	)
	metroID := uploadCoordinateFile(
		t, client, base, "metro_clinic.md",
		CoordinateMetroClinic, "text/markdown",
	)

	researcherID, researcherVer := createCoordinateSpecialist(
		t, client, base,
		demo.SpecialistResearcher,
		"Research the prospect using web search and return JSON priorities.",
	)
	librarianID, librarianVer := createCoordinateSpecialist(
		t, client, base,
		demo.SpecialistLibrarian,
		"Read case studies under /mnt/user-data/case_studies and pick the best two.",
	)
	pricerID, pricerVer := createCoordinateSpecialist(
		t, client, base,
		demo.SpecialistPricer,
		"Apply pricing_rules.md and return two pricing options as JSON.",
	)
	coordinatorID := createCoordinateCoordinator(
		t, client, base,
		[]coordinateWorkerRef{
			{researcherID, researcherVer},
			{librarianID, librarianVer},
			{pricerID, pricerVer},
		},
	)

	sessionID := createCoordinateSession(
		t, client, base, coordinatorID,
		[]coordinateResource{
			{productID, coordinateProductMount},
			{pricingID, coordinatePricingMount},
			{regionalID, coordinateCaseMount + "/regional_health.md"},
			{metroID, coordinateCaseMount + "/metro_clinic.md"},
		},
	)

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	sessionURL := base + "/v1/sessions/" + sessionID
	postCoordinateProspectMessage(t, client, eventsURL)

	waitForEventMarker(
		t, client, eventsURL, demo.CoordinateCompleteMarker, 5*time.Second,
	)
	waitForSessionIdle(t, client, sessionURL, 5*time.Second)

	last, ok := sim.LastRequest()
	if !ok {
		t.Fatal("expected harness turn request")
	}
	if last.SessionID != sessionID {
		t.Fatalf("harness session=%q want %q", last.SessionID, sessionID)
	}
	if len(last.Resources) < 4 {
		t.Fatalf("resources=%d want >=4", len(last.Resources))
	}
	if len(last.SubAgents) < 3 {
		t.Fatalf("sub_agents=%d want >=3", len(last.SubAgents))
	}

	assertCoordinateThreadEvents(t, client, eventsURL)
	assertCoordinateSessionThreads(
		t, client, base, sessionID,
		[]string{researcherID, librarianID, pricerID},
	)
}

type coordinateWorkerRef struct {
	id      string
	version int
}

type coordinateResource struct {
	fileID    string
	mountPath string
}

func uploadCoordinateFile(
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
		t.Fatalf("upload id=%v", file["id"])
	}
	return id
}

func createCoordinateSpecialist(
	t *testing.T,
	client *http.Client,
	base, name, systemPrompt string,
) (id string, version int) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"name":          name,
		"model":         "faux/test",
		"system_prompt": systemPrompt,
		"description":   name + " specialist for proposal coordination.",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/agents", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create specialist %s status=%d body=%s", name, resp.StatusCode, readBody(resp))
	}
	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	id, _ = agent["id"].(string)
	if id == "" {
		t.Fatalf("specialist id=%v", agent["id"])
	}
	version = int(agent["version"].(float64))
	return id, version
}

func createCoordinateCoordinator(
	t *testing.T,
	client *http.Client,
	base string,
	workers []coordinateWorkerRef,
) string {
	t.Helper()
	agents := make([]map[string]any, 0, len(workers))
	for _, w := range workers {
		agents = append(agents, map[string]any{
			"type":    "agent",
			"id":      w.id,
			"version": w.version,
		})
	}
	payload, err := json.Marshal(map[string]any{
		"name": "proposal_writer",
		"model": "faux/test",
		"system_prompt": "You coordinate prospect_researcher, case_study_picker, and " +
			"pricing_modeler to draft proposal.md.",
		"multiagent": map[string]any{
			"type":   "coordinator",
			"agents": agents,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, client, http.MethodPost, base+"/v1/agents", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create coordinator status=%d body=%s", resp.StatusCode, readBody(resp))
	}
	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	id, _ := agent["id"].(string)
	if id == "" {
		t.Fatalf("coordinator id=%v", agent["id"])
	}
	callable, ok := agent["callable_agents"].([]any)
	if !ok || len(callable) != 3 {
		t.Fatalf("callable_agents=%v want 3 entries", agent["callable_agents"])
	}
	return id
}

func createCoordinateSession(
	t *testing.T,
	client *http.Client,
	base, agentID string,
	resources []coordinateResource,
) string {
	t.Helper()
	resPayload := make([]any, 0, len(resources))
	for _, res := range resources {
		resPayload = append(resPayload, map[string]any{
			"type":       "file",
			"file_id":    res.fileID,
			"mount_path": res.mountPath,
		})
	}
	payload, err := json.Marshal(map[string]any{
		"agent":     agentID,
		"title":     "NorthStar proposal coordination",
		"resources": resPayload,
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

func postCoordinateProspectMessage(
	t *testing.T,
	client *http.Client,
	eventsURL string,
) {
	t.Helper()
	body := []byte(`{"events":[{"type":"user.message","content":[{"type":"text","text":"Draft a sales proposal for NorthStar Health targeting a 2,000-bed regional system. Delegate research, case studies, and pricing to your specialists."}]}]}`)
	resp := doJSON(t, client, http.MethodPost, eventsURL, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", resp.StatusCode, readBody(resp))
	}
}

func assertCoordinateThreadEvents(
	t *testing.T,
	client *http.Client,
	eventsURL string,
) {
	t.Helper()
	payloads := listEventPayloads(t, client, eventsURL)
	threadCreated := 0
	threadReceived := 0
	sawComplete := false
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
		switch meta.Type {
		case "session.thread_created":
			threadCreated++
		case "agent.thread_message_received":
			threadReceived++
		case "agent.message":
			for _, block := range meta.Content {
				if block.Type == "text" &&
					strings.Contains(block.Text, demo.CoordinateCompleteMarker) {
					sawComplete = true
				}
			}
		}
	}
	if threadCreated < 3 {
		t.Fatalf("thread_created=%d want >=3", threadCreated)
	}
	if threadReceived < 3 {
		t.Fatalf("thread_message_received=%d want >=3", threadReceived)
	}
	if !sawComplete {
		t.Fatalf("missing coordinator reply with %q", demo.CoordinateCompleteMarker)
	}
}

func assertCoordinateSessionThreads(
	t *testing.T,
	client *http.Client,
	base, sessionID string,
	specialistIDs []string,
) {
	t.Helper()
	resp, err := client.Get(base + "/v1/sessions/" + sessionID + "/threads")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("threads status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) < 4 {
		t.Fatalf("threads data=%v want >=4", body["data"])
	}
	seen := make(map[string]struct{})
	for _, item := range data {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		agentID, _ := row["agent_id"].(string)
		if agentID != "" {
			seen[agentID] = struct{}{}
		}
	}
	for _, id := range specialistIDs {
		if _, ok := seen[id]; !ok {
			t.Fatalf("threads missing specialist %s; seen=%v", id, mapKeysString(seen))
		}
	}
}

func mapKeysString(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
