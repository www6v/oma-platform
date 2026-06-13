package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type memoryStoresDeps struct {
	MemoryStores *store.MemoryStoreRepo
	Dreams       *store.DreamRepo
}

func mountMemoryStoreRoutes(r chi.Router, deps memoryStoresDeps) {
	if deps.MemoryStores == nil {
		return
	}
	repo := deps.MemoryStores

	r.Route("/v1/memory_stores", func(r chi.Router) {
		r.Post("/", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Name        string  `json:"name"`
				Description *string `json:"description"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if body.Name == "" {
				writeError(w, http.StatusBadRequest, "name is required")
				return
			}
			row, err := repo.CreateStore(
				req.Context(), tenantID(req), body.Name, body.Description,
			)
			if err != nil {
				writeMemoryStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, serializeMemoryStore(row))
		})

		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			opts, errResp := parseMemoryStoreListParams(req)
			if errResp != nil {
				writeJSON(w, http.StatusBadRequest, errResp)
				return
			}
			rows, err := repo.ListStores(req.Context(), tenantID(req), opts)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			data := make([]map[string]any, 0, len(rows))
			for i := range rows {
				data = append(data, serializeMemoryStore(&rows[i]))
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": data})
		})

		r.Route("/{id}", func(r chi.Router) {
			storeID := func(req *http.Request) string {
				return chi.URLParam(req, "id")
			}

			r.Get("/", func(w http.ResponseWriter, req *http.Request) {
				row, err := repo.GetStore(
					req.Context(), tenantID(req), storeID(req),
				)
				if err != nil {
					writeMemoryStoreError(w, err)
					return
				}
				if row == nil {
					writeError(w, http.StatusNotFound, "Memory store not found")
					return
				}
				writeJSON(w, http.StatusOK, serializeMemoryStore(row))
			})

			r.MethodFunc(http.MethodPost, "/", func(w http.ResponseWriter, req *http.Request) {
				handleUpdateMemoryStore(w, req, repo, storeID(req))
			})
			r.MethodFunc(http.MethodPut, "/", func(w http.ResponseWriter, req *http.Request) {
				handleUpdateMemoryStore(w, req, repo, storeID(req))
			})

			r.Post("/archive", func(w http.ResponseWriter, req *http.Request) {
				tid := tenantID(req)
				id := storeID(req)
				if blocked := dreamBlocksMemoryStore(
					req, deps.Dreams, tid, id,
				); blocked != nil {
					writeJSON(w, http.StatusBadRequest, blocked)
					return
				}
				row, err := repo.ArchiveStore(req.Context(), tid, id)
				if err != nil {
					writeMemoryStoreError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, serializeMemoryStore(row))
			})

			r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
				tid := tenantID(req)
				id := storeID(req)
				if blocked := dreamBlocksMemoryStore(
					req, deps.Dreams, tid, id,
				); blocked != nil {
					writeJSON(w, http.StatusBadRequest, blocked)
					return
				}
				if err := repo.DeleteStore(req.Context(), tid, id); err != nil {
					writeMemoryStoreError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"type": "memory_store_deleted",
					"id":   id,
				})
			})

			r.Post("/memories", func(w http.ResponseWriter, req *http.Request) {
				var body struct {
					Path          string          `json:"path"`
					Content       string          `json:"content"`
					Precondition  json.RawMessage `json:"precondition"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					writeError(w, http.StatusBadRequest, "invalid json")
					return
				}
				if body.Path == "" {
					writeError(w, http.StatusBadRequest, "path and content are required")
					return
				}
				actorType, actorID := memoryActor(req)
				row, err := repo.WriteMemory(
					req.Context(), tenantID(req), storeID(req),
					body.Path, body.Content, actorType, actorID,
					parseWritePrecondition(body.Precondition),
				)
				if err != nil {
					writeMemoryStoreError(w, err)
					return
				}
				writeJSON(w, http.StatusCreated, serializeMemory(row))
			})

			r.Get("/memories", func(w http.ResponseWriter, req *http.Request) {
				prefix := req.URL.Query().Get("path_prefix")
				if prefix == "" {
					prefix = req.URL.Query().Get("prefix")
				}
				depthRaw := req.URL.Query().Get("depth")
				var depth *int
				if depthRaw != "" {
					d, err := strconv.Atoi(depthRaw)
					if err != nil {
						writeError(w, http.StatusBadRequest, "invalid depth")
						return
					}
					if d < 0 {
						d = 0
					}
					depth = &d
				}
				rows, err := repo.ListMemories(
					req.Context(), tenantID(req), storeID(req), prefix,
				)
				if err != nil {
					writeMemoryStoreError(w, err)
					return
				}
				if depth != nil && prefix != "" {
					filtered := rows[:0]
					for _, m := range rows {
						if !strings.HasPrefix(m.Path, prefix) {
							continue
						}
						remainder := strings.TrimPrefix(m.Path, prefix)
						segments := 0
						if trimmed := strings.Trim(remainder, "/"); trimmed != "" {
							segments = len(strings.Split(trimmed, "/"))
						}
						if segments <= *depth {
							filtered = append(filtered, m)
						}
					}
					rows = filtered
				}
				data := make([]map[string]any, 0, len(rows))
				for i := range rows {
					data = append(data, serializeMemoryMeta(&rows[i]))
				}
				writeJSON(w, http.StatusOK, map[string]any{"data": data})
			})

			r.Route("/memories/{memoryId}", func(r chi.Router) {
				memID := func(req *http.Request) string {
					return chi.URLParam(req, "memoryId")
				}

				r.Get("/", func(w http.ResponseWriter, req *http.Request) {
					row, err := repo.GetMemory(
						req.Context(), tenantID(req), storeID(req), memID(req),
					)
					if err != nil {
						writeMemoryStoreError(w, err)
						return
					}
					if row == nil {
						writeError(w, http.StatusNotFound, "Memory not found")
						return
					}
					writeJSON(w, http.StatusOK, serializeMemory(row))
				})

				updateMemory := func(w http.ResponseWriter, req *http.Request) {
					var body struct {
						Path          *string         `json:"path"`
						Content       *string         `json:"content"`
						Precondition  json.RawMessage `json:"precondition"`
					}
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						writeError(w, http.StatusBadRequest, "invalid json")
						return
					}
					actorType, actorID := memoryActor(req)
					row, err := repo.UpdateMemory(
						req.Context(), tenantID(req), storeID(req), memID(req),
						body.Path, body.Content, actorType, actorID,
						parseWritePrecondition(body.Precondition),
					)
					if err != nil {
						writeMemoryStoreError(w, err)
						return
					}
					writeJSON(w, http.StatusOK, serializeMemory(row))
				}
				r.Patch("/", updateMemory)
				r.Post("/", updateMemory)

				r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
					expected := req.URL.Query().Get("expected_content_sha256")
					actorType, actorID := memoryActor(req)
					if err := repo.DeleteMemory(
						req.Context(), tenantID(req), storeID(req),
						memID(req), expected, actorType, actorID,
					); err != nil {
						writeMemoryStoreError(w, err)
						return
					}
					writeJSON(w, http.StatusOK, map[string]any{
						"type": "memory_deleted",
						"id":   memID(req),
					})
				})
			})

			r.Get("/memory_versions", func(w http.ResponseWriter, req *http.Request) {
				memoryID := req.URL.Query().Get("memory_id")
				rows, err := repo.ListVersions(
					req.Context(), tenantID(req), storeID(req), memoryID,
				)
				if err != nil {
					writeMemoryStoreError(w, err)
					return
				}
				data := make([]map[string]any, 0, len(rows))
				for i := range rows {
					data = append(data, serializeMemoryVersionMeta(&rows[i]))
				}
				writeJSON(w, http.StatusOK, map[string]any{"data": data})
			})

			r.Route("/memory_versions/{versionId}", func(r chi.Router) {
				verID := func(req *http.Request) string {
					return chi.URLParam(req, "versionId")
				}

				r.Get("/", func(w http.ResponseWriter, req *http.Request) {
					row, err := repo.GetVersion(
						req.Context(), tenantID(req), storeID(req), verID(req),
					)
					if err != nil {
						writeMemoryStoreError(w, err)
						return
					}
					if row == nil {
						writeError(w, http.StatusNotFound, "Memory version not found")
						return
					}
					writeJSON(w, http.StatusOK, serializeMemoryVersion(row))
				})

				r.Post("/redact", func(w http.ResponseWriter, req *http.Request) {
					row, err := repo.RedactVersion(
						req.Context(), tenantID(req), storeID(req), verID(req),
					)
					if err != nil {
						writeMemoryStoreError(w, err)
						return
					}
					writeJSON(w, http.StatusOK, serializeMemoryVersion(row))
				})
			})
		})
	})
}

func handleUpdateMemoryStore(
	w http.ResponseWriter,
	req *http.Request,
	repo *store.MemoryStoreRepo,
	storeID string,
) {
	var body struct {
		Name        *string `json:"name"`
		Description **string `json:"description"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	row, err := repo.UpdateStore(
		req.Context(), tenantID(req), storeID, body.Name, body.Description,
	)
	if err != nil {
		writeMemoryStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serializeMemoryStore(row))
}

func parseMemoryStoreListParams(
	req *http.Request,
) (store.MemoryStoreListOptions, map[string]any) {
	opts := store.MemoryStoreListOptions{}
	statusRaw := req.URL.Query().Get("status")
	if statusRaw != "" {
		if statusRaw != "active" && statusRaw != "archived" && statusRaw != "any" {
			return opts, map[string]any{
				"error": map[string]any{
					"type":    "invalid_request_error",
					"code":    "invalid_status",
					"message": "Invalid status '" + statusRaw + "'; expected one of active|archived|any.",
				},
			}
		}
		opts.Status = statusRaw
	}
	if req.URL.Query().Get("include_archived") == "true" {
		opts.IncludeArchived = true
	}
	if raw := req.URL.Query().Get("created_after"); raw != "" {
		ms, err := parseISOToMs(raw)
		if err != nil {
			return opts, invalidTimestampError("created_after", raw)
		}
		opts.CreatedAfter = &ms
	}
	if raw := req.URL.Query().Get("created_before"); raw != "" {
		ms, err := parseISOToMs(raw)
		if err != nil {
			return opts, invalidTimestampError("created_before", raw)
		}
		opts.CreatedBefore = &ms
	}
	return opts, nil
}

func parseISOToMs(raw string) (int64, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

func invalidTimestampError(field, raw string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"type":    "invalid_request_error",
			"code":    "invalid_timestamp",
			"message": "Invalid " + field + " '" + raw + "'; expected ISO-8601 timestamp.",
		},
	}
}

func memoryActor(req *http.Request) (string, string) {
	if uid := userID(req); uid != "" {
		return "user", uid
	}
	return "api_key", "api"
}

func parseWritePrecondition(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var pre map[string]any
	if err := json.Unmarshal(raw, &pre); err != nil {
		return nil
	}
	out := map[string]string{}
	if v, ok := pre["if_absent"].(bool); ok && v {
		out["if_absent"] = "true"
	}
	if v, ok := pre["content_sha256"].(string); ok && v != "" {
		out["content_sha256"] = v
	}
	return out
}

func writeMemoryStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrMemoryStoreNotFound):
		writeError(w, http.StatusNotFound, "Memory store not found")
	case errors.Is(err, store.ErrMemoryNotFound):
		writeError(w, http.StatusNotFound, "Memory not found")
	case errors.Is(err, store.ErrMemoryVersionNotFound):
		writeError(w, http.StatusNotFound, "Memory version not found")
	case errors.Is(err, store.ErrMemoryPreconditionFailed):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "precondition_failed",
			"detail": err.Error(),
		})
	case errors.Is(err, store.ErrMemoryContentTooLarge):
		writeError(w, http.StatusBadRequest, "content exceeds 100KB limit (102400 bytes)")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func serializeMemoryStore(row *store.MemoryStoreRow) map[string]any {
	out := map[string]any{
		"type":       "memory_store",
		"id":         row.ID,
		"name":       row.Name,
		"created_at": msToISO(row.CreatedAt),
	}
	if row.Description.Valid {
		out["description"] = row.Description.String
	}
	if row.UpdatedAt.Valid {
		out["updated_at"] = msToISO(row.UpdatedAt.Int64)
	}
	if row.ArchivedAt.Valid {
		out["archived_at"] = msToISO(row.ArchivedAt.Int64)
	}
	return out
}

func serializeMemory(row *store.MemoryRow) map[string]any {
	return map[string]any{
		"id":               row.ID,
		"store_id":         row.StoreID,
		"path":             row.Path,
		"content":          row.Content,
		"content_sha256":   row.ContentSHA256,
		"etag":             row.ETag,
		"size_bytes":       row.SizeBytes,
		"created_at":       msToISO(row.CreatedAt),
		"updated_at":       msToISO(row.UpdatedAt),
	}
}

func serializeMemoryMeta(row *store.MemoryRow) map[string]any {
	out := serializeMemory(row)
	delete(out, "content")
	return out
}

func serializeMemoryVersion(row *store.MemoryVersionRow) map[string]any {
	out := map[string]any{
		"id":         row.ID,
		"memory_id":  row.MemoryID,
		"store_id":   row.StoreID,
		"operation":  row.Operation,
		"actor":      map[string]string{"type": row.ActorType, "id": row.ActorID},
		"created_at": msToISO(row.CreatedAt),
	}
	if row.Path.Valid {
		out["path"] = row.Path.String
	}
	if row.Content.Valid {
		out["content"] = row.Content.String
	}
	if row.ContentSHA256.Valid {
		out["content_sha256"] = row.ContentSHA256.String
	}
	if row.SizeBytes.Valid {
		out["size_bytes"] = row.SizeBytes.Int64
	}
	if row.Redacted {
		out["redacted"] = true
	}
	return out
}

func serializeMemoryVersionMeta(row *store.MemoryVersionRow) map[string]any {
	out := serializeMemoryVersion(row)
	delete(out, "content")
	return out
}

func msToISO(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}
