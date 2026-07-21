package doctor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestCheckEmptyBranches(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		CreateBranch("withchanges").
		CommitChange("file1", "real change").
		Checkout("main").
		CreateBranch("nochanges").
		Checkout("main")

	empty := checkEmptyBranches(s.Engine)

	require.ElementsMatch(t, []string{"nochanges"}, empty)
}
