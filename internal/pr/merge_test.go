package pr

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/getstackit/stackit/internal/git"
)

func TestFormatMergeTitleWithDescription(t *testing.T) {
	t.Parallel()

	t.Run("uses description title when present", func(t *testing.T) {
		t.Parallel()
		desc := &git.StackDescription{Title: "Add user authentication"}
		result := FormatMergeTitleWithDescription(desc, []string{"PROJ-123"}, 2)
		assert.Equal(t, "Add user authentication", result)
	})

	t.Run("falls back when description is nil", func(t *testing.T) {
		t.Parallel()
		result := FormatMergeTitleWithDescription(nil, []string{"PROJ-123"}, 1)
		assert.Equal(t, "Merging PROJ-123", result)
	})

	t.Run("falls back when description title is empty", func(t *testing.T) {
		t.Parallel()
		desc := &git.StackDescription{Title: "", Description: "Some description"}
		result := FormatMergeTitleWithDescription(desc, []string{}, 3)
		assert.Equal(t, "Merging 3 PRs", result)
	})
}

func TestFormatMergeTitle(t *testing.T) {
	tests := []struct {
		name       string
		scopes     []string
		totalCount int
		expected   string
	}{
		{
			name:       "no scopes - fallback to PR count",
			scopes:     []string{"", "", ""},
			totalCount: 3,
			expected:   "Merging 3 PRs",
		},
		{
			name:       "empty scopes slice",
			scopes:     []string{},
			totalCount: 2,
			expected:   "Merging 2 PRs",
		},
		{
			name:       "single scope",
			scopes:     []string{"PROJ-123"},
			totalCount: 1,
			expected:   "Merging PROJ-123",
		},
		{
			name:       "multiple scopes all present",
			scopes:     []string{"PROJ-123", "PROJ-124"},
			totalCount: 2,
			expected:   "Merging PROJ-123, PROJ-124",
		},
		{
			name:       "multiple scopes with unscoped branches",
			scopes:     []string{"PROJ-123", "", "PROJ-124", ""},
			totalCount: 4,
			expected:   "Merging PROJ-123, PROJ-124 (+2)",
		},
		{
			name:       "single scope with unscoped branches",
			scopes:     []string{"PROJ-123", "", ""},
			totalCount: 3,
			expected:   "Merging PROJ-123 (+2)",
		},
		{
			name:       "duplicate scopes are deduplicated",
			scopes:     []string{"PROJ-123", "PROJ-123", "PROJ-124"},
			totalCount: 3,
			expected:   "Merging PROJ-123, PROJ-124 (+1)",
		},
		{
			name:       "preserves scope order",
			scopes:     []string{"FEAT-999", "BUG-001", "PROJ-123"},
			totalCount: 3,
			expected:   "Merging FEAT-999, BUG-001, PROJ-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatMergeTitle(tt.scopes, tt.totalCount)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatMergeBody(t *testing.T) {
	t.Run("single branch with PR", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 123, PRTitle: "Add feature A"},
			},
			StackTree: "main\n  └─ feature-a (#123)\n",
		})

		expected := `This PR merges the following changes:

1. **#123** Add feature A

### Stack

` + "```" + `
main
  └─ feature-a (#123)
` + "```" + `

Stackit-Stack-Size: 1
Stackit-PRs: 123
`
		assert.Equal(t, expected, result)
	})

	t.Run("multiple branches with PRs", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 1, PRTitle: "Add feature A"},
				{Name: "feature-a-1", PRNumber: 2, PRTitle: "Extend feature A"},
			},
			StackTree: "main\n  └─ feature-a (#1)\n    └─ feature-a-1 (#2)\n",
		})

		expected := `This PR merges the following changes:

1. **#1** Add feature A
2. **#2** Extend feature A

### Stack

` + "```" + `
main
  └─ feature-a (#1)
    └─ feature-a-1 (#2)
` + "```" + `

Stackit-Stack-Size: 2
Stackit-PRs: 1,2
`
		assert.Equal(t, expected, result)
	})

	t.Run("branch without PR", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 0, PRTitle: ""},
			},
			StackTree: "main\n  └─ feature-a\n",
		})

		expected := `This PR merges the following changes:

1. feature-a

### Stack

` + "```" + `
main
  └─ feature-a
` + "```" + `

Stackit-Stack-Size: 1
`
		assert.Equal(t, expected, result)
	})

	t.Run("with excluded branches", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 1, PRTitle: "Add feature A"},
			},
			Excluded: []ExcludedBranch{
				{Name: "feature-b", Reason: "merge conflict"},
				{Name: "feature-c", Reason: "CI failure"},
			},
			StackTree: "main\n  └─ feature-a (#1)\n",
		})

		expected := `This PR merges the following changes:

1. **#1** Add feature A

### Excluded

- **feature-b** — merge conflict
- **feature-c** — CI failure

### Stack

` + "```" + `
main
  └─ feature-a (#1)
` + "```" + `

Stackit-Stack-Size: 1
Stackit-PRs: 1
`
		assert.Equal(t, expected, result)
	})

	t.Run("no stack tree", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 1, PRTitle: "Add feature A"},
			},
		})

		expected := `This PR merges the following changes:

1. **#1** Add feature A

Stackit-Stack-Size: 1
Stackit-PRs: 1
`
		assert.Equal(t, expected, result)
	})

	t.Run("with stack description", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 1, PRTitle: "Add feature A"},
			},
			StackTree: "main\n  └─ feature-a (#1)\n",
			StackDescription: &git.StackDescription{
				Title:       "Add user authentication",
				Description: "This stack implements JWT-based auth.",
			},
			Metadata: StackMetadata{Scope: "PROJ-123"},
		})

		expected := `**Add user authentication**

This stack implements JWT-based auth.

This PR merges the following changes:

1. **#1** Add feature A

### Stack

` + "```" + `
main
  └─ feature-a (#1)
` + "```" + `

Stackit-Stack-Size: 1
Stackit-PRs: 1
Stackit-Scope: PROJ-123
`
		assert.Equal(t, expected, result)
	})

	t.Run("with stack description title only", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 1, PRTitle: "Add feature A"},
			},
			StackTree: "main\n  └─ feature-a (#1)\n",
			StackDescription: &git.StackDescription{
				Title: "Add user authentication",
			},
			Metadata: StackMetadata{Scope: "PROJ-456"},
		})

		expected := `**Add user authentication**

This PR merges the following changes:

1. **#1** Add feature A

### Stack

` + "```" + `
main
  └─ feature-a (#1)
` + "```" + `

Stackit-Stack-Size: 1
Stackit-PRs: 1
Stackit-Scope: PROJ-456
`
		assert.Equal(t, expected, result)
	})

	t.Run("with empty stack description", func(t *testing.T) {
		result := FormatMergeBody(MergeBodyParams{
			Branches: []MergeBranch{
				{Name: "feature-a", PRNumber: 1, PRTitle: "Add feature A"},
			},
			StackTree:        "main\n  └─ feature-a (#1)\n",
			StackDescription: &git.StackDescription{},
		})

		// Should fall back to no description
		expected := `This PR merges the following changes:

1. **#1** Add feature A

### Stack

` + "```" + `
main
  └─ feature-a (#1)
` + "```" + `

Stackit-Stack-Size: 1
Stackit-PRs: 1
`
		assert.Equal(t, expected, result)
	})
}

func TestFormatStackTree(t *testing.T) {
	t.Run("single branch", func(t *testing.T) {
		result := FormatStackTree(StackTreeParams{
			TrunkName: "main",
			Branches: []StackTreeBranch{
				{Name: "feature-a", Depth: 0, PRNumber: 123},
			},
		})

		expected := `main
  └─ feature-a (#123)
`
		assert.Equal(t, expected, result)
	})

	t.Run("nested branches", func(t *testing.T) {
		result := FormatStackTree(StackTreeParams{
			TrunkName: "main",
			Branches: []StackTreeBranch{
				{Name: "feature-a", Depth: 0, PRNumber: 1},
				{Name: "feature-a-1", Depth: 1, PRNumber: 2},
				{Name: "feature-a-1-1", Depth: 2, PRNumber: 3},
			},
		})

		expected := `main
  └─ feature-a (#1)
    └─ feature-a-1 (#2)
      └─ feature-a-1-1 (#3)
`
		assert.Equal(t, expected, result)
	})

	t.Run("branch without PR", func(t *testing.T) {
		result := FormatStackTree(StackTreeParams{
			TrunkName: "main",
			Branches: []StackTreeBranch{
				{Name: "feature-a", Depth: 0, PRNumber: 0},
			},
		})

		expected := `main
  └─ feature-a
`
		assert.Equal(t, expected, result)
	})
}
