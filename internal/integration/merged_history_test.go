package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestMergedDownstackHistory(t *testing.T) {
	t.Parallel()
	t.Run("restack captures merged history when parent is merged", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Mark branch1 as merged (simulate GitHub PR merge)
		prNum := 101
		meta1, err := sh.Engine.Git().ReadMetadata("branch1")
		require.NoError(t, err)
		meta1 = meta1.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  new(prStateMerged),
		})
		err = sh.Engine.Git().WriteMetadata("branch1", meta1)
		require.NoError(t, err)

		// Rebuild engine to pick up the metadata change
		err = sh.Engine.Rebuild("main")
		require.NoError(t, err)

		// Restack branch2 directly
		branch2 := sh.Engine.GetBranch("branch2")
		_, err = sh.Engine.RestackBranches(context.Background(), []engine.Branch{branch2})
		require.NoError(t, err)

		// Verify branch2 now has merged history
		branch2 = sh.Engine.GetBranch("branch2")
		mergedHistory := branch2.GetMergedDownstack()
		require.Len(t, mergedHistory, 1)
		require.Equal(t, "branch1", mergedHistory[0].BranchName)
		require.NotNil(t, mergedHistory[0].PRNumber)
		require.Equal(t, 101, *mergedHistory[0].PRNumber)
		require.NotNil(t, mergedHistory[0].PRState)
		require.Equal(t, prStateMerged, *mergedHistory[0].PRState)
	})

	t.Run("log displays merged history in annotations", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Mark branch1 as merged
		prNum := 99
		meta1, err := sh.Engine.Git().ReadMetadata("branch1")
		require.NoError(t, err)
		meta1 = meta1.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  new(prStateMerged),
		})
		err = sh.Engine.Git().WriteMetadata("branch1", meta1)
		require.NoError(t, err)

		// Rebuild
		err = sh.Engine.Rebuild("main")
		require.NoError(t, err)

		// Restack branch2
		branch2 := sh.Engine.GetBranch("branch2")
		_, err = sh.Engine.RestackBranches(context.Background(), []engine.Branch{branch2})
		require.NoError(t, err)

		// Get annotations for log display
		renderer := tui.NewStackTreeRenderer(sh.Engine)

		// Populate annotations
		branch2 = sh.Engine.GetBranch("branch2")
		ann := tui.GetBranchAnnotation(sh.Engine, branch2)
		renderer.SetAnnotation("branch2", ann)

		// Verify annotations include merged history
		require.Len(t, ann.MergedDownstack, 1)
		require.Equal(t, "branch1", ann.MergedDownstack[0].BranchName)
		require.NotNil(t, ann.MergedDownstack[0].PRNumber)
		require.Equal(t, 99, *ann.MergedDownstack[0].PRNumber)
		require.Equal(t, prStateMerged, ann.MergedDownstack[0].PRState)
	})

	t.Run("multi-level merge inherits history", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Merge branch1 first
		prNum1 := 100
		meta1, _ := sh.Engine.Git().ReadMetadata("branch1")
		meta1 = meta1.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum1,
			State:  new(prStateMerged),
		})
		_ = sh.Engine.Git().WriteMetadata("branch1", meta1)

		// Rebuild
		_ = sh.Engine.Rebuild("main")

		// Restack branch2
		branch2 := sh.Engine.GetBranch("branch2")
		_, _ = sh.Engine.RestackBranches(context.Background(), []engine.Branch{branch2})

		// Now merge branch2
		prNum2 := 101
		meta2, _ := sh.Engine.Git().ReadMetadata("branch2")
		meta2 = meta2.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum2,
			State:  new(prStateMerged),
		})
		_ = sh.Engine.Git().WriteMetadata("branch2", meta2)

		// Rebuild
		_ = sh.Engine.Rebuild("main")

		// Restack branch3
		branch3 := sh.Engine.GetBranch("branch3")
		_, err := sh.Engine.RestackBranches(context.Background(), []engine.Branch{branch3})
		require.NoError(t, err)

		// Verify branch3 has full history: [branch1, branch2]
		branch3 = sh.Engine.GetBranch("branch3")
		mergedHistory := branch3.GetMergedDownstack()
		require.Len(t, mergedHistory, 2)
		require.Equal(t, "branch1", mergedHistory[0].BranchName) // oldest
		require.Equal(t, "branch2", mergedHistory[1].BranchName) // newest
	})

	t.Run("metadata persists across rebuild", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		prNum := 50
		meta1, _ := sh.Engine.Git().ReadMetadata("branch1")
		meta1 = meta1.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  new(prStateMerged),
		})
		_ = sh.Engine.Git().WriteMetadata("branch1", meta1)

		// Rebuild
		_ = sh.Engine.Rebuild("main")

		// Restack branch2
		branch2 := sh.Engine.GetBranch("branch2")
		_, _ = sh.Engine.RestackBranches(context.Background(), []engine.Branch{branch2})

		// Read metadata directly to verify it was persisted
		meta2, err := sh.Engine.Git().ReadMetadata("branch2")
		require.NoError(t, err)
		mergedDownstack := meta2.GetMergedDownstack()
		require.Len(t, mergedDownstack, 1)
		require.Equal(t, "branch1", mergedDownstack[0].BranchName)
		require.NotNil(t, mergedDownstack[0].PRNumber)
		require.Equal(t, 50, *mergedDownstack[0].PRNumber)

		// Rebuild engine and verify history still accessible
		err = sh.Engine.Rebuild("main")
		require.NoError(t, err)

		branch2 = sh.Engine.GetBranch("branch2")
		mergedHistory := branch2.GetMergedDownstack()
		require.Len(t, mergedHistory, 1)
		require.Equal(t, "branch1", mergedHistory[0].BranchName)
	})
}

func TestMergedDownstackDisplayFormat(t *testing.T) {
	t.Parallel()
	t.Run("renders merged history line with PR number", func(t *testing.T) {
		t.Parallel()
		// Create a mock renderer
		data := &mockTreeData{
			currentBranch: "branch2",
			trunk:         "main",
			children:      map[string][]string{"main": {"branch2"}},
			parents:       map[string]string{"branch2": "main"},
		}
		renderer := tree.NewRenderer(data)

		// Set annotation with merged history
		prNum := 123
		ann := tree.BranchAnnotation{
			MergedDownstack: []tree.MergedParentDisplay{
				{BranchName: "branch1", PRNumber: &prNum, PRState: "MERGED"},
			},
		}
		renderer.SetAnnotation("branch2", ann)

		// Render the tree
		opts := tree.RenderOptions{Mode: tree.RenderModeFull}
		lines := renderer.RenderStack("branch2", opts)

		// Verify merged history line is present (should contain "previously based on")
		found := false
		for _, line := range lines {
			if containsAny(line, "previously based on") {
				found = true
				break
			}
		}
		require.True(t, found, "Should contain 'previously based on' in output, got: %v", lines)
	})
}

// mockTreeData implements tree.Data for testing
type mockTreeData struct {
	currentBranch string
	trunk         string
	children      map[string][]string
	parents       map[string]string
}

func (d *mockTreeData) CurrentBranch() string               { return d.currentBranch }
func (d *mockTreeData) Trunk() string                       { return d.trunk }
func (d *mockTreeData) Children(branchName string) []string { return d.children[branchName] }
func (d *mockTreeData) Parent(branchName string) string     { return d.parents[branchName] }
func (d *mockTreeData) IsTrunk(branchName string) bool      { return branchName == d.trunk }
func (d *mockTreeData) IsFixed(_ string) bool               { return true }

func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if contains(s, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstr(s, substr)
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
