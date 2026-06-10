package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

type listPageResponse struct {
	Data       any    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
	NextPage   string `json:"next_page,omitempty"`
	HasMore    bool   `json:"has_more"`
}

func writeListPage(w http.ResponseWriter, data any, nextCursor string) {
	writeJSON(w, http.StatusOK, listPageResponse{
		Data:       data,
		NextCursor: nextCursor,
		NextPage:   nextCursor,
		HasMore:    nextCursor != "",
	})
}

func parseListLimit(values url.Values) int {
	raw := values.Get("limit")
	if raw == "" {
		return store.DefaultListLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return store.DefaultListLimit
	}
	return store.ClampLimit(n)
}

func parseListCursor(values url.Values) string {
	if c := values.Get("cursor"); c != "" {
		return c
	}
	return values.Get("page")
}

func parseOptionalISO_ms(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	ms, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	v := ms.UnixMilli()
	return &v, nil
}

func parseArchiveStatus(
	values url.Values,
) (status string, includeArchived bool, errMsg string) {
	status = values.Get("status")
	includeArchived = values.Get("include_archived") == "true"
	switch status {
	case "any":
		return "any", includeArchived, ""
	case "":
		if includeArchived {
			return "any", true, ""
		}
		return "active", false, ""
	case "active", "archived":
		return status, includeArchived, ""
	default:
		return "", false, "Invalid status '" + status +
			"'; expected one of any|active|archived."
	}
}

func parseSessionStatus(values url.Values) (string, string) {
	raw := values.Get("status")
	if raw == "" {
		return "", ""
	}
	switch raw {
	case "idle", "running", "rescheduling", "terminated", "archived":
		return raw, ""
	default:
		return "", "Invalid status '" + raw +
			"'; expected one of idle|running|rescheduling|terminated."
	}
}

type agentListParams struct {
	Query store.AgentListQuery
	Err   string
}

func parseAgentListParams(r *http.Request) agentListParams {
	q := r.URL.Query()
	status, includeArchived, statusErr := parseArchiveStatus(q)
	if statusErr != "" {
		return agentListParams{Err: statusErr}
	}
	createdAfter, err := parseOptionalISO_ms(q.Get("created_after"))
	if err != nil {
		return agentListParams{
			Err: "Invalid created_after '" + q.Get("created_after") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	createdBefore, err := parseOptionalISO_ms(q.Get("created_before"))
	if err != nil {
		return agentListParams{
			Err: "Invalid created_before '" + q.Get("created_before") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	cursor := parseListCursor(q)
	if cursor != "" {
		if _, err := store.DecodePageCursor(cursor); err != nil {
			return agentListParams{Err: err.Error()}
		}
	}
	return agentListParams{
		Query: store.AgentListQuery{
			TenantID:        tenantID(r),
			Limit:           parseListLimit(q),
			Cursor:          cursor,
			Status:          status,
			Query:           strings.TrimSpace(q.Get("q")),
			CreatedAfter:    createdAfter,
			CreatedBefore:   createdBefore,
			IncludeArchived: includeArchived,
		},
	}
}

type environmentListParams struct {
	Query store.EnvironmentListQuery
	Err   string
}

func parseEnvironmentListParams(r *http.Request) environmentListParams {
	q := r.URL.Query()
	status, includeArchived, statusErr := parseArchiveStatus(q)
	if statusErr != "" {
		return environmentListParams{Err: statusErr}
	}
	createdAfter, err := parseOptionalISO_ms(q.Get("created_after"))
	if err != nil {
		return environmentListParams{
			Err: "Invalid created_after '" + q.Get("created_after") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	createdBefore, err := parseOptionalISO_ms(q.Get("created_before"))
	if err != nil {
		return environmentListParams{
			Err: "Invalid created_before '" + q.Get("created_before") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	cursor := parseListCursor(q)
	if cursor != "" {
		if _, err := store.DecodePageCursor(cursor); err != nil {
			return environmentListParams{Err: err.Error()}
		}
	}
	return environmentListParams{
		Query: store.EnvironmentListQuery{
			TenantID:        tenantID(r),
			Limit:           parseListLimit(q),
			Cursor:          cursor,
			Status:          status,
			Query:           strings.TrimSpace(q.Get("q")),
			CreatedAfter:    createdAfter,
			CreatedBefore:   createdBefore,
			IncludeArchived: includeArchived,
		},
	}
}

type sessionListParams struct {
	Query store.SessionListQuery
	Err   string
}

func parseSessionListParams(r *http.Request) sessionListParams {
	q := r.URL.Query()
	status, statusErr := parseSessionStatus(q)
	if statusErr != "" {
		return sessionListParams{Err: statusErr}
	}
	createdAfter, err := parseOptionalISO_ms(q.Get("created_after"))
	if err != nil {
		return sessionListParams{
			Err: "Invalid created_after '" + q.Get("created_after") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	createdBefore, err := parseOptionalISO_ms(q.Get("created_before"))
	if err != nil {
		return sessionListParams{
			Err: "Invalid created_before '" + q.Get("created_before") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	cursor := parseListCursor(q)
	if cursor != "" {
		if _, err := store.DecodePageCursor(cursor); err != nil {
			return sessionListParams{Err: err.Error()}
		}
	}
	return sessionListParams{
		Query: store.SessionListQuery{
			TenantID:        tenantID(r),
			Limit:           parseListLimit(q),
			Cursor:          cursor,
			Status:          status,
			AgentID:         strings.TrimSpace(q.Get("agent_id")),
			Query:           strings.TrimSpace(q.Get("q")),
			CreatedAfter:    createdAfter,
			CreatedBefore:   createdBefore,
			IncludeArchived: q.Get("include_archived") == "true",
		},
	}
}
