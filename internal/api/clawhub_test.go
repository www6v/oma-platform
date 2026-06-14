package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

func TestClawhubSearchFiltersSkillFamily(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	skillFiles := store.NewSkillFileStore(t.TempDir())
	skills := store.NewSkillRepo(db, skillFiles)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/packages" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]string{
				{
					"name":          "skill-a",
					"displayName":   "Skill A",
					"summary":       "desc",
					"family":        "skill",
					"latestVersion": "1.0.0",
					"ownerHandle":   "owner",
				},
				{
					"name":   "not-skill",
					"family": "plugin",
				},
			},
		})
	}))
	defer upstream.Close()

	client := upstream.Client()
	clawhubAPIBaseOverride = upstream.URL + "/api/v1"
	defer func() { clawhubAPIBaseOverride = "" }()

	r := chi.NewRouter()
	mountClawhubRoutes(r, clawhubDeps{
		Skills:     skills,
		SkillFiles: skillFiles,
		HTTPClient: client,
	})

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0]["slug"] != "skill-a" {
		t.Fatalf("data=%v", body.Data)
	}
}
