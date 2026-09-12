package engine

import (
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
)

// CommitBatch separates successful reads (including empty branches) from errors.
// Display-only callers may use Commits; operations that require complete history
// should check ForBranch for every branch they consume.
type CommitBatch struct {
	Commits map[string][]string
	Errors  map[string]error
}

// ForBranch looks up an already-read result without performing any Git work.
// A branch absent from the batch is an error, not an empty history.
func (r CommitBatch) ForBranch(branch Branch) ([]string, error) {
	name := branch.GetName()
	if err := r.Errors[name]; err != nil {
		return nil, err
	}
	commits, ok := r.Commits[name]
	if !ok {
		return nil, fmt.Errorf("commits for branch %s were not read", name)
	}
	return commits, nil
}

// BatchCommits resolves metadata and revisions in bulk, then reads each range
// on a bounded worker pool. Results are newest first. Missing metadata, revisions,
// or failed history reads are reported per branch without discarding successes.
func (e *engineImpl) BatchCommits(branches Branches, format CommitFormat) CommitBatch {
	result := CommitBatch{Commits: make(map[string][]string), Errors: make(map[string]error)}
	names := make([]string, 0, len(branches))
	for _, branch := range branches {
		if e.IsTrunk(branch) {
			result.Commits[branch.GetName()] = []string{}
		} else {
			names = append(names, branch.GetName())
		}
	}
	if len(names) == 0 {
		return result
	}
	metas, metaErrors := e.batchReadMetadata(names)
	type commitRead struct {
		name    string
		parent  string
		rr      git.RevRange
		commits []string
		err     error
	}
	reads := make([]*commitRead, 0, len(names))
	revNames := make([]string, 0, len(names)*2)
	for _, name := range names {
		if err := metaErrors[name]; err != nil {
			result.Errors[name] = err
			continue
		}
		meta := metas[name]
		if meta == nil {
			result.Errors[name] = fmt.Errorf("metadata for branch %s was not read", name)
			continue
		}
		read := &commitRead{name: name}
		if base := meta.GetParentBranchRevision(); base != nil {
			read.rr.Base = *base
		}
		if read.rr.Base == "" {
			read.parent = e.GetBranch(name).GetParentOrTrunk()
			revNames = append(revNames, read.parent)
		}
		revNames = append(revNames, name)
		reads = append(reads, read)
	}
	if len(reads) == 0 {
		return result
	}
	revs, revErrors := e.GetRevisions(revNames)
	revisionErr := errors.Join(revErrors...)
	for _, read := range reads {
		read.rr.Head = revs[read.name]
		if read.parent != "" {
			read.rr.Base = revs[read.parent]
		}
		if read.rr.Head == "" || read.rr.Base == "" {
			read.err = errors.Join(fmt.Errorf("cannot resolve commit range for branch %s", read.name), revisionErr)
		}
	}
	utils.Run(reads, func(read *commitRead) {
		if read.err == nil {
			read.commits, read.err = e.commitsBetween(read.rr, format)
		}
	})
	for _, read := range reads {
		if read.err != nil {
			result.Errors[read.name] = read.err
		} else {
			result.Commits[read.name] = read.commits
		}
	}
	return result
}
