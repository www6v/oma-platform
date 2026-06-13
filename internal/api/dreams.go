package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/dream"
	"github.com/open-ma/oma-building/internal/store"
)

const (
	managedAgentsBeta = "managed-agents-2026-04-01"
	dreamingBeta      = "dreaming-2026-04-21"
)

type dreamsDeps struct {
	Dreams       *store.DreamRepo
	MemoryStores *store.MemoryStoreRepo
	Sessions     *store.SessionRepo
	Worker       *dream.Worker
}

func mountDreamRoutes(r chi.Router, deps dreamsDeps) {
	if deps.Dreams == nil {
		return
	}

	r.Route("/v1/dreams", func(r chi.Router) {
		r.Use(requireDreamBetas)

		r.Post("/", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Inputs []dreamInputEntry `json:"inputs"`
				Model  string            `json:"model"`
				Instructions *string     `json:"instructions"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			storeID, sessionIDs, parseErr := parseDreamInputs(body.Inputs)
			if parseErr != "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": map[string]any{
						"type":    "invalid_request_error",
						"message": parseErr,
					},
				})
				return
			}
			if body.Model == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": map[string]any{
						"type":    "invalid_request_error",
						"message": "model is required",
					},
				})
				return
			}

			tid := tenantID(req)
			if deps.MemoryStores != nil {
				memStore, err := deps.MemoryStores.GetStore(
					req.Context(), tid, storeID,
				)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if memStore == nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{
						"error": map[string]any{
							"type":    "invalid_request_error",
							"message": "input memory store not found",
						},
					})
					return
				}
			}
			if deps.Sessions != nil {
				for _, sid := range sessionIDs {
					sess, err := deps.Sessions.Get(req.Context(), tid, sid)
					if err != nil {
						writeError(w, http.StatusInternalServerError, err.Error())
						return
					}
					if sess == nil {
						writeJSON(w, http.StatusBadRequest, map[string]any{
							"error": map[string]any{
								"type":    "invalid_request_error",
								"message": "input session " + sid + " not found",
							},
						})
						return
					}
				}
			}

			row, err := deps.Dreams.Create(req.Context(), store.CreateDreamInput{
				TenantID:           tid,
				InputMemoryStoreID: storeID,
				InputSessionIDs:    sessionIDs,
				Model:              body.Model,
				Instructions:       body.Instructions,
			})
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": map[string]any{
						"type":    "invalid_request_error",
						"message": err.Error(),
					},
				})
				return
			}
			if deps.Worker != nil {
				dreamID := row.ID
				worker := deps.Worker
				tenant := tid
				go func() {
					_ = worker.Process(context.Background(), tenant, dreamID)
				}()
			}
			writeJSON(w, http.StatusCreated, serializeDream(row))
		})

		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			limit := parseDreamLimit(req.URL.Query().Get("limit"))
			includeArchived := req.URL.Query().Get("include_archived") == "true"
			opts := store.DreamListOptions{
				IncludeArchived: includeArchived,
				Limit:           limit,
			}
			if raw := req.URL.Query().Get("page"); raw != "" {
				if cursor, ok := decodeDreamCursor(raw); ok {
					opts.AfterCreatedAt = &cursor.CreatedAt
					opts.AfterID = cursor.ID
				}
			}
			rows, hasMore, err := deps.Dreams.List(
				req.Context(), tenantID(req), opts,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			data := make([]map[string]any, 0, len(rows))
			for i := range rows {
				data = append(data, serializeDream(&rows[i]))
			}
			resp := map[string]any{
				"data":     data,
				"has_more": hasMore,
			}
			if hasMore && len(rows) > 0 {
				last := rows[len(rows)-1]
				resp["next_page"] = encodeDreamCursor(last.CreatedAt, last.ID)
			} else {
				resp["next_page"] = nil
			}
			writeJSON(w, http.StatusOK, resp)
		})

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, req *http.Request) {
				row, err := deps.Dreams.Get(
					req.Context(), tenantID(req), chi.URLParam(req, "id"),
				)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if row == nil {
					writeJSON(w, http.StatusNotFound, map[string]any{
						"error": map[string]any{
							"type":    "not_found_error",
							"message": "Dream not found",
						},
					})
					return
				}
				writeJSON(w, http.StatusOK, serializeDream(row))
			})

			r.Post("/cancel", func(w http.ResponseWriter, req *http.Request) {
				tid := tenantID(req)
				id := chi.URLParam(req, "id")
				row, err := deps.Dreams.Cancel(req.Context(), tid, id)
				if err != nil {
					if errors.Is(err, store.ErrDreamNotFound) {
						writeJSON(w, http.StatusNotFound, map[string]any{
							"error": map[string]any{
								"type":    "not_found_error",
								"message": "Dream not found",
							},
						})
						return
					}
					if errors.Is(err, store.ErrDreamInvalidState) {
						writeJSON(w, http.StatusBadRequest, map[string]any{
							"error": map[string]any{
								"type":    "invalid_request_error",
								"message": err.Error(),
							},
						})
						return
					}
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, serializeDream(row))
			})

			r.Post("/archive", func(w http.ResponseWriter, req *http.Request) {
				tid := tenantID(req)
				id := chi.URLParam(req, "id")
				row, err := deps.Dreams.Archive(req.Context(), tid, id)
				if err != nil {
					if errors.Is(err, store.ErrDreamNotFound) {
						writeJSON(w, http.StatusNotFound, map[string]any{
							"error": map[string]any{
								"type":    "not_found_error",
								"message": "Dream not found",
							},
						})
						return
					}
					if errors.Is(err, store.ErrDreamInvalidState) {
						writeJSON(w, http.StatusBadRequest, map[string]any{
							"error": map[string]any{
								"type":    "invalid_request_error",
								"message": "cannot archive a non-terminal dream; cancel it first",
							},
						})
						return
					}
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, serializeDream(row))
			})
		})
	})
}

type dreamInputEntry struct {
	Type          string   `json:"type"`
	MemoryStoreID string   `json:"memory_store_id"`
	SessionIDs    []string `json:"session_ids"`
}

type dreamCursor struct {
	CreatedAt int64
	ID        string
}

func parseDreamInputs(inputs []dreamInputEntry) (string, []string, string) {
	if len(inputs) == 0 {
		return "", nil, "inputs[] is required"
	}
	var storeID string
	var storeSeen bool
	var sessionIDs []string
	var sessionsSeen bool
	for _, entry := range inputs {
		switch entry.Type {
		case "memory_store":
			if storeSeen {
				return "", nil, "only one memory_store input permitted"
			}
			storeSeen = true
			if entry.MemoryStoreID == "" {
				return "", nil, "inputs[].memory_store_id is required for memory_store input"
			}
			storeID = entry.MemoryStoreID
		case "sessions":
			if sessionsSeen {
				return "", nil, "only one sessions input permitted"
			}
			sessionsSeen = true
			if len(entry.SessionIDs) > store.MaxSessionsPerDream {
				return "", nil, "sessions per dream capped at 100"
			}
			for _, sid := range entry.SessionIDs {
				if sid == "" {
					return "", nil, "session_ids[] must contain strings"
				}
				sessionIDs = append(sessionIDs, sid)
			}
		default:
			return "", nil, "unknown inputs[].type: " + entry.Type
		}
	}
	if storeID == "" {
		return "", nil, "inputs[] must include a memory_store entry"
	}
	return storeID, sessionIDs, ""
}

func requireDreamBetas(next http.Handler) http.Handler {
	required := []string{managedAgentsBeta, dreamingBeta}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw := req.Header.Get("anthropic-beta")
		present := make(map[string]struct{})
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				present[part] = struct{}{}
			}
		}
		var missing []string
		for _, beta := range required {
			if _, ok := present[beta]; !ok {
				missing = append(missing, beta)
			}
		}
		if len(missing) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"type": "invalid_request_error",
					"message": "Missing required anthropic-beta flag(s): " +
						strings.Join(missing, ", "),
				},
			})
			return
		}
		next.ServeHTTP(w, req)
	})
}

func parseDreamLimit(raw string) int {
	if raw == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

func decodeDreamCursor(raw string) (dreamCursor, bool) {
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return dreamCursor{}, false
	}
	var payload struct {
		T  int64  `json:"t"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return dreamCursor{}, false
	}
	if payload.ID == "" {
		return dreamCursor{}, false
	}
	return dreamCursor{CreatedAt: payload.T, ID: payload.ID}, true
}

func encodeDreamCursor(createdAt int64, id string) string {
	payload, _ := json.Marshal(map[string]any{
		"t":  createdAt,
		"id": id,
	})
	return base64.StdEncoding.EncodeToString(payload)
}

func serializeDream(row *store.DreamRow) map[string]any {
	inputs := []map[string]any{
		{
			"type":            "memory_store",
			"memory_store_id": row.InputMemoryStoreID,
		},
	}
	if len(row.InputSessionIDs) > 0 {
		inputs = append(inputs, map[string]any{
			"type":        "sessions",
			"session_ids": row.InputSessionIDs,
		})
	}
	var outputs []map[string]any
	if row.OutputMemoryStoreID.Valid {
		outputs = []map[string]any{
			{
				"type":            "memory_store",
				"memory_store_id": row.OutputMemoryStoreID.String,
			},
		}
	}
	out := map[string]any{
		"type":         "dream",
		"id":           row.ID,
		"status":       string(row.Status),
		"inputs":       inputs,
		"outputs":      outputs,
		"model":        map[string]any{"id": row.Model},
		"instructions": nil,
		"session_id":   nil,
		"created_at":   msToISO(row.CreatedAt),
		"started_at":   nil,
		"ended_at":     nil,
		"archived_at":  nil,
		"usage":        row.Usage,
		"error":        nil,
	}
	if row.Instructions.Valid {
		out["instructions"] = row.Instructions.String
	}
	if row.SessionID.Valid {
		out["session_id"] = row.SessionID.String
	}
	if row.StartedAt.Valid {
		out["started_at"] = msToISO(row.StartedAt.Int64)
	}
	if row.EndedAt.Valid {
		out["ended_at"] = msToISO(row.EndedAt.Int64)
	}
	if row.ArchivedAt.Valid {
		out["archived_at"] = msToISO(row.ArchivedAt.Int64)
	}
	if row.Error != nil {
		out["error"] = row.Error
	}
	return out
}

func dreamBlocksMemoryStore(
	req *http.Request,
	dreams *store.DreamRepo,
	tenantID, storeID string,
) map[string]any {
	if dreams == nil {
		return nil
	}
	active, err := dreams.FindActiveByOutputStore(
		req.Context(), tenantID, storeID,
	)
	if err != nil || len(active) == 0 {
		return nil
	}
	return map[string]any{
		"error": map[string]any{
			"type": "invalid_request_error",
			"message": "memory store is bound to an active dream " +
				active[0].ID + "; cancel the dream first",
		},
	}
}
