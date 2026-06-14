package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/skillzip"
	"github.com/open-ma/oma-building/internal/store"
)

const clawhubAPIBase = "https://clawhub.ai/api/v1"

var clawhubAPIBaseOverride string

func clawhubBaseURL() string {
	if clawhubAPIBaseOverride != "" {
		return clawhubAPIBaseOverride
	}
	return clawhubAPIBase
}

type clawhubDeps struct {
	Skills     *store.SkillRepo
	SkillFiles *store.SkillFileStore
	HTTPClient *http.Client
}

func mountClawhubRoutes(r chi.Router, deps clawhubDeps) {
	if deps.Skills == nil {
		return
	}
	client := deps.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	r.Get("/search", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query().Get("q")
		searchURL := clawhubBaseURL() + "/packages"
		if q != "" {
			searchURL += "?q=" + url.QueryEscape(q)
		}
		res, err := client.Get(searchURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": fmt.Sprintf("ClawHub search failed: %d", res.StatusCode),
			})
			return
		}
		var body struct {
			Items []struct {
				Name          string `json:"name"`
				DisplayName   string `json:"displayName"`
				Summary       string `json:"summary"`
				Family        string `json:"family"`
				LatestVersion string `json:"latestVersion"`
				OwnerHandle   string `json:"ownerHandle"`
			} `json:"items"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		skills := make([]map[string]string, 0)
		for _, item := range body.Items {
			if item.Family != "skill" {
				continue
			}
			skills = append(skills, map[string]string{
				"slug":        item.Name,
				"name":        pickString(item.DisplayName, item.Name),
				"description": item.Summary,
				"version":     item.LatestVersion,
				"owner":       item.OwnerHandle,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": skills})
	})

	r.Post("/install", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Slug string `json:"slug"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.Slug == "" {
			writeError(w, http.StatusBadRequest, "slug is required")
			return
		}

		metaURL := clawhubBaseURL() + "/packages/" + url.PathEscape(body.Slug)
		metaRes, err := client.Get(metaURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer metaRes.Body.Close()
		if metaRes.StatusCode == http.StatusNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": fmt.Sprintf("Skill %q not found on ClawHub", body.Slug),
			})
			return
		}
		if metaRes.StatusCode < 200 || metaRes.StatusCode >= 300 {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": fmt.Sprintf("ClawHub metadata failed: %d", metaRes.StatusCode),
			})
			return
		}
		var meta struct {
			Package struct {
				DisplayName string `json:"displayName"`
				Name        string `json:"name"`
				Summary     string `json:"summary"`
			} `json:"package"`
		}
		if err := json.NewDecoder(metaRes.Body).Decode(&meta); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		dlURL := clawhubBaseURL() + "/download?slug=" + url.QueryEscape(body.Slug)
		dlRes, err := client.Get(dlURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer dlRes.Body.Close()
		if dlRes.StatusCode < 200 || dlRes.StatusCode >= 300 {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": fmt.Sprintf("Failed to download skill: %d", dlRes.StatusCode),
			})
			return
		}
		zipBytes, err := io.ReadAll(io.LimitReader(dlRes.Body, 100<<20))
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		parsed, err := skillzip.ParseSkillZip(zipBytes)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		displayTitle := meta.Package.DisplayName
		if displayTitle == "" {
			displayTitle = meta.Package.Name
		}
		description := meta.Package.Summary
		if description == "" {
			description = parsed.Description
		}
		name := parsed.Name
		if name == "" {
			name = slugToSkillName(displayTitle)
		}

		skill, ver, err := deps.Skills.Create(req.Context(), store.CreateSkillInput{
			TenantID:     tenantID(req),
			DisplayTitle: displayTitle,
			Name:         name,
			Description:  description,
			Files:        parsed.Files,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		resp := toAPISkill(skill)
		resp["clawhub_slug"] = body.Slug
		files, _ := deps.SkillFiles.ReadVersionFiles(
			tenantID(req), skill.ID, ver.Version, ver.Files,
		)
		resp["files"] = files
		writeJSON(w, http.StatusCreated, resp)
	})
}

func pickString(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func slugToSkillName(title string) string {
	out := strings.ToLower(title)
	var b strings.Builder
	for _, r := range out {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' {
			b.WriteRune('-')
		}
	}
	s := b.String()
	if len(s) > 64 {
		s = s[:64]
	}
	return strings.Trim(s, "-")
}
