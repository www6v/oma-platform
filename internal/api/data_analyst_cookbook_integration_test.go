package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestDataAnalystCookbookCriticalPath replicates cookbook §3–6 on the Go server:
// upload CSV → session with resources → user message → files.list(scope_id).
func TestDataAnalystCookbookCriticalPath(t *testing.T) {
	sim := &harness.DataAnalystSimulatingClient{ReportMinBytes: 11 * 1024}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunDataAnalystCookbookFlow(
		t, handler, sim, integrationtest.SalesDataCSV,
	)
}
