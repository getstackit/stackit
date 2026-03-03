package tree

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// ansiRegex matches ANSI escape sequences
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes ANSI escape sequences from a string for readable golden files
func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// goldenTest represents a single golden file test case
type goldenTest struct {
	name        string
	mock        *MockTreeData
	annotations map[string]BranchAnnotation
	opts        RenderOptions
}

// buildGoldenTests returns all test cases for golden file testing
func buildGoldenTests() []goldenTest {
	return []goldenTest{
		// Basic linear stacks
		{
			name: "linear_short",
			mock: NewMockTreeData(),
			opts: RenderOptions{Mode: RenderModeCompact},
		},
		{
			name: "linear_full",
			mock: NewMockTreeData(),
			opts: RenderOptions{Mode: RenderModeFull},
		},
		{
			name: "linear_single_line",
			mock: NewMockTreeData(),
			opts: RenderOptions{Mode: RenderModeSelect},
		},

		// Branching stacks
		{
			name: "branching_short",
			mock: &MockTreeData{
				CurrentVal: "feature-1a",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":       {"feature-1a", "feature-1b"},
					"feature-1a": {},
					"feature-1b": {},
				},
				ParentsMap: map[string]string{
					"feature-1a": "main",
					"feature-1b": "main",
				},
				FixedMap: map[string]bool{
					"main":       true,
					"feature-1a": true,
					"feature-1b": true,
				},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},
		{
			name: "branching_full",
			mock: &MockTreeData{
				CurrentVal: "feature-1a",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":       {"feature-1a", "feature-1b"},
					"feature-1a": {},
					"feature-1b": {},
				},
				ParentsMap: map[string]string{
					"feature-1a": "main",
					"feature-1b": "main",
				},
				FixedMap: map[string]bool{
					"main":       true,
					"feature-1a": true,
					"feature-1b": true,
				},
			},
			opts: RenderOptions{Mode: RenderModeFull},
		},

		// Deep branching
		{
			name: "deep_branching",
			mock: &MockTreeData{
				CurrentVal: "child-1",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":       {"branch-a", "branch-b", "branch-c"},
					"branch-a":   {"child-1", "child-2"},
					"branch-b":   {},
					"branch-c":   {"child-3"},
					"child-1":    {},
					"child-2":    {},
					"child-3":    {"grandchild"},
					"grandchild": {},
				},
				ParentsMap: map[string]string{
					"branch-a":   "main",
					"branch-b":   "main",
					"branch-c":   "main",
					"child-1":    "branch-a",
					"child-2":    "branch-a",
					"child-3":    "branch-c",
					"grandchild": "child-3",
				},
				FixedMap: map[string]bool{
					"main":       true,
					"branch-a":   true,
					"branch-b":   true,
					"branch-c":   true,
					"child-1":    true,
					"child-2":    true,
					"child-3":    true,
					"grandchild": true,
				},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},

		// With selection cursor
		{
			name: "with_selection",
			mock: NewMockTreeData(),
			opts: RenderOptions{
				Mode:           RenderModeCompact,
				SelectedBranch: "feature-1",
			},
		},
		{
			name: "with_selection_full",
			mock: NewMockTreeData(),
			opts: RenderOptions{
				Mode:           RenderModeFull,
				SelectedBranch: "feature-1",
			},
		},

		// With annotations
		{
			name: "with_pr_numbers",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {PRNumber: new(123)},
				"feature-2": {PRNumber: new(456)},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},
		{
			name: "with_check_status",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {CheckStatus: CheckStatusPassing},
				"feature-2": {CheckStatus: CheckStatusFailing},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},
		{
			name: "with_full_annotations",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {
					PRNumber:     new(123),
					CheckStatus:  CheckStatusPassing,
					ReviewStatus: "Approved",
					CommitCount:  3,
					LinesAdded:   50,
					LinesDeleted: 10,
				},
				"feature-2": {
					PRNumber:     new(456),
					CheckStatus:  CheckStatusPending,
					IsDraft:      true,
					CommitCount:  1,
					LinesAdded:   25,
					LinesDeleted: 0,
				},
			},
			opts: RenderOptions{Mode: RenderModeFull},
		},

		// Needs restack indicator
		{
			name: "needs_restack",
			mock: &MockTreeData{
				CurrentVal: "feature-1",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":      {"feature-1"},
					"feature-1": {},
				},
				ParentsMap: map[string]string{
					"feature-1": "main",
				},
				FixedMap: map[string]bool{
					"main":      true,
					"feature-1": false, // Not fixed
				},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},

		// Merged/Closed PRs (dimmed)
		{
			name: "merged_pr",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {PRNumber: new(123), PRState: PRStateMerged},
			},
			opts: RenderOptions{Mode: RenderModeFull},
		},
		{
			name: "closed_pr",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {PRNumber: new(123), PRState: PRStateClosed},
			},
			opts: RenderOptions{Mode: RenderModeFull},
		},

		// With scopes
		{
			name: "with_scopes",
			mock: &MockTreeData{
				CurrentVal: "feature-login",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":              {"feature-auth-base", "feature-api"},
					"feature-auth-base": {"feature-login"},
					"feature-api":       {},
					"feature-login":     {},
				},
				ParentsMap: map[string]string{
					"feature-auth-base": "main",
					"feature-login":     "feature-auth-base",
					"feature-api":       "main",
				},
				FixedMap: map[string]bool{
					"main":              true,
					"feature-auth-base": true,
					"feature-login":     true,
					"feature-api":       true,
				},
			},
			annotations: map[string]BranchAnnotation{
				"feature-auth-base": {Scope: "AUTH", ExplicitScope: "AUTH"},
				"feature-login":     {Scope: "AUTH"},
				"feature-api":       {Scope: "API", ExplicitScope: "API"},
			},
			opts: RenderOptions{Mode: RenderModeFull},
		},

		// Non-selectable branches
		{
			name: "non_selectable",
			mock: NewMockTreeData(),
			opts: RenderOptions{
				Mode:           RenderModeCompact,
				SelectedBranch: "feature-2",
				NonSelectable: map[string]bool{
					"feature-1": true,
				},
			},
		},

		// Custom labels
		{
			name: "custom_labels",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {CustomLabel: "<---- source branch"},
				"feature-2": {CustomLabel: "(will be moved)"},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},

		// Locked and frozen
		{
			name: "locked_frozen",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {IsLocked: true},
				"feature-2": {IsFrozen: true},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},

		// Hide stats
		{
			name: "hide_stats",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {CommitCount: 5, LinesAdded: 100, LinesDeleted: 20},
				"feature-2": {CommitCount: 3, LinesAdded: 50, LinesDeleted: 10},
			},
			opts: RenderOptions{Mode: RenderModeFull, HideStats: true},
		},

		// Hide summary
		{
			name: "hide_summary",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {PRNumber: new(123), CommitCount: 5},
				"feature-2": {PRNumber: new(456), CommitCount: 3},
			},
			opts: RenderOptions{Mode: RenderModeFull, HideSummary: true},
		},

		// RenderMode tests (new enum-based API)
		{
			name: "mode_full",
			mock: NewMockTreeData(),
			opts: RenderOptions{Mode: RenderModeFull},
		},
		{
			name: "mode_compact",
			mock: NewMockTreeData(),
			opts: RenderOptions{Mode: RenderModeCompact},
		},
		{
			name: "mode_select",
			mock: NewMockTreeData(),
			opts: RenderOptions{Mode: RenderModeSelect},
		},
		{
			name: "mode_compact_with_annotations",
			mock: NewMockTreeData(),
			annotations: map[string]BranchAnnotation{
				"feature-1": {PRNumber: new(123), CheckStatus: CheckStatusPassing},
				"feature-2": {PRNumber: new(456), CheckStatus: CheckStatusFailing},
			},
			opts: RenderOptions{Mode: RenderModeCompact},
		},

		// Split direction previews - linear stack with virtual branch inserted
		// These simulate the split command's direction selection UI
		// Uses full format (not short) with HideSummary to show clean │ connectors

		// Split below: insert [new branch] between feature-1 and feature-2
		// main → feature-1 → [new branch] → feature-2 (current)
		{
			name: "split_below_linear",
			mock: &MockTreeData{
				CurrentVal: "feature-2",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":         {"feature-1"},
					"feature-1":    {"[new branch]"},
					"[new branch]": {"feature-2"},
					"feature-2":    {},
				},
				ParentsMap: map[string]string{
					"feature-1":    "main",
					"[new branch]": "feature-1",
					"feature-2":    "[new branch]",
				},
				FixedMap: map[string]bool{
					"main":         true,
					"feature-1":    true,
					"[new branch]": true,
					"feature-2":    true,
				},
			},
			annotations: map[string]BranchAnnotation{
				"feature-2":    {CustomLabel: "← current"},
				"[new branch]": {CustomLabel: "← new"},
			},
			opts: RenderOptions{Mode: RenderModeFull, HideSummary: true, SkipSelectionPrefix: true},
		},

		// Split above: insert [new branch] as child of feature-2 (current)
		// main → feature-1 → feature-2 (current) → [new branch]
		{
			name: "split_above_linear",
			mock: &MockTreeData{
				CurrentVal: "feature-2",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":         {"feature-1"},
					"feature-1":    {"feature-2"},
					"feature-2":    {"[new branch]"},
					"[new branch]": {},
				},
				ParentsMap: map[string]string{
					"feature-1":    "main",
					"feature-2":    "feature-1",
					"[new branch]": "feature-2",
				},
				FixedMap: map[string]bool{
					"main":         true,
					"feature-1":    true,
					"feature-2":    true,
					"[new branch]": true,
				},
			},
			annotations: map[string]BranchAnnotation{
				"feature-2":    {CustomLabel: "← current"},
				"[new branch]": {CustomLabel: "← new"},
			},
			opts: RenderOptions{Mode: RenderModeFull, HideSummary: true, SkipSelectionPrefix: true},
		},

		// Split above with re-parented children
		// main → feature-1 → feature-2 (current) → [new branch] → child-1
		{
			name: "split_above_with_children",
			mock: &MockTreeData{
				CurrentVal: "feature-2",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":         {"feature-1"},
					"feature-1":    {"feature-2"},
					"feature-2":    {"[new branch]"},
					"[new branch]": {"child-1"},
					"child-1":      {},
				},
				ParentsMap: map[string]string{
					"feature-1":    "main",
					"feature-2":    "feature-1",
					"[new branch]": "feature-2",
					"child-1":      "[new branch]",
				},
				FixedMap: map[string]bool{
					"main":         true,
					"feature-1":    true,
					"feature-2":    true,
					"[new branch]": true,
					"child-1":      true,
				},
			},
			annotations: map[string]BranchAnnotation{
				"feature-2":    {CustomLabel: "← current"},
				"[new branch]": {CustomLabel: "← new"},
				"child-1":      {CustomLabel: "(re-parented)"},
			},
			opts: RenderOptions{Mode: RenderModeFull, HideSummary: true, SkipSelectionPrefix: true},
		},
	}
}

func TestStackTreeRenderer_Golden(t *testing.T) {
	tests := buildGoldenTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := NewRenderer(tt.mock)

			// Apply annotations if provided
			if tt.annotations != nil {
				renderer.SetAnnotations(tt.annotations)
			}

			lines := renderer.RenderStack(tt.mock.TrunkVal, tt.opts)
			output := strings.Join(lines, "\n")

			// Strip ANSI codes for readable golden files
			cleanOutput := stripANSI(output)

			goldenPath := filepath.Join("testdata", tt.name+".golden")

			if *update {
				if err := os.WriteFile(goldenPath, []byte(cleanOutput), 0644); err != nil {
					t.Fatalf("failed to update golden file: %v", err)
				}
				t.Logf("updated golden file: %s", goldenPath)
				return
			}

			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				if os.IsNotExist(err) {
					t.Fatalf("golden file does not exist: %s\nRun with -update to create it.\nActual output:\n%s", goldenPath, cleanOutput)
				}
				t.Fatalf("failed to read golden file: %v", err)
			}

			if cleanOutput != string(expected) {
				t.Errorf("output mismatch for %s\n\nGot:\n%s\n\nWant:\n%s\n\nDiff:\n%s",
					tt.name,
					cleanOutput,
					string(expected),
					diffStrings(string(expected), cleanOutput))
			}
		})
	}
}

// diffStrings provides a simple line-by-line diff for debugging
func diffStrings(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")

	var diff strings.Builder
	maxLines := max(len(expectedLines), len(actualLines))

	for i := range maxLines {
		var expLine, actLine string
		if i < len(expectedLines) {
			expLine = expectedLines[i]
		}
		if i < len(actualLines) {
			actLine = actualLines[i]
		}

		if expLine != actLine {
			diff.WriteString("- ")
			diff.WriteString(expLine)
			diff.WriteString("\n+ ")
			diff.WriteString(actLine)
			diff.WriteString("\n")
		}
	}

	return diff.String()
}

// TestStackTreeRenderer_GoldenWithColors tests that colors are applied correctly
// This test verifies ANSI codes are present without checking exact sequences
func TestStackTreeRenderer_GoldenWithColors(t *testing.T) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	prNum := 123
	renderer.SetAnnotation("feature-1", BranchAnnotation{
		PRNumber:    &prNum,
		CheckStatus: CheckStatusPassing,
		Scope:       "AUTH",
	})

	lines := renderer.RenderStack(mock.TrunkVal, RenderOptions{Mode: RenderModeFull})
	output := strings.Join(lines, "\n")

	// Verify ANSI codes are present (colors are being applied)
	if !strings.Contains(output, "\x1b[") {
		t.Error("expected output to contain ANSI escape codes for colors")
	}

	// Verify stripping works
	clean := stripANSI(output)
	if strings.Contains(clean, "\x1b[") {
		t.Error("stripANSI failed to remove all escape codes")
	}
}
