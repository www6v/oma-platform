package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestExploreCookbookUnfamiliarCodebase runs zip explore + mid-session resources.
func TestExploreCookbookUnfamiliarCodebase(t *testing.T) {
	sim := &demo.ExploreSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunExploreUnfamiliarCodebaseFlow(t, handler, sim)
}
