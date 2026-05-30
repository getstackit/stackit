package tree

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/style"
)

func TestStackTreeRenderer_RenderStack_LinearStack(t *testing.T) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeCompact,
	})

	// Should have 3 branches: main, feature-1, feature-2
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	// Check that all branch names appear
	output := strings.Join(lines, "\n")
	for _, branch := range []string{"main", "feature-1", "feature-2"} {
		if !strings.Contains(output, branch) {
			t.Errorf("expected output to contain %q, got: %s", branch, output)
		}
	}
}

func TestStackTreeRenderer_RenderStack_WithAnnotations(t *testing.T) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	prNum := 123
	renderer.SetAnnotation("feature-1", BranchAnnotation{
		PRNumber: &prNum,
		PRAction: "update",
	})

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeCompact,
	})

	output := strings.Join(lines, "\n")
	// Should contain PR number
	if !strings.Contains(output, "#123") {
		t.Errorf("expected output to contain PR number #123, got: %s", output)
	}
}

func TestStackTreeRenderer_RenderStack_BranchingStack(t *testing.T) {
	mock := &MockTreeData{
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
	}

	renderer := NewRenderer(mock)

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeCompact,
	})

	// Should have 3 branches
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	output := strings.Join(lines, "\n")
	// Should contain branching characters for multiple children
	if !strings.Contains(output, "─") {
		t.Errorf("expected output to contain branching characters, got: %s", output)
	}
}

func TestStackTreeRenderer_RenderStack_FullFormat(t *testing.T) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeFull,
	})

	// Full format has more lines (branch line + trailing │ line for each)
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines in full format, got %d: %v", len(lines), lines)
	}

	output := strings.Join(lines, "\n")
	// Should contain the branch circle symbol
	if !strings.Contains(output, "◯") && !strings.Contains(output, "◉") {
		t.Errorf("expected output to contain circle symbols, got: %s", output)
	}
}

func TestStackTreeRenderer_RenderBranchList(t *testing.T) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	prNum := 42
	renderer.SetAnnotation("feature-1", BranchAnnotation{
		PRNumber: &prNum,
	})

	lines := renderer.RenderBranchList([]string{"feature-1", "feature-2"})

	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "feature-1") || !strings.Contains(output, "feature-2") {
		t.Errorf("expected both branches in output, got: %s", output)
	}
}

func TestStackTreeRenderer_NeedsRestack(t *testing.T) {
	mock := &MockTreeData{
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
			"feature-1": false, // Not fixed - restack suggested
		},
	}

	renderer := NewRenderer(mock)

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeCompact,
	})

	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "restack suggested") {
		t.Errorf("expected 'restack suggested' indicator, got: %s", output)
	}
}

func TestBranchAnnotation_CheckStatus(t *testing.T) {
	mock := NewMockTreeData()
	renderer := NewRenderer(mock)

	renderer.SetAnnotation("feature-1", BranchAnnotation{
		CheckStatus: CheckStatusPassing,
	})
	renderer.SetAnnotation("feature-2", BranchAnnotation{
		CheckStatus: CheckStatusFailing,
	})

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeCompact,
	})

	output := strings.Join(lines, "\n")
	// Should contain check icons
	if !strings.Contains(output, "✓") {
		t.Errorf("expected passing check icon ✓, got: %s", output)
	}
	if !strings.Contains(output, "✗") {
		t.Errorf("expected failing check icon ✗, got: %s", output)
	}
}

func TestStackTreeRenderer_ScopeColoring(t *testing.T) {
	mock := &MockTreeData{
		CurrentVal: "feature-login",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":              {"feature-auth-base", "feature-api-v1"},
			"feature-auth-base": {"feature-login"},
			"feature-api-v1":    {},
			"feature-login":     {},
		},
		ParentsMap: map[string]string{
			"feature-auth-base": "main",
			"feature-login":     "feature-auth-base",
			"feature-api-v1":    "main",
		},
		FixedMap: map[string]bool{
			"main":              true,
			"feature-auth-base": true,
			"feature-login":     true,
			"feature-api-v1":    true,
		},
	}

	renderer := NewRenderer(mock)

	// Set scopes
	renderer.SetAnnotation("feature-auth-base", BranchAnnotation{Scope: "AUTH", ExplicitScope: "AUTH"})
	renderer.SetAnnotation("feature-login", BranchAnnotation{Scope: "AUTH"})
	renderer.SetAnnotation("feature-api-v1", BranchAnnotation{Scope: "API", ExplicitScope: "API"})

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeFull,
	})

	output := strings.Join(lines, "\n")

	// Get expected colors
	authHex, _ := style.GetScopeColor("AUTH")

	// Verify that the scope labels are present and colored
	if !strings.Contains(output, "[AUTH]") {
		t.Errorf("expected output to contain [AUTH]")
	}
	if !strings.Contains(output, "[API]") {
		t.Errorf("expected output to contain [API]")
	}

	// Verify that tree lines are colored
	authStyle := lipgloss.NewStyle().Foreground(authHex).Render("│")

	if !strings.Contains(output, authStyle) {
		t.Errorf("expected output to contain colored vertical line for AUTH scope")
	}
}

func TestStackTreeRenderer_InheritedScopeColoring(t *testing.T) {
	mock := &MockTreeData{
		CurrentVal: "feature-login",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":              {"feature-auth-base"},
			"feature-auth-base": {"feature-login"},
			"feature-login":     {},
		},
		ParentsMap: map[string]string{
			"feature-auth-base": "main",
			"feature-login":     "feature-auth-base",
		},
		FixedMap: map[string]bool{
			"main":              true,
			"feature-auth-base": true,
			"feature-login":     true,
		},
	}

	renderer := NewRenderer(mock)

	// Set scope only on the base branch
	renderer.SetAnnotation("feature-auth-base", BranchAnnotation{Scope: "AUTH", ExplicitScope: "AUTH"})
	// Inherited scope on child, but NO ExplicitScope
	renderer.SetAnnotation("feature-login", BranchAnnotation{Scope: "AUTH"})

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeFull,
	})

	output := strings.Join(lines, "\n")

	// Get expected colors
	authHex, _ := style.GetScopeColor("AUTH")

	// Verify that the scope label is present only for the base branch
	if !strings.Contains(output, "feature-auth-base") || !strings.Contains(output, "[AUTH]") {
		t.Errorf("expected base branch to have [AUTH] label")
	}

	// feature-login should have the [AUTH] label and its symbol and tree lines should be colored
	loginLine := ""
	trailingLine := ""
	for i, line := range lines {
		if strings.Contains(line, "feature-login") {
			loginLine = line
			if i+1 < len(lines) {
				trailingLine = lines[i+1]
			}
			break
		}
	}

	if loginLine == "" {
		t.Fatalf("could not find line for feature-login")
	}

	if !strings.Contains(loginLine, "[AUTH]") {
		t.Errorf("expected inherited branch to have [AUTH] label")
	}

	authStyleSymbol := lipgloss.NewStyle().Foreground(authHex).Render(CurrentBranchSymbol)
	if !strings.Contains(loginLine, authStyleSymbol) {
		t.Errorf("expected inherited branch symbol to be colored with AUTH scope. Line was: %q", loginLine)
	}

	authStyleVertical := lipgloss.NewStyle().Foreground(authHex).Render("│")
	if !strings.Contains(trailingLine, authStyleVertical) {
		t.Errorf("expected trailing line to be colored with AUTH scope. Line was: %q", trailingLine)
	}
}

func TestStackTreeRenderer_BranchingScopeColoring(t *testing.T) {
	mock := &MockTreeData{
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
	}

	renderer := NewRenderer(mock)

	// Set scope on main
	renderer.SetAnnotation("main", BranchAnnotation{Scope: "AUTH", ExplicitScope: "AUTH"})
	renderer.SetAnnotation("feature-1a", BranchAnnotation{Scope: "AUTH"})
	renderer.SetAnnotation("feature-1b", BranchAnnotation{Scope: "AUTH"})

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeFull,
	})

	output := strings.Join(lines, "\n")

	// Get expected color for AUTH
	authHex, _ := style.GetScopeColor("AUTH")
	authStyleBranch := lipgloss.NewStyle().Foreground(authHex).Render("├──┘")

	if !strings.Contains(output, authStyleBranch) {
		t.Errorf("expected output to contain colored branching characters for AUTH scope. Output was:\n%s", output)
	}
}

func TestStackTreeRenderer_ScopeColoringBoundaries(t *testing.T) {
	// Setup a branching structure where one branch has a scope
	// main
	//   └─ base (A)
	//       ├─ scoped-branch (B) [SCOPE-X]
	//       └─ unscoped-branch (C)
	mock := &MockTreeData{
		CurrentVal: "scoped-branch",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main": {"base"},
			"base": {
				"scoped-branch",
				"unscoped-branch",
			},
			"scoped-branch":   {},
			"unscoped-branch": {},
		},
		ParentsMap: map[string]string{
			"base":            "main",
			"scoped-branch":   "base",
			"unscoped-branch": "base",
		},
		FixedMap: map[string]bool{
			"main":            true,
			"base":            true,
			"scoped-branch":   true,
			"unscoped-branch": true,
		},
	}

	renderer := NewRenderer(mock)

	// Only scoped-branch has the scope
	renderer.SetAnnotation("scoped-branch", BranchAnnotation{
		Scope:         "SCOPE-X",
		ExplicitScope: "SCOPE-X",
	})
	renderer.SetAnnotation("base", BranchAnnotation{Scope: ""})
	renderer.SetAnnotation("unscoped-branch", BranchAnnotation{Scope: ""})

	lines := renderer.RenderStack("main", RenderOptions{
		Mode: RenderModeFull,
	})

	// Get colors
	scopeXColor, _ := style.GetScopeColor("SCOPE-X")
	scopeXStyle := lipgloss.NewStyle().Foreground(scopeXColor)
	scopeXLine := scopeXStyle.Render("│")

	// 1. unscoped-branch should NOT have the scope color in its vertical line
	for _, line := range lines {
		if strings.Contains(line, "unscoped-branch") {
			if strings.Contains(line, scopeXLine) {
				t.Errorf("unscoped-branch line should not contain SCOPE-X color. Line: %q", line)
			}
		}
	}

	// 2. The vertical line connecting scoped-branch to its UNSCOPED parent should NOT be colored
	foundScoped := false
	for i, line := range lines {
		if strings.Contains(line, "scoped-branch") {
			foundScoped = true
			if i+1 < len(lines) {
				nextLine := lines[i+1] // Vertical line below scoped-branch
				if strings.Contains(nextLine, scopeXLine) {
					t.Errorf("Vertical line connecting scoped-branch to unscoped parent should not be colored. Line: %q", nextLine)
				}
			}
		}
	}
	if !foundScoped {
		t.Error("scoped-branch not found in output")
	}

	// 3. The symbol for scoped-branch SHOULD be colored
	scopeXSymbol := scopeXStyle.Render(CurrentBranchSymbol)
	foundSymbol := false
	for _, line := range lines {
		if strings.Contains(line, "scoped-branch") {
			if strings.Contains(line, scopeXSymbol) {
				foundSymbol = true
			}
		}
	}
	if !foundSymbol {
		t.Error("scoped-branch symbol should be colored")
	}
}

// Edge case tests

func TestStackTreeRenderer_EmptyTree(t *testing.T) {
	// A tree with just the trunk and no children
	mock := &MockTreeData{
		CurrentVal: "main",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main": {},
		},
		ParentsMap: map[string]string{},
		FixedMap: map[string]bool{
			"main": true,
		},
	}

	renderer := NewRenderer(mock)
	lines := renderer.RenderStack("main", RenderOptions{Mode: RenderModeCompact})

	// Should have exactly 1 line (just main)
	if len(lines) != 1 {
		t.Errorf("expected 1 line for empty tree, got %d: %v", len(lines), lines)
	}

	if !strings.Contains(lines[0], "main") {
		t.Errorf("expected 'main' in output, got: %s", lines[0])
	}
}

func TestStackTreeRenderer_SingleBranch(t *testing.T) {
	// A tree with trunk -> one branch
	mock := &MockTreeData{
		CurrentVal: "feature",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":    {"feature"},
			"feature": {},
		},
		ParentsMap: map[string]string{
			"feature": "main",
		},
		FixedMap: map[string]bool{
			"main":    true,
			"feature": true,
		},
	}

	renderer := NewRenderer(mock)
	lines := renderer.RenderStack("main", RenderOptions{Mode: RenderModeCompact})

	// Should have 2 lines
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}

	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "main") || !strings.Contains(output, "feature") {
		t.Errorf("expected both main and feature in output, got: %s", output)
	}
}

func TestStackTreeRenderer_DeeplyNested(t *testing.T) {
	// A deeply nested linear tree with 15 levels
	depth := 15
	childrenMap := make(map[string][]string)
	parentsMap := make(map[string]string)
	fixedMap := make(map[string]bool)

	branches := make([]string, depth)
	branches[0] = "main"
	for i := 1; i < depth; i++ {
		branches[i] = strings.Repeat("f", i) // "f", "ff", "fff", etc.
	}

	fixedMap["main"] = true
	for i := 1; i < depth; i++ {
		branch := branches[i]
		parent := branches[i-1]
		parentsMap[branch] = parent
		childrenMap[parent] = []string{branch}
		fixedMap[branch] = true
	}
	childrenMap[branches[depth-1]] = []string{}

	mock := &MockTreeData{
		CurrentVal:  branches[depth-1], // Deepest branch
		TrunkVal:    "main",
		ChildrenMap: childrenMap,
		ParentsMap:  parentsMap,
		FixedMap:    fixedMap,
	}

	renderer := NewRenderer(mock)
	lines := renderer.RenderStack("main", RenderOptions{Mode: RenderModeCompact})

	// Should have all 15 branches
	if len(lines) != depth {
		t.Errorf("expected %d lines, got %d: %v", depth, len(lines), lines)
	}

	// The deepest branch should have significant indentation
	output := strings.Join(lines, "\n")
	if !strings.Contains(output, branches[depth-1]) {
		t.Errorf("expected deepest branch %q in output", branches[depth-1])
	}
}

func TestStackTreeRenderer_CollapsedBranch(t *testing.T) {
	mock := &MockTreeData{
		CurrentVal: "feature-1",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":      {"feature-1"},
			"feature-1": {"feature-2", "feature-3"},
			"feature-2": {},
			"feature-3": {},
		},
		ParentsMap: map[string]string{
			"feature-1": "main",
			"feature-2": "feature-1",
			"feature-3": "feature-1",
		},
		FixedMap: map[string]bool{
			"main":      true,
			"feature-1": true,
			"feature-2": true,
			"feature-3": true,
		},
	}

	renderer := NewRenderer(mock)

	// Without collapse
	linesExpanded := renderer.RenderStack("main", RenderOptions{Mode: RenderModeCompact})

	// With collapse on feature-1
	linesCollapsed := renderer.RenderStack("main", RenderOptions{
		Mode:      RenderModeCompact,
		Collapsed: map[string]bool{"feature-1": true},
	})

	// Collapsed should have fewer lines (children not shown)
	if len(linesCollapsed) >= len(linesExpanded) {
		t.Errorf("collapsed tree should have fewer lines: expanded=%d, collapsed=%d",
			len(linesExpanded), len(linesCollapsed))
	}

	// Collapsed output should still have main and feature-1
	collapsedOutput := strings.Join(linesCollapsed, "\n")
	if !strings.Contains(collapsedOutput, "main") {
		t.Error("collapsed output should contain main")
	}
	if !strings.Contains(collapsedOutput, "feature-1") {
		t.Error("collapsed output should contain feature-1")
	}

	// Collapsed output should NOT have feature-2 or feature-3
	if strings.Contains(collapsedOutput, "feature-2") {
		t.Error("collapsed output should not contain feature-2")
	}
	if strings.Contains(collapsedOutput, "feature-3") {
		t.Error("collapsed output should not contain feature-3")
	}
}

func TestStackTreeRenderer_WideBranching(t *testing.T) {
	// A tree where main has 5 direct children
	mock := &MockTreeData{
		CurrentVal: "branch-1",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":     {"branch-1", "branch-2", "branch-3", "branch-4", "branch-5"},
			"branch-1": {},
			"branch-2": {},
			"branch-3": {},
			"branch-4": {},
			"branch-5": {},
		},
		ParentsMap: map[string]string{
			"branch-1": "main",
			"branch-2": "main",
			"branch-3": "main",
			"branch-4": "main",
			"branch-5": "main",
		},
		FixedMap: map[string]bool{
			"main":     true,
			"branch-1": true,
			"branch-2": true,
			"branch-3": true,
			"branch-4": true,
			"branch-5": true,
		},
	}

	renderer := NewRenderer(mock)
	lines := renderer.RenderStack("main", RenderOptions{Mode: RenderModeCompact})

	// Should have 6 lines (main + 5 children)
	if len(lines) != 6 {
		t.Errorf("expected 6 lines, got %d: %v", len(lines), lines)
	}

	output := strings.Join(lines, "\n")
	// Should contain branching characters for 5 children
	if !strings.Contains(output, "─") {
		t.Error("expected branching characters in output")
	}

	// All branches should be present
	for i := 1; i <= 5; i++ {
		branchName := "branch-" + string(rune('0'+i))
		if !strings.Contains(output, branchName) {
			t.Errorf("expected %s in output", branchName)
		}
	}
}

func TestStackTreeRenderer_RenderFromMiddleBranch(t *testing.T) {
	// Test rendering from a branch in the middle of the stack
	mock := &MockTreeData{
		CurrentVal: "feature-2",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":      {"feature-1"},
			"feature-1": {"feature-2"},
			"feature-2": {"feature-3"},
			"feature-3": {},
		},
		ParentsMap: map[string]string{
			"feature-1": "main",
			"feature-2": "feature-1",
			"feature-3": "feature-2",
		},
		FixedMap: map[string]bool{
			"main":      true,
			"feature-1": true,
			"feature-2": true,
			"feature-3": true,
		},
	}

	renderer := NewRenderer(mock)

	// Render from feature-2 (middle of stack)
	lines := renderer.RenderStack("feature-2", RenderOptions{Mode: RenderModeCompact})

	output := strings.Join(lines, "\n")

	// Should include upstack (feature-3)
	if !strings.Contains(output, "feature-3") {
		t.Error("expected feature-3 in upstack")
	}

	// Should include downstack (main, feature-1)
	if !strings.Contains(output, "main") {
		t.Error("expected main in downstack")
	}
	if !strings.Contains(output, "feature-1") {
		t.Error("expected feature-1 in downstack")
	}
}

func TestStackTreeRenderer_CacheIsCleared(t *testing.T) {
	// Test that cache is properly cleared between renders
	mock := &MockTreeData{
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
			"feature-1": true,
		},
	}

	renderer := NewRenderer(mock)

	// First render
	lines1 := renderer.RenderStack("main", RenderOptions{Mode: RenderModeCompact})

	// Modify the tree (add a new child)
	mock.ChildrenMap["feature-1"] = []string{"feature-2"}
	mock.ChildrenMap["feature-2"] = []string{}
	mock.ParentsMap["feature-2"] = "feature-1"
	mock.FixedMap["feature-2"] = true

	// Second render should include the new branch
	lines2 := renderer.RenderStack("main", RenderOptions{Mode: RenderModeCompact})

	if len(lines2) <= len(lines1) {
		t.Errorf("second render should have more lines after adding branch: got %d, want > %d",
			len(lines2), len(lines1))
	}

	output := strings.Join(lines2, "\n")
	if !strings.Contains(output, "feature-2") {
		t.Error("expected feature-2 in second render")
	}
}

func TestStackTreeRenderer_MergedDownstackDisplay(t *testing.T) {
	t.Parallel()
	mock := &MockTreeData{
		CurrentVal: "branch2",
		TrunkVal:   "main",
		ChildrenMap: map[string][]string{
			"main":    {"branch2"},
			"branch2": {},
		},
		ParentsMap: map[string]string{
			"branch2": "main",
		},
		FixedMap: map[string]bool{
			"main":    true,
			"branch2": true,
		},
	}
	renderer := NewRenderer(mock)

	prNum := 123
	renderer.SetAnnotation("branch2", BranchAnnotation{
		MergedDownstack: []MergedParentDisplay{
			{BranchName: "branch1", PRNumber: &prNum, PRState: "MERGED"},
		},
	})

	lines := renderer.RenderStack("branch2", RenderOptions{Mode: RenderModeFull})
	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "previously based on") {
		t.Errorf("expected 'previously based on' in merged downstack output, got: %s", output)
	}
}
