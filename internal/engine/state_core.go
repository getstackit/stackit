package engine

import (
	"slices"

	"github.com/getstackit/stackit/internal/git"
)

// BranchState holds the cached metadata state for a single branch.
// This consolidates what was previously stored in separate maps.
type BranchState struct {
	Parent               string         // Parent branch name
	ParentBranchRevision string         // Stored parent SHA from metadata; "" if none recorded
	Scope                string         // Scope string (may be empty)
	LockReason           git.LockReason // Lock reason (empty if not locked)
	Frozen               bool           // Whether branch is frozen (local-only state)
	BranchType           git.BranchType // Branch type (worktree-anchor, utility, etc.)
	LocalModified        bool           // Has local metadata changes not yet pushed
}

// HasScope returns true if this branch has an explicit scope set.
func (s *BranchState) HasScope() bool {
	return s.Scope != ""
}

// GetScope returns the scope as a Scope type.
func (s *BranchState) GetScope() Scope {
	return NewScope(s.Scope)
}

// IsLocked returns true if this branch is locked.
func (s *BranchState) IsLocked() bool {
	return s.LockReason.IsLocked()
}

// BranchStateMap is a map of branch names to their state.
type BranchStateMap map[string]*BranchState

// Get returns the state for a branch, or nil if not found.
func (m BranchStateMap) Get(branch Branch) *BranchState {
	return m[branch.GetName()]
}

// GetByName returns the state for a branch name, or nil if not found.
func (m BranchStateMap) GetByName(name string) *BranchState {
	return m[name]
}

// Has returns true if the branch exists in the map.
func (m BranchStateMap) Has(branch Branch) bool {
	_, ok := m[branch.GetName()]
	return ok
}

// HasByName returns true if the branch name exists in the map.
func (m BranchStateMap) HasByName(name string) bool {
	_, ok := m[name]
	return ok
}

// Set sets the state for a branch.
func (m BranchStateMap) Set(name string, state *BranchState) {
	m[name] = state
}

// Delete removes a branch from the map.
func (m BranchStateMap) Delete(name string) {
	delete(m, name)
}

// GetOrCreate returns the state for a branch, creating it if it doesn't exist.
func (m BranchStateMap) GetOrCreate(name string) *BranchState {
	if state, ok := m[name]; ok {
		return state
	}
	state := &BranchState{}
	m[name] = state
	return state
}

type stateCore struct {
	branches       []string
	branchNamesSet *BranchSet          // cached set for O(1) lookups, lazily built
	branchState    BranchStateMap      // branch -> consolidated state
	childrenMap    map[string][]string // branch -> children (computed from parents)
}

func newStateCore() *stateCore {
	return &stateCore{
		branchState: make(BranchStateMap),
		childrenMap: make(map[string][]string),
	}
}

func newStateCoreFromSnapshot(branches []string, branchState BranchStateMap, childrenMap map[string][]string) *stateCore {
	return &stateCore{
		branches:    branches,
		branchState: branchState,
		childrenMap: childrenMap,
	}
}

func (s *stateCore) setBranches(branches []string) {
	s.branches = branches
	s.branchNamesSet = nil // invalidate cache
}

func (s *stateCore) removeFromChildren(parent, child string) {
	if children, ok := s.childrenMap[parent]; ok {
		if i := slices.Index(children, child); i >= 0 {
			s.childrenMap[parent] = slices.Delete(children, i, i+1)
		}
		if len(s.childrenMap[parent]) == 0 {
			delete(s.childrenMap, parent)
		}
	}
}

func (s *stateCore) removeBranch(branch string) {
	state := s.branchState.GetByName(branch)
	if state == nil {
		return
	}

	if state.Parent != "" {
		s.removeFromChildren(state.Parent, branch)
	}

	s.branchState.Delete(branch)
	delete(s.childrenMap, branch)
}

func (s *stateCore) rebuildFromMetadata(
	trunk string,
	branches []string,
	allMeta map[string]*git.Meta,
	allLocalMeta git.LocalMetaMap,
) {
	s.applySharedMetadata(trunk, branches, allMeta)
	s.applyLocalMetadata(allLocalMeta)
}

// applySharedMetadata rebuilds branchState and childrenMap from shared
// metadata (refs/stackit/metadata/*). Resets branchState before populating
// so stale entries from external mutations are dropped. Frozen is NOT
// modified here — call applyLocalMetadata to fill that in.
//
// Callers must hold the engine write lock.
func (s *stateCore) applySharedMetadata(
	trunk string,
	branches []string,
	allMeta map[string]*git.Meta,
) {
	s.setBranches(branches)

	// Reset tracked-state caches from fresh metadata snapshot.
	s.branchState = make(BranchStateMap)
	s.childrenMap = make(map[string][]string)

	for name, meta := range allMeta {
		if name == trunk {
			continue // Trunk branches should never be tracked
		}

		if meta.GetParentBranchName() == nil {
			continue // No parent means not tracked
		}

		parent := *meta.GetParentBranchName()
		if parent == name {
			continue // Skip self-parenting to avoid cycles
		}

		state := &BranchState{
			Parent:     parent,
			LockReason: meta.GetLockReason(),
			BranchType: meta.GetBranchType(),
		}
		if meta.GetScope() != nil {
			state.Scope = *meta.GetScope()
		}
		if rev := meta.GetParentBranchRevision(); rev != nil {
			state.ParentBranchRevision = *rev
		}

		s.branchState.Set(name, state)
		s.childrenMap[parent] = append(s.childrenMap[parent], name)
	}

	for _, children := range s.childrenMap {
		slices.Sort(children)
	}
}

// applyLocalMetadata layers local-only state (Frozen) onto branchState.
// Safe to call after applySharedMetadata or independently; entries for
// branches not already in branchState are created as bare wrappers so the
// flag is preserved for untracked branches.
//
// Callers must hold the engine write lock.
func (s *stateCore) applyLocalMetadata(allLocalMeta git.LocalMetaMap) {
	for name, meta := range allLocalMeta {
		if meta.Frozen {
			state := s.branchState.GetOrCreate(name)
			state.Frozen = true
		}
	}
}

func (s *stateCore) updateBranchStateFromMeta(branch string, meta *git.Meta) {
	state := s.branchState.GetOrCreate(branch)

	if parentName := meta.GetParentBranchName(); parentName != nil {
		if state.Parent != "" && state.Parent != *parentName {
			s.removeFromChildren(state.Parent, branch)
		}

		state.Parent = *parentName

		if state.Parent != "" && !slices.Contains(s.childrenMap[state.Parent], branch) {
			s.childrenMap[state.Parent] = append(s.childrenMap[state.Parent], branch)
			slices.Sort(s.childrenMap[state.Parent])
		}
	}

	if meta.GetScope() != nil {
		state.Scope = *meta.GetScope()
	} else {
		state.Scope = ""
	}

	if rev := meta.GetParentBranchRevision(); rev != nil {
		state.ParentBranchRevision = *rev
	} else {
		state.ParentBranchRevision = ""
	}

	state.LockReason = meta.GetLockReason()
	state.BranchType = meta.GetBranchType()
}

func (s *stateCore) updateBranchStateFromLocalMeta(branch string, meta *git.LocalMeta) {
	state := s.branchState.GetOrCreate(branch)
	state.Frozen = meta.Frozen
}
