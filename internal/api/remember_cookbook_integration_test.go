package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestRememberCookbookPreferences runs cross-session memory_store recall.
func TestRememberCookbookPreferences(t *testing.T) {
	sim := &demo.RememberSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunRememberPreferencesCookbookFlow(t, handler, sim)
}
