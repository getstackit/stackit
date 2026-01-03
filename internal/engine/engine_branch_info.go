package engine

import (
	"fmt"
	"strings"
	"time"
)

// GetCommitDate returns the commit date for a branch
func (e *engineImpl) GetCommitDate(branch Branch) (time.Time, error) {
	branchName := branch.GetName()
	return e.git.GetCommitDate(branchName)
}

// GetCommitAuthor returns the commit author for a branch
func (e *engineImpl) GetCommitAuthor(branch Branch) (string, error) {
	branchName := branch.GetName()
	return e.git.GetCommitAuthor(branchName)
}

// GetRevision returns the SHA of a branch
func (e *engineImpl) GetRevision(branch Branch) (string, error) {
	branchName := branch.GetName()
	return e.git.GetRevision(branchName)
}

// GetRevisionForName returns the SHA of a branch by name
func (e *engineImpl) GetRevisionForName(branchName string) (string, error) {
	return e.git.GetRevision(branchName)
}

// BatchGetRevisions returns the SHAs for multiple branches
func (e *engineImpl) BatchGetRevisions(branchNames []string) (map[string]string, []error) {
	return e.git.BatchGetRevisions(branchNames)
}

// GetCommitCount returns the number of commits for a branch
func (e *engineImpl) GetCommitCount(branch Branch) (int, error) {
	branchName := branch.GetName()
	e.mu.RLock()
	trunk := e.trunk
	parent, ok := e.parentMap[branchName]
	e.mu.RUnlock()

	if !ok {
		parent = trunk
	}

	// Get base revision (stored parent revision)
	meta, err := e.git.ReadMetadata(branchName)
	var base string
	if err == nil && meta.ParentBranchRevision != nil {
		base = *meta.ParentBranchRevision
	} else {
		// Fallback to current parent branch tip if metadata is missing
		baseRev, err := e.git.GetRevision(parent)
		if err != nil {
			return 0, err
		}
		base = baseRev
	}

	branchRev, err := e.git.GetRevision(branchName)
	if err != nil {
		return 0, err
	}

	// If revisions are same, count is 0
	if branchRev == base {
		return 0, nil
	}

	// For real git, we'd use a git helper. I'll use git.GetCommitRange count.
	commits, err := e.GetAllCommits(branch, CommitFormatSHA)
	if err != nil {
		return 0, err
	}
	return len(commits), nil
}

// GetDiffStats returns diff stats for a branch
func (e *engineImpl) GetDiffStats(branch Branch) (int, int, error) {
	branchName := branch.GetName()
	e.mu.RLock()
	trunk := e.trunk
	parent, ok := e.parentMap[branchName]
	e.mu.RUnlock()

	if !ok {
		parent = trunk
	}

	// Get base revision (stored parent revision)
	meta, err := e.git.ReadMetadata(branchName)
	var base string
	if err == nil && meta.ParentBranchRevision != nil {
		base = *meta.ParentBranchRevision
	} else {
		baseRev, err := e.git.GetRevision(parent)
		if err != nil {
			return 0, 0, err
		}
		base = baseRev
	}

	branchRev, err := e.git.GetRevision(branchName)
	if err != nil {
		return 0, 0, err
	}

	// If revisions are same, stats are 0
	if branchRev == base {
		return 0, 0, nil
	}

	// Use git diff --numstat
	output, err := e.git.GetDiffNumstat(base, branchRev)
	if err != nil {
		return 0, 0, err
	}

	added, deleted := 0, 0
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			var a, d int
			_, _ = fmt.Sscanf(fields[0], "%d", &a)
			_, _ = fmt.Sscanf(fields[1], "%d", &d)
			added += a
			deleted += d
		}
	}

	return added, deleted, nil
}

// GetAllCommits returns commits for a branch in various formats
func (e *engineImpl) GetAllCommits(branch Branch, format CommitFormat) ([]string, error) {
	branchName := branch.GetName()
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check if branch is trunk
	if branchName == e.trunk {
		// Trunk is the base, so it has no commits "on" it relative to a parent
		return []string{}, nil
	}

	// Get metadata to find parent revision
	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		return nil, err
	}

	// Get branch revision
	branchRevision, err := e.git.GetRevision(branchName)
	if err != nil {
		return nil, err
	}

	// Get parent revision (base)
	var baseRevision string
	if meta.ParentBranchRevision != nil {
		baseRevision = *meta.ParentBranchRevision
	}

	// Get SHAs first
	shas, err := e.git.GetCommitRangeSHAs(baseRevision, branchRevision)
	if err != nil {
		return nil, err
	}

	if format == CommitFormatSHA {
		return shas, nil
	}

	// Format results
	result := make([]string, len(shas))
	for i, sha := range shas {
		var formatted string
		switch format {
		case CommitFormatSubject:
			formatted, _ = e.git.GetCommitLog(sha, "%s")
		case CommitFormatMessage:
			formatted, _ = e.git.GetCommitLog(sha, "%B")
		case CommitFormatReadable:
			formatted, _ = e.git.GetCommitLog(sha, "%h %s")
		default:
			return nil, fmt.Errorf("unknown commit format: %s", format)
		}
		result[i] = strings.TrimSpace(formatted)
	}

	return result, nil
}
