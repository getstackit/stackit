package move

import (
	"context"
	"errors"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Selection contains precomputed data for validating potential move targets.
type Selection struct {
	eng          Engine
	out          output.Output
	source       string
	sourceBranch engine.Branch
	descendants  engine.Branches
	oldParent    *engine.Branch
	oldParentRev string
}

// PrepareSelection builds the data needed for interactive onto validation.
func PrepareSelection(ctx context.Context, eng Engine, out output.Output, source string) (*Selection, error) {
	if source == "" {
		return nil, errors.New("source branch is required")
	}

	sourceBranch := eng.GetBranch(source)

	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	descendants := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	oldParent := sourceBranch.Parent()

	// Match move.Action behavior: capture divergence point, allow empty fallback.
	oldParentRev, _ := eng.GetDivergencePoint(ctx, source)

	return &Selection{
		eng:          eng,
		out:          out,
		source:       source,
		sourceBranch: sourceBranch,
		descendants:  descendants,
		oldParent:    oldParent,
		oldParentRev: oldParentRev,
	}, nil
}

// Descendants returns the descendant branches (including the source).
func (s *Selection) Descendants() engine.Branches {
	return s.descendants
}

// OldParent returns the current parent branch (nil if trunk).
func (s *Selection) OldParent() *engine.Branch {
	return s.oldParent
}

// OldParentRev returns the captured old parent revision.
func (s *Selection) OldParentRev() string {
	return s.oldParentRev
}

// ValidateOnto validates a potential move target and returns validation, commits, and rebase specs.
func (s *Selection) ValidateOnto(ctx context.Context, onto string) (*engine.RebaseValidation, []string, []engine.RebaseSpec, error) {
	rebaseSpecs := BuildRebaseSpecs(ctx, s.eng, s.out, s.source, onto, s.oldParent, s.oldParentRev, s.descendants)

	validation, err := s.eng.ValidateRebases(ctx, rebaseSpecs, engine.ValidateRebasesOpts{})
	if err != nil {
		return nil, nil, rebaseSpecs, err
	}

	commitsData, _ := s.eng.GetAllCommits(ctx, s.sourceBranch)
	commits := commitsData.Subjects()
	return validation, commits, rebaseSpecs, nil
}
