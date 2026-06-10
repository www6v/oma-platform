package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type skillsDeps struct {
	Skills *store.SkillRepo
	Files  *store.SkillFileStore
}

func mountSkillRoutes(r chi.Router, deps skillsDeps) {
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		source := req.URL.Query().Get("source")
		if source != "" &&
			source != "anthropic" &&
			source != "custom" &&
			source != "any" {
			writeError(
				w, http.StatusBadRequest,
				"Invalid source '"+source+
					"'; expected one of anthropic|custom|any.",
			)
			return
		}

		var customs []*store.Skill
		if source != "anthropic" && deps.Skills != nil {
			list, err := deps.Skills.ListCustom(req.Context(), tenantID(req))
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			customs = list
		}

		items := make([]map[string]any, 0)
		if source != "custom" {
			for _, builtin := range store.ListBuiltinSkills() {
				items = append(items, toAPISkillFromBuiltin(builtin))
			}
		}
		for _, skill := range customs {
			items = append(items, toAPISkill(skill))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data":      items,
			"has_more":  false,
			"next_page": nil,
		})
	})

	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		if deps.Skills == nil {
			writeError(w, http.StatusNotImplemented, "skills store not configured")
			return
		}
		var body struct {
			DisplayTitle string                `json:"display_title"`
			Name         string                `json:"name"`
			Description  string                `json:"description"`
			Files        []store.SkillFileInput `json:"files"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		skill, ver, err := deps.Skills.Create(req.Context(), store.CreateSkillInput{
			TenantID:     tenantID(req),
			DisplayTitle: body.DisplayTitle,
			Name:         body.Name,
			Description:  body.Description,
			Files:        body.Files,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		files, _ := deps.Files.ReadVersionFiles(
			tenantID(req), skill.ID, ver.Version, ver.Files,
		)
		resp := toAPISkill(skill)
		resp["files"] = files
		writeJSON(w, http.StatusCreated, resp)
	})

	r.Post("/upload", handleSkillUploadNotImplemented)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			if builtin := store.BuiltinSkillByID(id); builtin != nil {
				writeJSON(w, http.StatusOK, toAPISkillFromBuiltin(*builtin))
				return
			}
			skill, err := deps.Skills.Get(req.Context(), tenantID(req), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if skill == nil {
				writeError(w, http.StatusNotFound, "Skill not found")
				return
			}
			writeJSON(w, http.StatusOK, toAPISkill(skill))
		})

		r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			if store.IsBuiltinSkillID(id) {
				writeError(w, http.StatusForbidden, "Cannot delete built-in skills")
				return
			}
			err := deps.Skills.Delete(req.Context(), tenantID(req), id)
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Skill not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"type": "skill_deleted",
				"id":   id,
			})
		})

		r.Get("/versions", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			if store.IsBuiltinSkillID(id) {
				writeError(w, http.StatusNotFound, "Skill not found")
				return
			}
			skill, err := deps.Skills.Get(req.Context(), tenantID(req), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if skill == nil {
				writeError(w, http.StatusNotFound, "Skill not found")
				return
			}
			summaries, err := deps.Skills.ListVersionSummaries(
				req.Context(), tenantID(req), id,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			items := make([]map[string]any, 0, len(summaries))
			for _, summary := range summaries {
				items = append(items, map[string]any{
					"version":    summary.Version,
					"file_count": summary.FileCount,
					"created_at": formatISO(summary.CreatedAt),
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": items})
		})

		r.Post("/versions", handleSkillUploadNotImplemented)
		r.Post("/versions/upload", handleSkillUploadNotImplemented)

		r.Route("/versions/{version}", func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, req *http.Request) {
				id := chi.URLParam(req, "id")
				version := chi.URLParam(req, "version")
				if store.IsBuiltinSkillID(id) {
					writeError(w, http.StatusNotFound, "Version not found")
					return
				}
				ver, err := deps.Skills.GetVersion(
					req.Context(), tenantID(req), id, version,
				)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if ver == nil {
					writeError(w, http.StatusNotFound, "Version not found")
					return
				}
				files, err := deps.Files.ReadVersionFiles(
					tenantID(req), id, version, ver.Files,
				)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"version":    ver.Version,
					"files":      files,
					"created_at": formatISO(ver.CreatedAt),
				})
			})

			r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
				writeError(
					w, http.StatusNotImplemented,
					"not implemented in oma-platform MVP",
				)
			})
		})
	})
}

func handleSkillUploadNotImplemented(w http.ResponseWriter, _ *http.Request) {
	writeError(
		w,
		http.StatusNotImplemented,
		"not implemented in oma-platform MVP",
	)
}

func toAPISkill(skill *store.Skill) map[string]any {
	source := skill.Source
	if source == "builtin" {
		source = "anthropic"
	}
	out := map[string]any{
		"type":           "skill",
		"id":             skill.ID,
		"display_title":  skill.DisplayTitle,
		"name":           skill.Name,
		"description":    skill.Description,
		"source":         source,
		"latest_version": skill.LatestVersion,
		"created_at":     formatISO(skill.CreatedAt),
	}
	if skill.UpdatedAt != nil {
		out["updated_at"] = formatISO(*skill.UpdatedAt)
	} else {
		out["updated_at"] = formatISO(skill.CreatedAt)
	}
	return out
}

func toAPISkillFromBuiltin(skill store.BuiltinSkill) map[string]any {
	return map[string]any{
		"type":           "skill",
		"id":             skill.ID,
		"display_title":  skill.DisplayTitle,
		"name":           skill.Name,
		"description":    skill.Description,
		"source":         "anthropic",
		"latest_version": skill.LatestVersion,
		"created_at":     formatISO(skill.CreatedAt),
		"updated_at":     formatISO(skill.CreatedAt),
	}
}
