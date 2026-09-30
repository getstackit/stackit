package tree

import (
	"testing"

	"github.com/getstackit/stackit/internal/git"
)

const benchTrunk = "main"

// generateLargeTree creates a tree with the specified number of branches
// arranged in a binary tree structure for testing performance.
func generateLargeTree(numBranches int) *MockTreeData {
	childrenMap := make(map[string][]string)
	parentsMap := make(map[string]string)
	fixedMap := make(map[string]bool)

	// Create branch names
	branches := make([]string, numBranches)
	branches[0] = benchTrunk
	for i := 1; i < numBranches; i++ {
		branches[i] = branchName(i)
	}

	// Build binary tree structure
	fixedMap[benchTrunk] = true
	for i := 1; i < numBranches; i++ {
		branch := branches[i]
		parentIdx := (i - 1) / 2
		parent := branches[parentIdx]

		parentsMap[branch] = parent
		childrenMap[parent] = append(childrenMap[parent], branch)
		fixedMap[branch] = true
	}

	// Initialize empty children lists for leaf nodes
	for _, branch := range branches {
		if _, exists := childrenMap[branch]; !exists {
			childrenMap[branch] = []string{}
		}
	}

	return &MockTreeData{
		CurrentVal:  branches[numBranches/2], // Middle branch
		TrunkVal:    benchTrunk,
		ChildrenMap: childrenMap,
		ParentsMap:  parentsMap,
		FixedMap:    fixedMap,
	}
}

func branchName(i int) string {
	// Generate branch names like "branch-1", "branch-2", etc.
	return "branch-" + string(rune('0'+i%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i/100%10))
}

// generateDeepLinearTree creates a deep linear stack (no branching).
func generateDeepLinearTree(depth int) *MockTreeData {
	childrenMap := make(map[string][]string)
	parentsMap := make(map[string]string)
	fixedMap := make(map[string]bool)

	branches := make([]string, depth)
	branches[0] = benchTrunk
	for i := 1; i < depth; i++ {
		branches[i] = branchName(i)
	}

	fixedMap[benchTrunk] = true
	for i := 1; i < depth; i++ {
		branch := branches[i]
		parent := branches[i-1]
		parentsMap[branch] = parent
		childrenMap[parent] = []string{branch}
		fixedMap[branch] = true
	}
	childrenMap[branches[depth-1]] = []string{}

	return &MockTreeData{
		CurrentVal:  branches[depth-1],
		TrunkVal:    benchTrunk,
		ChildrenMap: childrenMap,
		ParentsMap:  parentsMap,
		FixedMap:    fixedMap,
	}
}

func BenchmarkRenderStack_SmallTree(b *testing.B) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeCompact})
	}
}

func BenchmarkRenderStack_SmallTree_Full(b *testing.B) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeFull})
	}
}

func BenchmarkRenderStack_LargeTree_100Branches(b *testing.B) {
	mock := generateLargeTree(100)
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeCompact})
	}
}

func BenchmarkRenderStack_LargeTree_100Branches_Full(b *testing.B) {
	mock := generateLargeTree(100)
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeFull})
	}
}

func BenchmarkRenderStack_DeepLinear_50Levels(b *testing.B) {
	mock := generateDeepLinearTree(50)
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeCompact})
	}
}

func BenchmarkRenderStack_DeepLinear_50Levels_Full(b *testing.B) {
	mock := generateDeepLinearTree(50)
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeFull})
	}
}

func BenchmarkRenderStack_WithAnnotations(b *testing.B) {
	mock := generateLargeTree(50)
	renderer := NewRenderer(mock)

	// Add annotations to all branches
	prNum := git.PRNumber(1)
	for branch := range mock.ChildrenMap {
		renderer.SetAnnotation(branch, BranchAnnotation{
			PRNumber:     &prNum,
			CheckStatus:  CheckStatusPassing,
			ReviewStatus: "Approved",
			CommitCount:  3,
			LinesAdded:   100,
			LinesDeleted: 20,
			Scope:        "test",
		})
		prNum++
	}

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeFull})
	}
}

func BenchmarkRenderStackDetailed(b *testing.B) {
	mock := generateLargeTree(50)
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStackDetailed(mock.TrunkVal, RenderOptions{Mode: RenderModeFull})
	}
}

// BenchmarkRenderStackCached_Small tests the cache building time for a small tree.
func BenchmarkRenderStackCached_Small(b *testing.B) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStackCached(mock.TrunkVal, RenderOptions{Mode: RenderModeSelect})
	}
}

// BenchmarkRenderStackCached_Large tests the cache building time for a larger tree.
func BenchmarkRenderStackCached_Large(b *testing.B) {
	mock := generateLargeTree(50)
	renderer := NewRenderer(mock)

	b.ResetTimer()
	for b.Loop() {
		renderer.RenderStackCached(mock.TrunkVal, RenderOptions{Mode: RenderModeSelect})
	}
}

// BenchmarkApplySelection_Small tests the fast path for selection updates
// on a small tree - this should be very fast since it only updates cursor.
func BenchmarkApplySelection_Small(b *testing.B) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	// Pre-cache the tree
	cached := renderer.RenderStackCached(mock.TrunkVal, RenderOptions{Mode: RenderModeSelect})

	b.ResetTimer()
	for b.Loop() {
		cached.ApplySelection("feature-1")
	}
}

// BenchmarkApplySelection_Large tests the fast path for selection updates
// on a larger tree.
func BenchmarkApplySelection_Large(b *testing.B) {
	mock := generateLargeTree(100)
	renderer := NewRenderer(mock)

	// Pre-cache the tree
	cached := renderer.RenderStackCached(mock.TrunkVal, RenderOptions{Mode: RenderModeSelect})

	// Select a branch in the middle
	selectedBranch := branchName(50)

	b.ResetTimer()
	for b.Loop() {
		cached.ApplySelection(selectedBranch)
	}
}

// BenchmarkFullRender_vs_CachedSelection compares full render vs cached selection
func BenchmarkFullRender_vs_CachedSelection(b *testing.B) {
	mock := generateLargeTree(50)
	renderer := NewRenderer(mock)

	// Add annotations to make rendering more realistic
	prNum := git.PRNumber(1)
	for branch := range mock.ChildrenMap {
		renderer.SetAnnotation(branch, BranchAnnotation{
			PRNumber:     &prNum,
			CheckStatus:  CheckStatusPassing,
			ReviewStatus: "Approved",
			CommitCount:  3,
			LinesAdded:   100,
			LinesDeleted: 20,
			Scope:        "test",
		})
		prNum++
	}

	b.Run("full_render", func(b *testing.B) {
		for b.Loop() {
			renderer.RenderStackDetailed(mock.TrunkVal, RenderOptions{
				Mode:           RenderModeSelect,
				SelectedBranch: branchName(25),
			})
		}
	})

	b.Run("cached_then_apply", func(b *testing.B) {
		// Pre-cache the tree once (this would happen on first render or after invalidation)
		cached := renderer.RenderStackCached(mock.TrunkVal, RenderOptions{Mode: RenderModeSelect})

		b.ResetTimer()
		for b.Loop() {
			cached.ApplySelection(branchName(25))
		}
	})
}

// BenchmarkNavigationSimulation simulates navigating through a tree
func BenchmarkNavigationSimulation(b *testing.B) {
	mock := generateLargeTree(50)
	renderer := NewRenderer(mock)

	// Add annotations
	prNum := git.PRNumber(1)
	for branch := range mock.ChildrenMap {
		renderer.SetAnnotation(branch, BranchAnnotation{
			PRNumber:    &prNum,
			CheckStatus: CheckStatusPassing,
			Scope:       "feat",
		})
		prNum++
	}

	b.Run("old_approach_full_render_each_nav", func(b *testing.B) {
		// Simulate the old approach: full render on each navigation
		for b.Loop() {
			// Simulate navigating down 10 times
			for i := range 10 {
				renderer.RenderStackDetailed(mock.TrunkVal, RenderOptions{
					Mode:           RenderModeSelect,
					SelectedBranch: branchName(i),
				})
			}
		}
	})

	b.Run("new_approach_cached_selection", func(b *testing.B) {
		// Simulate the new approach: cache once, apply selection on each navigation
		cached := renderer.RenderStackCached(mock.TrunkVal, RenderOptions{Mode: RenderModeSelect})

		b.ResetTimer()
		for b.Loop() {
			// Simulate navigating down 10 times
			for i := range 10 {
				cached.ApplySelection(branchName(i))
			}
		}
	})
}
