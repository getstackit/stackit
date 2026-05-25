package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
)

func TestFindConflictMarkerSections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected []conflictMarkerSection
	}{
		{
			name: "single section",
			content: strings.Join([]string{
				"before",
				"<<<<<<< HEAD",
				"base",
				"=======",
				"branch",
				">>>>>>> feature",
				"after",
			}, "\n"),
			expected: []conflictMarkerSection{{StartLine: 2, EndLine: 6}},
		},
		{
			name: "multiple sections",
			content: strings.Join([]string{
				"<<<<<<< HEAD",
				"a",
				"=======",
				"b",
				">>>>>>> branch-a",
				"middle",
				"<<<<<<< HEAD",
				"c",
				"=======",
				"d",
				">>>>>>> branch-b",
			}, "\n"),
			expected: []conflictMarkerSection{
				{StartLine: 1, EndLine: 5},
				{StartLine: 7, EndLine: 11},
			},
		},
		{
			name: "no complete section",
			content: strings.Join([]string{
				"<<<<<<< HEAD",
				"base",
				"=======",
				"branch",
			}, "\n"),
			expected: []conflictMarkerSection{},
		},
		{
			name: "diff3 style with ancestor marker",
			content: strings.Join([]string{
				"<<<<<<< HEAD",
				"ours",
				"||||||| common ancestor",
				"base",
				"=======",
				"theirs",
				">>>>>>> branch",
			}, "\n"),
			expected: []conflictMarkerSection{{StartLine: 1, EndLine: 7}},
		},
		{
			name: "unterminated section then complete section",
			content: strings.Join([]string{
				"<<<<<<< HEAD",
				"unterminated",
				"<<<<<<< HEAD",
				"a",
				"=======",
				"b",
				">>>>>>> branch",
			}, "\n"),
			expected: []conflictMarkerSection{{StartLine: 3, EndLine: 7}},
		},
		{
			name: "separator before any start marker is ignored",
			content: strings.Join([]string{
				"heading",
				"=======",
				"more text",
				"<<<<<<< HEAD",
				"a",
				"=======",
				"b",
				">>>>>>> branch",
			}, "\n"),
			expected: []conflictMarkerSection{{StartLine: 4, EndLine: 8}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.expected, findConflictMarkerSections(tt.content))
		})
	}
}

func TestConflictMarkerSectionsForFile(t *testing.T) {
	t.Parallel()

	t.Run("reads file via relative path joined to repo root", func(t *testing.T) {
		t.Parallel()
		repoRoot := t.TempDir()
		rel := filepath.Join("sub", "file.txt")
		require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "sub"), 0o755))
		content := strings.Join([]string{
			"<<<<<<< HEAD",
			"a",
			"=======",
			"b",
			">>>>>>> branch",
		}, "\n")
		require.NoError(t, os.WriteFile(filepath.Join(repoRoot, rel), []byte(content), 0o644))

		sections, err := conflictMarkerSectionsForFile(repoRoot, rel)
		require.NoError(t, err)
		require.Equal(t, []conflictMarkerSection{{StartLine: 1, EndLine: 5}}, sections)
	})

	t.Run("absolute path is used as-is", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		abs := filepath.Join(dir, "file.txt")
		require.NoError(t, os.WriteFile(abs, []byte("no markers here"), 0o644))

		sections, err := conflictMarkerSectionsForFile("/unrelated/root", abs)
		require.NoError(t, err)
		require.Empty(t, sections)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		t.Parallel()
		_, err := conflictMarkerSectionsForFile(t.TempDir(), "does-not-exist.txt")
		require.Error(t, err)
	})
}

func TestClassifyValidatedBranches(t *testing.T) {
	t.Parallel()

	mkBranch := func(name string) engine.Branch {
		return engine.NewBranch(name, nil)
	}
	mkPlan := func(items map[string]engine.RestackPlanAction) *engine.RestackPlan {
		plan := &engine.RestackPlan{
			Items:    map[string]engine.RestackPlanItem{},
			ApplyMap: map[string]bool{},
		}
		for name, action := range items {
			plan.Items[name] = engine.RestackPlanItem{Branch: name, Action: action}
			plan.ApplyMap[name] = true
		}
		return plan
	}

	t.Run("all validated branches succeed when each has a NewSHA", func(t *testing.T) {
		t.Parallel()
		plan := mkPlan(map[string]engine.RestackPlanAction{
			"a": engine.RestackPlanApplyValidated,
			"b": engine.RestackPlanApplyValidated,
		})
		validation := &engine.RebaseValidation{
			Success: true,
			NewSHAs: map[string]string{"a": "sha-a", "b": "sha-b"},
		}

		success, conflicts := classifyValidatedBranches(
			engine.BranchesOf(mkBranch("a"), mkBranch("b")),
			plan,
			validation,
		)

		require.Empty(t, conflicts)
		require.Len(t, success, 2)
		require.Equal(t, "a", success[0].GetName())
		require.Equal(t, "b", success[1].GetName())
	})

	t.Run("frozen and anchor branches succeed without a NewSHA", func(t *testing.T) {
		t.Parallel()
		plan := mkPlan(map[string]engine.RestackPlanAction{
			"frozen": engine.RestackPlanApplyFrozen,
			"anchor": engine.RestackPlanApplyAnchor,
		})
		validation := &engine.RebaseValidation{
			Success: true,
			NewSHAs: map[string]string{},
		}

		success, conflicts := classifyValidatedBranches(
			engine.BranchesOf(mkBranch("frozen"), mkBranch("anchor")),
			plan,
			validation,
		)

		require.Empty(t, conflicts)
		require.Len(t, success, 2)
	})

	// Regression: when sibling validations race in parallel and one fails,
	// canceled siblings have no NewSHA. The previous position-based logic
	// would put an alphabetically-earlier canceled sibling into success
	// (its position in specs was < the failed branch's position) and the
	// engine then errored with "missing validated SHA for X". The fix
	// classifies by capability: only branches with a confirmed NewSHA
	// (or a no-validation action) are eligible.
	t.Run("canceled sibling at same depth is dropped, not added to success", func(t *testing.T) {
		t.Parallel()
		plan := mkPlan(map[string]engine.RestackPlanAction{
			"alpha-canceled": engine.RestackPlanApplyValidated,
			"beta-conflict":  engine.RestackPlanApplyValidated,
			"gamma-ok":       engine.RestackPlanApplyValidated,
		})
		validation := &engine.RebaseValidation{
			Success:      false,
			FailedBranch: "beta-conflict",
			NewSHAs:      map[string]string{"gamma-ok": "sha-gamma"},
		}

		success, conflicts := classifyValidatedBranches(
			engine.BranchesOf(mkBranch("alpha-canceled"), mkBranch("beta-conflict"), mkBranch("gamma-ok")),
			plan,
			validation,
		)

		require.Equal(t, []string{"beta-conflict"}, conflicts)
		require.Len(t, success, 1, "only gamma-ok (with confirmed NewSHA) should succeed")
		require.Equal(t, "gamma-ok", success[0].GetName())
	})

	t.Run("branches not in the plan are ignored", func(t *testing.T) {
		t.Parallel()
		plan := mkPlan(map[string]engine.RestackPlanAction{
			"a": engine.RestackPlanApplyValidated,
		})
		validation := &engine.RebaseValidation{
			Success: true,
			NewSHAs: map[string]string{"a": "sha-a"},
		}

		success, conflicts := classifyValidatedBranches(
			engine.BranchesOf(mkBranch("a"), mkBranch("not-in-plan")),
			plan,
			validation,
		)

		require.Empty(t, conflicts)
		require.Len(t, success, 1)
		require.Equal(t, "a", success[0].GetName())
	})
}
