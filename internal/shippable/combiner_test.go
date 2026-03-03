package shippable

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"stackit.dev/stackit/internal/actions/merge"
)

func TestCombinationResult_IncludedCount(t *testing.T) {
	result := &CombinationResult{
		WorkingStacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "a"}},
			{Stack: merge.MultiStackInfo{RootBranch: "b"}},
		},
	}
	assert.Equal(t, 2, result.IncludedCount())
}

func TestCombinationResult_ExcludedCount(t *testing.T) {
	result := &CombinationResult{
		ConflictingStacks: []ExcludedStack{
			{Stack: Stack{Stack: merge.MultiStackInfo{RootBranch: "c"}}},
		},
	}
	assert.Equal(t, 1, result.ExcludedCount())
}

func TestCombinationResult_AllCombined(t *testing.T) {
	tests := []struct {
		name     string
		result   *CombinationResult
		expected bool
	}{
		{
			name: "all combined",
			result: &CombinationResult{
				WorkingStacks:     []Stack{{Stack: merge.MultiStackInfo{RootBranch: "a"}}},
				ConflictingStacks: nil,
			},
			expected: true,
		},
		{
			name: "some excluded",
			result: &CombinationResult{
				WorkingStacks: []Stack{{Stack: merge.MultiStackInfo{RootBranch: "a"}}},
				ConflictingStacks: []ExcludedStack{
					{Stack: Stack{Stack: merge.MultiStackInfo{RootBranch: "b"}}},
				},
			},
			expected: false,
		},
		{
			name: "empty result",
			result: &CombinationResult{
				WorkingStacks:     nil,
				ConflictingStacks: nil,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result.AllCombined())
		})
	}
}

func TestCombinationResult_GetWorkingRoots(t *testing.T) {
	result := &CombinationResult{
		WorkingStacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "stack-a"}},
			{Stack: merge.MultiStackInfo{RootBranch: "stack-b"}},
		},
	}

	roots := result.GetWorkingRoots()
	assert.Equal(t, []string{"stack-a", "stack-b"}, roots)
}

func TestCombinationResult_GetConflictingRoots(t *testing.T) {
	result := &CombinationResult{
		ConflictingStacks: []ExcludedStack{
			{Stack: Stack{Stack: merge.MultiStackInfo{RootBranch: "stack-c"}}},
			{Stack: Stack{Stack: merge.MultiStackInfo{RootBranch: "stack-d"}}},
		},
	}

	roots := result.GetConflictingRoots()
	assert.Equal(t, []string{"stack-c", "stack-d"}, roots)
}

func TestExclusionReason_Constants(t *testing.T) {
	// Verify exclusion reason constants are defined correctly
	assert.Equal(t, ExclusionReason("merge_conflict"), ReasonMergeConflict)
	assert.Equal(t, ExclusionReason("local_ci_failed"), ReasonLocalCIFailed)
}

func TestUpdateCompatibility(t *testing.T) {
	tests := []struct {
		name                string
		stacks              []Stack
		result              *CombinationResult
		expectedCompatibleA []string
		expectedConflictsA  []string
	}{
		{
			name: "all stacks compatible",
			stacks: []Stack{
				{Stack: merge.MultiStackInfo{RootBranch: "a"}},
				{Stack: merge.MultiStackInfo{RootBranch: "b"}},
				{Stack: merge.MultiStackInfo{RootBranch: "c"}},
			},
			result: &CombinationResult{
				WorkingStacks: []Stack{
					{Stack: merge.MultiStackInfo{RootBranch: "a"}},
					{Stack: merge.MultiStackInfo{RootBranch: "b"}},
					{Stack: merge.MultiStackInfo{RootBranch: "c"}},
				},
				ConflictingStacks: nil,
			},
			expectedCompatibleA: []string{"b", "c"},
			expectedConflictsA:  nil,
		},
		{
			name: "one stack conflicts",
			stacks: []Stack{
				{Stack: merge.MultiStackInfo{RootBranch: "a"}},
				{Stack: merge.MultiStackInfo{RootBranch: "b"}},
				{Stack: merge.MultiStackInfo{RootBranch: "c"}},
			},
			result: &CombinationResult{
				WorkingStacks: []Stack{
					{Stack: merge.MultiStackInfo{RootBranch: "a"}},
					{Stack: merge.MultiStackInfo{RootBranch: "b"}},
				},
				ConflictingStacks: []ExcludedStack{
					{Stack: Stack{Stack: merge.MultiStackInfo{RootBranch: "c"}}},
				},
			},
			expectedCompatibleA: []string{"b"},
			expectedConflictsA:  []string{"c"},
		},
		{
			name: "single stack",
			stacks: []Stack{
				{Stack: merge.MultiStackInfo{RootBranch: "a"}},
			},
			result: &CombinationResult{
				WorkingStacks: []Stack{
					{Stack: merge.MultiStackInfo{RootBranch: "a"}},
				},
			},
			expectedCompatibleA: nil,
			expectedConflictsA:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of stacks to avoid mutating test data
			stacks := make([]Stack, len(tt.stacks))
			copy(stacks, tt.stacks)

			UpdateCompatibility(stacks, tt.result)

			// Find stack "a" and check its compatibility
			for _, s := range stacks {
				if s.RootBranch() == "a" {
					assert.Equal(t, tt.expectedCompatibleA, s.CompatibleWith)
					assert.Equal(t, tt.expectedConflictsA, s.ConflictsWith)
					break
				}
			}
		})
	}
}

func TestCombinationResult_EmptyStacks(t *testing.T) {
	result := &CombinationResult{
		Combinable:        true,
		WorkingStacks:     nil,
		ConflictingStacks: nil,
	}

	assert.Equal(t, 0, result.IncludedCount())
	assert.Equal(t, 0, result.ExcludedCount())
	assert.True(t, result.AllCombined())
	assert.Empty(t, result.GetWorkingRoots())
	assert.Empty(t, result.GetConflictingRoots())
}

func TestCombinationResult_LocalCIFields(t *testing.T) {
	tests := []struct {
		name          string
		localCIPassed *bool
		localCIError  error
		expectPassed  *bool
	}{
		{
			name:          "CI not run",
			localCIPassed: nil,
			localCIError:  nil,
			expectPassed:  nil,
		},
		{
			name:          "CI passed",
			localCIPassed: new(true),
			localCIError:  nil,
			expectPassed:  new(true),
		},
		{
			name:          "CI failed",
			localCIPassed: new(false),
			localCIError:  assert.AnError,
			expectPassed:  new(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &CombinationResult{
				LocalCIPassed: tt.localCIPassed,
				LocalCIError:  tt.localCIError,
			}

			assert.Equal(t, tt.expectPassed, result.LocalCIPassed)
		})
	}
}
