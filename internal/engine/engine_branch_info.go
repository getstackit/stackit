package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// GetCommitDate returns the commit date for a branch
func (e *engineImpl) GetCommitDate(branch Branch) (time.Time, error) {
	branchName := branch.GetName()
	info, err := e.git.ReadCommitInfo(context.Background(), "refs/heads/"+branchName).One()
	return info.Date, err
}

// GetCommitAuthor returns the commit author for a branch
func (e *engineImpl) GetCommitAuthor(branch Branch) (string, error) {
	branchName := branch.GetName()
	info, err := e.git.ReadCommitInfo(context.Background(), "refs/heads/"+branchName).One()
	return info.Author, err
}

// BatchCommitInfo returns each branch's tip commit date and author, keyed by
// branch name, resolved in one batched pass.
func (e *engineImpl) BatchCommitInfo(branches Branches) map[string]git.CommitInfo {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = "refs/heads/" + b.GetName()
	}
	data := e.git.ReadCommitInfo(context.Background(), names...)
	result := make(map[string]git.CommitInfo, len(data.Values))
	for ref, info := range data.Values {
		result[strings.TrimPrefix(ref, "refs/heads/")] = info
	}
	return result
}

// GetRevision returns the SHA of a branch
func (e *engineImpl) GetRevision(branch Branch) (string, error) {
	branchName := branch.GetName()
	return e.git.ReadRevisions(context.Background(), branchName).One()
}

// GetRevisionForName returns the SHA of a branch by name
func (e *engineImpl) GetRevisionForName(branchName string) (string, error) {
	return e.git.ReadRevisions(context.Background(), branchName).One()
}

// GetRevisions returns the SHAs for multiple branches.
func (e *engineImpl) GetRevisions(branchNames []string) (RevisionMap, []error) {
	return e.git.ReadRevisions(context.Background(), branchNames...).ValuesAndErrors()
}

// BatchDivergencePoints returns each branch's divergence point keyed by branch
// name, matching GetDivergencePoint (the stored parent revision when present,
// else the parent's current tip) but resolving the whole set in one batched pass.
func (e *engineImpl) BatchDivergencePoints(branches Branches) RevisionMap {
	return RevisionMap(batchByBranch(e, branches, func(b Branch, head, parentRev, storedBase string) string {
		return statBase(parentRev, storedBase)
	}))
}

// CommitCountBetween returns how many commits are in (base, head]. It is the
// exported entry point to the cached counter for callers that have two plain
// revisions rather than a branch set — e.g. reporting how far trunk moved
// during a sync.
func (e *engineImpl) CommitCountBetween(base, head string) (int, error) {
	return e.commitCountBetween(git.RevRange{Base: base, Head: head})
}

// commitCountBetween returns the commit count in (base, head], using the
// (base, head)-keyed cache. It takes pre-resolved revisions so batched callers
// need not re-resolve a branch's head.
func (e *engineImpl) commitCountBetween(rr git.RevRange) (int, error) {
	base, head := rr.Base, rr.Head
	if head == base {
		return 0, nil
	}

	cacheKey := base + ":" + head
	if v, ok := e.commitCountCache.Load(cacheKey); ok {
		return v.(int), nil
	}

	count, err := e.git.ReadCommitCounts(context.Background(), rr).One()
	if err != nil {
		return 0, err
	}
	e.commitCountCache.Store(cacheKey, count)
	return count, nil
}

// GetRecentTrunkCommits returns the most recent commits on the trunk branch,
// including any stack trailer metadata embedded in consolidation merge commits.
func (e *engineImpl) GetRecentTrunkCommits(count int) ([]git.RecentCommit, error) {
	return e.git.GetRecentCommits(context.Background(), e.trunk, count)
}

// GetTrunkCommitsInRange returns the commits in the revision range from..to with
// stack trailer metadata. An empty `to` defaults to the trunk branch tip. Use it
// to build a changelog over a tag range (e.g. from "v1.4.0" to trunk).
func (e *engineImpl) GetTrunkCommitsInRange(rr git.RevRange) ([]git.RecentCommit, error) {
	if rr.Head == "" {
		rr.Head = e.trunk
	}
	return e.git.GetRecentCommitsInRange(context.Background(), rr.String())
}

// GetAllCommits returns commits for a branch in various formats
func (e *engineImpl) GetAllCommits(branch Branch, format CommitFormat) ([]string, error) {
	mode := git.CommitDetails
	if format == CommitFormatSHA {
		mode = git.CommitIDs
	}
	history, err := e.ReadBranchCommits(context.Background(), mode, BranchesOf(branch)).One()
	if err != nil {
		return nil, err
	}
	return FormatCommits(history.Commits, format)
}

// FormatCommits formats already-read commit data without repository I/O.
func FormatCommits(commits []git.CommitMetadata, format CommitFormat) ([]string, error) {
	var result []string
	for _, commit := range commits {
		var line string
		switch format {
		case CommitFormatSHA:
			line = commit.SHA
		case CommitFormatSubject:
			line = commit.Subject
		case CommitFormatSHASubject:
			line = commit.SHA + "\x00" + commit.Subject
		case CommitFormatReadable:
			line = commit.ShortSHA + " " + commit.Subject
		case CommitFormatMessage:
			line = commit.Message
		case CommitFormatReadableWithDate:
			date, err := time.Parse(time.RFC3339, commit.AuthorDate)
			if err != nil {
				return nil, err
			}
			line = commit.ShortSHA + "\t" + date.UTC().Format(time.RFC3339) + "\t" + commit.Subject
		default:
			return nil, fmt.Errorf("unknown commit format: %s", format)
		}
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}
