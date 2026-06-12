package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/modelslist"
)

type modelsListDeps struct {
	Client *modelslist.Client
}

func mountModelsListRoutes(r chi.Router, deps modelsListDeps) {
	client := deps.Client
	if client == nil {
		client = modelslist.DefaultClient
	}

	r.Route("/v1/models", func(r chi.Router) {
		r.Get("/list", handleModelsListCatalogStub)
		r.Post("/list", func(w http.ResponseWriter, req *http.Request) {
			handleModelsListPost(w, req, client)
		})
	})
}

func handleModelsListPost(
	w http.ResponseWriter,
	req *http.Request,
	client *modelslist.Client,
) {
	var body struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.APIKey == "" {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}

	provider := body.Provider
	if provider == "" {
		provider = "ant"
	}

	models, err := client.Fetch(req.Context(), provider, body.APIKey)
	if err != nil {
		writeError(
			w,
			http.StatusBadGateway,
			"Failed to fetch models: "+err.Error(),
		)
		return
	}
	if models == nil {
		models = []modelslist.Model{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": models})
}

// handleModelsListCatalogStub serves GET probes with a static catalog shape.
func handleModelsListCatalogStub(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": []map[string]any{
			{
				"id":           "claude-haiku-4-5-20251001",
				"display_name": "Claude Haiku 4.5",
				"speeds":       []string{"standard", "fast"},
			},
			{
				"id":           "claude-sonnet-4-6",
				"display_name": "Claude Sonnet 4.6",
				"speeds":       []string{"standard"},
			},
			{
				"id":           "claude-opus-4-7",
				"display_name": "Claude Opus 4.7",
				"speeds":       []string{"standard"},
			},
		},
	})
}
