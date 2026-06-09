package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type modelCardResponse struct {
	ID            string          `json:"id"`
	ModelID       string          `json:"model_id"`
	Model         string          `json:"model"`
	Provider      string          `json:"provider"`
	APIKeyPreview string          `json:"api_key_preview"`
	BaseURL       string          `json:"base_url,omitempty"`
	CustomHeaders json.RawMessage `json:"custom_headers,omitempty"`
	IsDefault     bool            `json:"is_default"`
	CreatedAt     int64           `json:"created_at"`
	UpdatedAt     *int64          `json:"updated_at,omitempty"`
	ArchivedAt    *int64          `json:"archived_at,omitempty"`
}

func formatModelCard(card *store.ModelCard) modelCardResponse {
	return modelCardResponse{
		ID:            card.ID,
		ModelID:       card.ModelID,
		Model:         card.Model,
		Provider:      card.Provider,
		APIKeyPreview: card.APIKeyPreview,
		BaseURL:       card.BaseURL,
		CustomHeaders: card.CustomHeaders,
		IsDefault:     card.IsDefault,
		CreatedAt:     card.CreatedAt,
		UpdatedAt:     card.UpdatedAt,
		ArchivedAt:    card.ArchivedAt,
	}
}

func mountModelCardRoutes(r chi.Router, cards *store.ModelCardRepo) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			ModelID       string          `json:"model_id"`
			Model         string          `json:"model"`
			Provider      string          `json:"provider"`
			APIKey        string          `json:"api_key"`
			BaseURL       string          `json:"base_url"`
			CustomHeaders json.RawMessage `json:"custom_headers"`
			IsDefault     bool            `json:"is_default"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.ModelID == "" || body.Provider == "" || body.APIKey == "" {
			writeError(w, http.StatusBadRequest, "model_id, provider, api_key required")
			return
		}
		card, err := cards.Create(req.Context(), store.CreateModelCardInput{
			TenantID:      tenantID(req),
			ModelID:       body.ModelID,
			Model:         body.Model,
			Provider:      body.Provider,
			APIKey:        body.APIKey,
			BaseURL:       body.BaseURL,
			CustomHeaders: body.CustomHeaders,
			MakeDefault:   body.IsDefault,
		})
		if err == store.ErrDuplicate {
			writeError(w, http.StatusConflict, "model_id already exists")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, formatModelCard(card))
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		list, err := cards.List(req.Context(), tenantID(req), false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]modelCardResponse, 0, len(list))
		for _, card := range list {
			out = append(out, formatModelCard(card))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": out})
	})

	r.Get("/{id}/key", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		key, err := cards.GetAPIKey(req.Context(), tenantID(req), id)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
	})

	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		card, err := cards.Get(req.Context(), tenantID(req), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if card == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, formatModelCard(card))
	})

	r.Post("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		var body struct {
			ModelID       *string          `json:"model_id"`
			Model         *string          `json:"model"`
			Provider      *string          `json:"provider"`
			APIKey        *string          `json:"api_key"`
			BaseURL       *string          `json:"base_url"`
			CustomHeaders *json.RawMessage `json:"custom_headers"`
			IsDefault     *bool            `json:"is_default"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		patch := store.UpdateModelCardInput{
			ModelID:  body.ModelID,
			Model:    body.Model,
			Provider: body.Provider,
			APIKey:   body.APIKey,
			IsDefault: body.IsDefault,
		}
		if body.BaseURL != nil {
			patch.BaseURL = body.BaseURL
			patch.BaseURLSet = true
		}
		if body.CustomHeaders != nil {
			patch.CustomHeaders = *body.CustomHeaders
			patch.CustomSet = true
		}
		card, err := cards.Update(req.Context(), tenantID(req), id, patch)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err == store.ErrDuplicate {
			writeError(w, http.StatusConflict, "model_id already exists")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, formatModelCard(card))
	})

	r.Delete("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		if err := cards.Delete(req.Context(), tenantID(req), id); err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"type": "model_card_deleted", "id": id})
	})
}
