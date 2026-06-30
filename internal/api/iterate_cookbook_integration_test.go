package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestIterateCookbookMultiTurn replicates iterate cookbook MT1 on the Go server:
// two user.message turns on one session (fix loop + verify) with outputs sync.
func TestIterateCookbookMultiTurn(t *testing.T) {
	sim := &harness.IterateSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunIterateCookbookFlow(t, handler, sim)
}
