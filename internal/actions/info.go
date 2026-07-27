package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// BranchInfoPR represents pull request information for a branch.
type BranchInfoPR struct {
	Number  *int
	Title   string
	State   git.PRState
	IsDraft bool
	URL     string
	Body    string
}

// BranchDiffStats represents summary diff information for a branch. It is pure
// data; the JSON shape lives in the CLI adapter that renders it.
type BranchDiffStats struct {
	FilesChanged int
	Additions    int
	Deletions    int
}

// BranchInfoResult contains structured data for `stackit info` on a single branch.
type BranchInfoResult struct {
	Name             string
	IsCurrent        bool
	IsTrunk          bool
	IsLocked         bool
	IsFrozen         bool
	NeedsRestack     bool
	Scope            string
	CommitDate       string
	Parent           string
	Children         []string
	PR               *BranchInfoPR
	CommitMessages   []string
	DiffStats        BranchDiffStats
	StackTitle       string
	StackDescription string
	PatchOutput      string
	DiffOutput       string
}

// BranchInfoQueryOptions contains options for querying single-branch info.
type BranchInfoQueryOptions struct {
	BranchName string
	Diff       bool
	Patch      bool
	Stat       bool
}

type branchInfoDebugLogger interface {
	Debug(format string, args ...any)
}

// QueryBranchInfo gathers structured information for `stackit info`.
func QueryBranchInfo(ctx context.Context, eng engine.Engine, opts BranchInfoQueryOptions, debug branchInfoDebugLogger) (BranchInfoResult, error) {
	branchName, err := ResolveBranchName(eng, opts.BranchName)
	if err != nil {
		return BranchInfoResult{}, err
	}

	branch := eng.GetBranch(branchName)
	if !branch.IsTracked() && !branch.IsTrunk() {
		if _, err := eng.GetRevision(branch); err != nil {
			return BranchInfoResult{}, fmt.Errorf("branch %s does not exist", branchName)
		}
		refreshRemoteMetadataForBranchInfo(ctx, eng, branchName, debug)
	}

	return buildBranchInfoResult(ctx, eng, branchName, branch, opts)
}

func refreshRemoteMetadataForBranchInfo(ctx context.Context, eng engine.Engine, branchName string, debug branchInfoDebugLogger) {
	if err := eng.FetchRemoteMetadata(ctx); err != nil {
		debugBranchInfof(debug, "Failed to fetch remote metadata: %v", err)
		return
	}

	if err := eng.ApplyRemoteMetadataForBranches(ctx, []string{branchName}); err != nil {
		debugBranchInfof(debug, "Failed to apply remote metadata for %s: %v", branchName, err)
	}
}

func buildBranchInfoResult(ctx context.Context, eng engine.Engine, branchName string, branch engine.Branch, opts BranchInfoQueryOptions) (BranchInfoResult, error) {
	currentBranch := eng.CurrentBranch()
	isCurrent := currentBranch != nil && branchName == currentBranch.GetName()
	isTrunk := branch.IsTrunk()

	result := BranchInfoResult{
		Name:           branchName,
		IsCurrent:      isCurrent,
		IsTrunk:        isTrunk,
		IsLocked:       branch.IsLocked(),
		IsFrozen:       branch.IsFrozen(),
		NeedsRestack:   !isTrunk && !branch.IsBranchUpToDate(),
		Scope:          branch.GetScope().String(),
		CommitMessages: []string{},
		Children:       []string{},
	}

	commitDate, err := branch.GetCommitDate()
	if err == nil {
		result.CommitDate = commitDate.Format(time.RFC3339)
	}

	if parent := branch.GetParent(); parent != nil {
		result.Parent = parent.GetName()
	}

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	for _, child := range graph.ChildBranches(branch) {
		result.Children = append(result.Children, child.GetName())
	}

	if !isTrunk {
		prInfo, _ := branch.GetPrInfo()
		if prInfo != nil {
			result.PR = &BranchInfoPR{
				Number:  prInfo.Number(),
				Title:   prInfo.Title(),
				State:   prInfo.State(),
				IsDraft: prInfo.IsDraft(),
				URL:     prInfo.URL(),
				Body:    prInfo.Body(),
			}
		}
	}

	commits, err := branch.GetAllCommits(engine.CommitFormatReadable)
	if err == nil {
		result.CommitMessages = commits
	}

	// Diff stats — resolved via the batched reader over a single-branch set,
	// rather than a per-branch accessor.
	diff := eng.BatchDiffStats(engine.BranchesOf(branch))[branchName]
	result.DiffStats.Additions = diff.Added
	result.DiffStats.Deletions = diff.Deleted

	// Files changed — measured against the branch's divergence point, the same
	// base BatchDiffStats uses above, so the file count stays consistent with the
	// additions/deletions when the parent has advanced since the branch diverged.
	if !isTrunk {
		base, err := eng.GetDivergencePoint(branchName)
		if err == nil && base != "" {
			branchRev, err := branch.GetRevision()
			if err == nil {
				files, err := eng.GetChangedFiles(ctx, git.RevRange{Base: base, Head: branchRev})
				if err == nil {
					result.DiffStats.FilesChanged = len(files)
				}
			}
		}
	}

	stackDesc := eng.GetStackDescription(branch)
	if stackDesc != nil && !stackDesc.IsEmpty() {
		result.StackTitle = stackDesc.Title
		result.StackDescription = stackDesc.Description
	}

	effectiveDiff := opts.Diff || (opts.Stat && !opts.Patch)
	effectivePatch := opts.Patch && !opts.Diff

	if effectivePatch {
		baseRevision := ""
		if isTrunk {
			baseRevision = branchName + "~"
		} else {
			commits, err := branch.GetAllCommits(engine.CommitFormatSHA)
			if err == nil && len(commits) > 0 {
				oldestSHA := commits[0]
				baseRevision, _ = eng.GetParentCommitSHA(oldestSHA)
			}
		}
		branchRevision, err := branch.GetRevision()
		if err == nil {
			patchOutput, err := eng.ShowCommits(ctx, git.RevRange{Base: baseRevision, Head: branchRevision}, true, opts.Stat)
			if err == nil {
				result.PatchOutput = patchOutput
			}
		}
	}

	if effectiveDiff {
		if isTrunk {
			headRevision, err := branch.GetRevision()
			if err == nil {
				parentSHA, err := eng.GetCommitSHA(branchName, 1)
				if err == nil {
					diffOutput, err := eng.ShowDiff(ctx, parentSHA, headRevision, opts.Stat)
					if err == nil {
						result.DiffOutput = diffOutput
					}
				}
			}
		} else {
			commits, err := branch.GetAllCommits(engine.CommitFormatSHA)
			if err == nil && len(commits) > 0 {
				oldestSHA := commits[0]
				parentSHA, _ := eng.GetParentCommitSHA(oldestSHA)
				branchRevision, err := branch.GetRevision()
				if err == nil {
					diffOutput, err := eng.ShowDiff(ctx, parentSHA, branchRevision, opts.Stat)
					if err == nil {
						result.DiffOutput = diffOutput
					}
				}
			}
		}
	}

	return result, nil
}

func debugBranchInfof(debug branchInfoDebugLogger, format string, args ...any) {
	if debug == nil {
		return
	}
	debug.Debug(format, args...)
}
