package api_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
)

// TestCoordinateCookbookSpecialistTeam runs CMA_coordinate_specialist_team parity.
func TestCoordinateCookbookSpecialistTeam(t *testing.T) {
	sim := &demo.CoordinateSimulatingClient{}
	handler, _ := testRouterHarness(t, sim)
	integrationtest.RunCoordinateSpecialistTeamFlow(t, handler, sim)
}
