package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

const (
	maxSessionResources          = 100
	maxSessionMemoryStoreResources = 8
)

func (h *sessionHandlers) mountSessionResourceRoutes(r chi.Router) {
	r.Post("/{id}/resources", h.handleSessionResourceAdd)
	r.Get("/{id}/resources", h.handleSessionResourceList)
	r.Get("/{id}/resources/{resource_id}", h.handleSessionResourceGet)
	r.Post("/{id}/resources/{resource_id}", h.handleSessionResourceUpdate)
	r.Delete("/{id}/resources/{resource_id}", h.handleSessionResourceDelete)
}

func (h *sessionHandlers) handleSessionUpdate(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	if sess.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "session archived")
		return
	}

	var body struct {
		Title    *string         `json:"title"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	patch := store.UpdateSessionInput{Title: body.Title}
	if body.Metadata != nil {
		patch.Metadata = body.Metadata
		patch.MetadataSet = true
	}
	updated, err := h.sessions.Update(
		req.Context(), tenantID(req), sess.ID, patch,
	)
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err == store.ErrArchived {
		writeError(w, http.StatusConflict, "session archived")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, formatAPISession(updated))
}

func (h *sessionHandlers) handleSessionResourceAdd(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	if sess.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "session archived")
		return
	}

	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	resType, _ := body["type"].(string)
	if resType == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if err := validateResourceInput(body); err != nil {
		if err.Error() == "file not found" {
			writeError(w, http.StatusNotFound, "File not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resources, err := loadSessionResourceMaps(sess)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(resources) >= maxSessionResources {
		writeError(
			w, http.StatusBadRequest,
			fmt.Sprintf("session cannot have more than %d resources", maxSessionResources),
		)
		return
	}
	if resType == "memory_store" &&
		countResourcesByType(resources, "memory_store") >= maxSessionMemoryStoreResources {
		writeError(
			w, http.StatusBadRequest,
			fmt.Sprintf(
				"session cannot have more than %d memory_store resources",
				maxSessionMemoryStoreResources,
			),
		)
		return
	}

	spec := body
	if resType == "file" {
		scoped, ok := h.scopeResourceFile(
			req.Context(), tenantID(req), sess.ID, spec,
		)
		if !ok {
			writeError(w, http.StatusNotFound, "File not found")
			return
		}
		spec = scoped
	}

	now := formatISO(time.Now().UnixMilli())
	stamped := map[string]any{
		"id":         store.NewResourceID(),
		"session_id": sess.ID,
		"created_at": now,
	}
	for k, v := range spec {
		stamped[k] = v
	}
	resources = append(resources, stamped)

	sess, err = h.persistSessionResources(
		req.Context(), tenantID(req), sess.ID, resources,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, stamped)
}

func (h *sessionHandlers) handleSessionResourceList(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	resources, err := loadSessionResourceMaps(sess)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := make([]any, 0, len(resources))
	for _, res := range resources {
		data = append(data, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *sessionHandlers) handleSessionResourceGet(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	resourceID := chi.URLParam(req, "resource_id")
	res, found := findSessionResource(sess, resourceID)
	if !found {
		writeError(w, http.StatusNotFound, "Resource not found")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *sessionHandlers) handleSessionResourceUpdate(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	if sess.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "session archived")
		return
	}

	resourceID := chi.URLParam(req, "resource_id")
	resources, err := loadSessionResourceMaps(sess)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	idx := -1
	var createdAt any
	for i, res := range resources {
		if id, _ := res["id"].(string); id == resourceID {
			idx = i
			createdAt = res["created_at"]
			break
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, "Resource not found")
		return
	}

	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if resType, _ := body["type"].(string); resType == "" {
		writeError(w, http.StatusBadRequest, "resource body with `type` field is required")
		return
	}

	stamped := map[string]any{
		"id":         resourceID,
		"session_id": sess.ID,
		"created_at": createdAt,
	}
	for k, v := range body {
		stamped[k] = v
	}
	resources[idx] = stamped

	sess, err = h.persistSessionResources(
		req.Context(), tenantID(req), sess.ID, resources,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stamped)
}

func (h *sessionHandlers) handleSessionResourceDelete(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	resourceID := chi.URLParam(req, "resource_id")
	resources, err := loadSessionResourceMaps(sess)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next := make([]map[string]any, 0, len(resources))
	found := false
	for _, res := range resources {
		if id, _ := res["id"].(string); id == resourceID {
			found = true
			continue
		}
		next = append(next, res)
	}
	if !found {
		writeError(w, http.StatusNotFound, "Resource not found")
		return
	}
	if _, err := h.persistSessionResources(
		req.Context(), tenantID(req), sess.ID, next,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"type": "resource_deleted",
		"id":   resourceID,
	})
}

func (h *sessionHandlers) persistSessionResources(
	ctx context.Context,
	tenantID, sessionID string,
	resources []map[string]any,
) (*store.Session, error) {
	data, err := json.Marshal(resources)
	if err != nil {
		return nil, err
	}
	return h.sessions.SetResources(ctx, tenantID, sessionID, data)
}

func (h *sessionHandlers) scopeResourceFile(
	ctx context.Context,
	tenantID, sessionID string,
	spec map[string]any,
) (map[string]any, bool) {
	if h.resources == nil {
		return spec, true
	}
	scoped := h.resources.ScopeSessionResources(
		ctx, tenantID, sessionID, []map[string]any{spec},
	)
	if len(scoped) == 0 {
		return nil, false
	}
	return scoped[0], true
}

func loadSessionResourceMaps(sess *store.Session) ([]map[string]any, error) {
	raw := harness.ParseResourceArray(sess.Resources)
	out := make([]map[string]any, 0, len(raw))
	for _, spec := range raw {
		stampResourceRecord(sess.ID, spec)
		out = append(out, spec)
	}
	return out, nil
}

func findSessionResource(
	sess *store.Session,
	resourceID string,
) (map[string]any, bool) {
	resources, err := loadSessionResourceMaps(sess)
	if err != nil {
		return nil, false
	}
	for _, res := range resources {
		if id, _ := res["id"].(string); id == resourceID {
			return res, true
		}
	}
	return nil, false
}

func stampResourceRecord(sessionID string, spec map[string]any) {
	if _, ok := spec["id"].(string); !ok {
		spec["id"] = store.NewResourceID()
	}
	if _, ok := spec["session_id"].(string); !ok {
		spec["session_id"] = sessionID
	}
	if _, ok := spec["created_at"].(string); !ok {
		spec["created_at"] = formatISO(time.Now().UnixMilli())
	}
}

func stampResourceRecords(
	sessionID string,
	specs []map[string]any,
) []map[string]any {
	for _, spec := range specs {
		stampResourceRecord(sessionID, spec)
	}
	return specs
}

func countResourcesByType(
	resources []map[string]any,
	resType string,
) int {
	n := 0
	for _, res := range resources {
		if t, _ := res["type"].(string); t == resType {
			n++
		}
	}
	return n
}

func validateResourceInput(body map[string]any) error {
	resType, _ := body["type"].(string)
	switch resType {
	case "file":
		fileID, _ := body["file_id"].(string)
		if fileID == "" {
			return fmt.Errorf("file_id is required for file resources")
		}
	case "memory_store":
		storeID, _ := body["memory_store_id"].(string)
		if storeID == "" {
			return fmt.Errorf("memory_store_id is required for memory_store resources")
		}
	default:
		return nil
	}
	return nil
}
