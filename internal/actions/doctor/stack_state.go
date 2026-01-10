package doctor

import (
	"context"
	"fmt"
	"strings"

	"stackit.dev/stackit/internal/engine"
)

// checkStackState performs stack state and metadata integrity checks
func checkStackState(ctx context.Context, eng engine.Engine, handler Handler, warnings int, errors int, fix bool) (int, int) {
	// Get all branches
	allBranches, err := eng.Git().GetAllBranchNames()
	if err != nil {
		errors++
		handler.OnCheck("branch_list", CheckError, fmt.Sprintf("failed to get branch names: %v", err))
		return warnings, errors
	}

	// Get all metadata refs
	metadataRefs, err := eng.Git().ListMetadata()
	if err != nil {
		errors++
		handler.OnCheck("metadata_list", CheckError, fmt.Sprintf("failed to get metadata refs: %v", err))
		return warnings, errors
	}

	// Check for orphaned metadata (metadata for branches that don't exist)
	branchSet := make(map[string]bool)
	for _, branch := range allBranches {
		branchSet[branch] = true
	}

	orphanedCount := 0
	prunedCount := 0
	for branchName := range metadataRefs {
		if !branchSet[branchName] {
			orphanedCount++
			if fix {
				if err := eng.Git().DeleteMetadata(branchName); err != nil {
					warnings++
					handler.OnCheck("orphaned_metadata", CheckWarning, fmt.Sprintf("orphaned metadata for '%s' (fix failed: %v)", branchName, err))
				} else {
					prunedCount++
					handler.OnCheck("orphaned_metadata", CheckPassed, fmt.Sprintf("Pruned orphaned metadata for deleted branch %s", branchName))
				}
			} else {
				warnings++
			}
		}
	}

	if orphanedCount > 0 {
		if fix {
			if prunedCount == orphanedCount {
				handler.OnCheck("orphaned_metadata", CheckPassed, fmt.Sprintf("All %d orphaned metadata ref(s) pruned", prunedCount))
			} else {
				handler.OnCheck("orphaned_metadata", CheckWarning, fmt.Sprintf("Found %d orphaned metadata ref(s), pruned %d", orphanedCount, prunedCount))
			}
		} else {
			handler.OnCheck("orphaned_metadata", CheckWarning, fmt.Sprintf("Found %d orphaned metadata ref(s) (run 'stackit doctor --fix' to prune)", orphanedCount))
		}
	} else {
		handler.OnCheck("orphaned_metadata", CheckPassed, "No orphaned metadata found")
	}

	// Check for corrupted metadata
	metadataRefNames := make([]string, 0, len(metadataRefs))
	for branchName := range metadataRefs {
		metadataRefNames = append(metadataRefNames, branchName)
	}
	allMeta, allMetaErrs := eng.Git().BatchReadMetadata(metadataRefNames)

	corruptedCount := 0
	for _, branchName := range metadataRefNames {
		if err := allMetaErrs[branchName]; err != nil {
			corruptedCount++
			errors++
		} else if meta := allMeta[branchName]; meta != nil {
			// Validate that if parent is set, it's not empty
			if meta.ParentBranchName != nil && *meta.ParentBranchName == "" {
				corruptedCount++
				errors++
			}
		}
	}

	if corruptedCount > 0 {
		handler.OnCheck("metadata_integrity", CheckError, fmt.Sprintf("Found %d corrupted metadata ref(s)", corruptedCount))
	} else {
		handler.OnCheck("metadata_integrity", CheckPassed, "Metadata integrity check passed")
	}

	// Check for cycles in the stack graph
	cycles := detectCycles(eng)
	if len(cycles) > 0 {
		for range cycles {
			errors++
		}
		handler.OnCheck("cycles", CheckError, fmt.Sprintf("Found %d cycle(s) in stack graph: %s", len(cycles), strings.Join(cycles[0], " -> ")))
	} else {
		handler.OnCheck("cycles", CheckPassed, "No cycles detected in stack graph")
	}

	// Check for missing parent branches
	missingParents := checkMissingParents(eng, allBranches)
	if len(missingParents) > 0 {
		warnings += len(missingParents)
		handler.OnCheck("missing_parents", CheckWarning, fmt.Sprintf("Found %d branch(es) with missing parents", len(missingParents)))
	} else {
		handler.OnCheck("missing_parents", CheckPassed, "All parent branches exist")
	}

	// Check for empty branches (branches with no commits vs their parent)
	emptyBranches := checkEmptyBranches(ctx, eng)
	if len(emptyBranches) > 0 {
		warnings += len(emptyBranches)
		branchList := strings.Join(emptyBranches, ", ")
		handler.OnCheck("empty_branches", CheckWarning, fmt.Sprintf("Found %d empty branch(es): %s", len(emptyBranches), branchList))
	} else {
		handler.OnCheck("empty_branches", CheckPassed, "No empty branches found")
	}

	return warnings, errors
}

// checkEmptyBranches finds branches that have no commits compared to their parent
func checkEmptyBranches(ctx context.Context, eng engine.Engine) []string {
	var emptyBranches []string
	trunk := eng.Trunk()
	trunkName := trunk.GetName()

	for _, branch := range eng.AllBranches() {
		branchName := branch.GetName()
		if branchName == trunkName {
			continue
		}

		isEmpty, err := eng.IsBranchEmpty(ctx, branchName)
		if err != nil {
			// Skip branches we can't check
			continue
		}
		if isEmpty {
			emptyBranches = append(emptyBranches, branchName)
		}
	}

	return emptyBranches
}

// detectCycles detects cycles in the branch parent graph using DFS
func detectCycles(eng engine.Engine) [][]string {
	var cycles [][]string
	allBranches := eng.AllBranches()
	branchNames := make([]string, len(allBranches))
	for i, b := range allBranches {
		branchNames[i] = b.GetName()
	}
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	parentMap := make(map[string]string)
	trunk := eng.Trunk()
	trunkName := trunk.GetName()

	// Build parent map
	for _, branch := range allBranches {
		branchName := branch.GetName()
		parent := branch.GetParent()
		if parent != nil && parent.GetName() != trunkName {
			parentMap[branchName] = parent.GetName()
		}
	}

	var dfs func(string, []string)
	dfs = func(branch string, path []string) {
		if recStack[branch] {
			// Found a cycle - find where the cycle starts in the path
			cycleStart := -1
			for i, b := range path {
				if b == branch {
					cycleStart = i
					break
				}
			}
			if cycleStart >= 0 {
				// Extract the cycle: from first occurrence to current
				cycle := make([]string, len(path)-cycleStart+1)
				copy(cycle, path[cycleStart:])
				cycle[len(cycle)-1] = branch
				cycles = append(cycles, cycle)
			}
			return
		}

		if visited[branch] {
			// Already fully explored this branch
			return
		}

		visited[branch] = true
		recStack[branch] = true

		// Follow parent if it exists
		if parent, hasParent := parentMap[branch]; hasParent {
			dfs(parent, append(path, branch))
		}

		recStack[branch] = false
	}

	// Run DFS on all branches
	for _, branchName := range branchNames {
		if branchName != trunkName && !visited[branchName] {
			dfs(branchName, []string{})
		}
	}

	return cycles
}

// checkMissingParents checks for branches whose parent branches don't exist
func checkMissingParents(eng engine.Engine, allBranches []string) []string {
	var missing []string
	branchSet := make(map[string]bool)
	trunk := eng.Trunk()
	trunkName := trunk.GetName()

	for _, branch := range allBranches {
		branchSet[branch] = true
	}

	for _, branch := range allBranches {
		if branch == trunkName {
			continue
		}
		branchObj := eng.GetBranch(branch)
		parent := branchObj.GetParent()
		if parent != nil && parent.GetName() != trunkName {
			if !branchSet[parent.GetName()] {
				missing = append(missing, branch)
			}
		}
	}

	return missing
}
