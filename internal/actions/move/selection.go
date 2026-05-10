package move

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Selection contains precomputed data for validating potential move targets.
type Selection struct {
	eng          engine.Engine
	out          output.Output
	source       string
	sourceBranch engine.Branch
	descendants  []engine.Branch
	oldParent    *engine.Branch
	oldParentRev string
}

// PrepareSelection builds the data needed for interactive onto validation.
func PrepareSelection(ctx *app.Context, source string) (*Selection, error) {
	if source == "" {
		return nil, fmt.Errorf("source branch is required")
	}

	eng := ctx.Engine
	sourceBranch := eng.GetBranch(source)

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	descendants := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	oldParent := sourceBranch.GetParent()

	// Match move.Action behavior: capture divergence point, allow empty fallback.
	oldParentRev, _ := eng.GetDivergencePoint(source)

	return &Selection{
		eng:          eng,
		out:          ctx.Output,
		source:       source,
		sourceBranch: sourceBranch,
		descendants:  descendants,
		oldParent:    oldParent,
		oldParentRev: oldParentRev,
	}, nil
}

// Descendants returns the descendant branches (including the source).
func (s *Selection) Descendants() []engine.Branch {
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
	rebaseSpecs := BuildRebaseSpecs(s.eng, s.out, s.source, onto, s.oldParent, s.oldParentRev, s.descendants)

	validation, err := s.eng.ValidateRebases(ctx, rebaseSpecs)
	if err != nil {
		return nil, nil, rebaseSpecs, err
	}

	commits, _ := s.eng.GetAllCommits(s.sourceBranch, engine.CommitFormatSubject)
	return validation, commits, rebaseSpecs, nil
}
