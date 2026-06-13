package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/usage"
)

type costReportDeps struct {
	Events   *store.EventRepo
	Sessions *store.SessionRepo
}

func mountCostReportRoutes(r chi.Router, deps costReportDeps) {
	if deps.Events == nil {
		return
	}

	r.Get("/v1/cost_report", func(w http.ResponseWriter, req *http.Request) {
		days := 30
		if raw := req.URL.Query().Get("days"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err == nil && parsed > 0 {
				days = parsed
			}
		}
		if days > 90 {
			days = 90
		}
		if days < 1 {
			days = 1
		}

		until := time.Now().UTC()
		since := until.Add(-time.Duration(days) * 24 * time.Hour)
		sinceMs := since.UnixMilli()
		untilMs := until.UnixMilli()

		rows, err := deps.Events.ListModelUsageEvents(
			req.Context(), tenantID(req), sinceMs,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		report := usage.BuildReport(rows, days, sinceMs, untilMs)
		writeJSON(w, http.StatusOK, map[string]any{
			"type":           "cost_report",
			"period_days":    report.PeriodDays,
			"since":          msToISO(report.SinceMs),
			"until":          msToISO(report.UntilMs),
			"usage":          report.Usage,
			"by_agent":       report.ByAgent,
			"session_count":  report.SessionCount,
			"span_count":     report.SpanCount,
		})
	})
}
