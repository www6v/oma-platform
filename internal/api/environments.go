package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

func mountEnvironmentRoutes(r chi.Router, envs *store.EnvironmentRepo) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Config      json.RawMessage `json:"config"`
			Metadata    json.RawMessage `json:"metadata"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		env, err := envs.Create(req.Context(), store.CreateEnvironmentInput{
			TenantID:    defaultTenant,
			Name:        body.Name,
			Description: body.Description,
			Config:      body.Config,
			Metadata:    body.Metadata,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, env)
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		params := parseEnvironmentListParams(req)
		if params.Err != "" {
			writeError(w, http.StatusBadRequest, params.Err)
			return
		}
		page, err := envs.ListPage(req.Context(), params.Query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeListPage(w, page.Items, page.NextCursor)
	})

	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		env, err := envs.Get(req.Context(), defaultTenant, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if env == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, env)
	})

	r.Put("/{id}", func(w http.ResponseWriter, req *http.Request) {
		updateEnvironment(w, req, envs)
	})

	r.Post("/{id}", func(w http.ResponseWriter, req *http.Request) {
		updateEnvironment(w, req, envs)
	})

	r.Post("/{id}/archive", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		env, err := envs.Archive(req.Context(), defaultTenant, id)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, env)
	})
}

func updateEnvironment(
	w http.ResponseWriter,
	req *http.Request,
	envs *store.EnvironmentRepo,
) {
	id := chi.URLParam(req, "id")
	var body struct {
		Name        *string          `json:"name"`
		Description *string          `json:"description"`
		Config      *json.RawMessage `json:"config"`
		Metadata    *json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	patch := store.UpdateEnvironmentInput{
		Name:        body.Name,
		Description: body.Description,
	}
	if body.Config != nil {
		patch.Config = *body.Config
		patch.ConfigSet = true
	}
	if body.Metadata != nil {
		patch.Metadata = *body.Metadata
		patch.MetadataSet = true
	}
	env, err := envs.Update(req.Context(), defaultTenant, id, patch)
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err == store.ErrArchived {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, env)
}
