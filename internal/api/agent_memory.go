package api

// Agent built-in memory (Hermes parity). Each agent owns a reserved
// memory store (id "agentmem-{agent_id}", kind "agent_builtin") holding
// two documents: /MEMORY.md (agent notes) and /USER.md (user profile).
// The piPy memory extension (piPy-hermes-memory) reads and writes them
// through these internal endpoints; semantics (entry limits, substring
// matching, snapshot rendering) live in the extension package.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/open-ma/oma-building/internal/store"
)

const (
	agentMemoryStorePrefix = "agentmem-"
	agentMemoryStoreName   = "Agent Memory"
	agentMemoryKind        = "agent_builtin"

	memoryMDPath = "/MEMORY.md"
	userMDPath   = "/USER.md"
)

func handleInternalAgentMemoryGet(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := internalTenantID(r)
		agentID := r.URL.Query().Get("agent_id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent_id is required")
			return
		}
		storeID := agentMemoryStorePrefix + agentID
		row, err := deps.MemoryStores.EnsureStoreWithID(
			r.Context(), tenantID, storeID, agentMemoryStoreName, agentMemoryKind,
		)
		if err != nil || row == nil {
			writeError(w, http.StatusInternalServerError, "failed to ensure agent memory store")
			return
		}
		contents := map[string]string{memoryMDPath: "", userMDPath: ""}
		for path := range contents {
			mem, err := deps.MemoryStores.GetMemoryByPath(
				r.Context(), tenantID, storeID, path,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read agent memory")
				return
			}
			if mem != nil {
				contents[path] = mem.Content
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"store_id": storeID,
			"contents": contents,
		})
	}
}

func handleInternalAgentMemoryWrite(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TenantID  string `json:"tenant_id"`
			AgentID   string `json:"agent_id"`
			Path      string `json:"path"`
			Content   string `json:"content"`
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		tenantID := body.TenantID
		if tenantID == "" {
			tenantID = internalTenantID(r)
		}
		if body.AgentID == "" {
			writeError(w, http.StatusBadRequest, "agent_id is required")
			return
		}
		if body.Path != memoryMDPath && body.Path != userMDPath {
			writeError(w, http.StatusBadRequest,
				"path must be /MEMORY.md or /USER.md")
			return
		}
		storeID := agentMemoryStorePrefix + body.AgentID
		row, err := deps.MemoryStores.EnsureStoreWithID(
			r.Context(), tenantID, storeID, agentMemoryStoreName, agentMemoryKind,
		)
		if err != nil || row == nil {
			writeError(w, http.StatusInternalServerError, "failed to ensure agent memory store")
			return
		}
		sessionID := body.SessionID
		if sessionID == "" {
			sessionID = "unknown"
		}
		mem, err := deps.MemoryStores.WriteMemory(
			r.Context(), tenantID, storeID, body.Path, body.Content,
			"agent_session", sessionID, nil,
		)
		if err != nil {
			if errors.Is(err, store.ErrMemoryContentTooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to write agent memory")
			return
		}
		writeJSON(w, http.StatusOK, serializeMemoryMeta(mem))
	}
}
