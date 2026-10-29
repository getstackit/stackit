// Package debug implements the debug command, which dumps stack, snapshot,
// continuation, and remote metadata state as JSON for troubleshooting.
package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// Engine lists exactly the engine methods debug calls.
type Engine interface {
	Snapshots() ([]engine.SnapshotInfo, error)
	LoadSnapshot(snapshotID string) (*engine.Snapshot, error)
	Trunk() engine.Branch
	CurrentBranch(ctx context.Context) *engine.Branch
	AllBranches() engine.Branches
	GetBranch(branchName string) engine.Branch
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	ListMetadataRefs(ctx context.Context) (map[string]string, error)
	BatchReadMetadataRaw(ctx context.Context, branchNames []string) (engine.MetaMap, map[string]error)
	BatchRevisions(ctx context.Context, branches engine.Branches) engine.RevisionMap
	ReadBranchStatuses(ctx context.Context, branches engine.Branches) engine.BranchStatuses
	GetRemoteURL(ctx context.Context) (string, error)
	EnsureRemoteMetadata(ctx context.Context) error
	LoadRemoteMetadataCache(ctx context.Context) error
	RemoteMetadataCache() engine.RemoteMetadataView
	IsRemoteSyncEnabled(ctx context.Context) bool
}

// Deps are the collaborators debug needs.
type Deps struct {
	Engine   Engine
	Out      output.Output
	RepoRoot string
}

// Options contains options for the debug command
type Options struct {
	Limit      int  // Limit number of recent commands to show (0 = all)
	ShowRemote bool // Fetch and show remote metadata state
}

// Info represents the complete debugging information
type Info struct {
	Timestamp           time.Time                `json:"timestamp"`
	RecentCommands      []CommandSnapshot        `json:"recent_commands"`
	StackState          StackStateInfo           `json:"stack_state"`
	ContinuationState   *ContinuationStateInfo   `json:"continuation_state,omitempty"`
	RepositoryInfo      RepositoryInfo           `json:"repository_info"`
	RemoteMetadataState *RemoteMetadataStateInfo `json:"remote_metadata_state,omitempty"`
}

// RemoteMetadataStateInfo represents the state of metadata on the remote
type RemoteMetadataStateInfo struct {
	RemoteStateAvailable bool                     `json:"remote_state_available"`
	RemoteRefs           map[string]RemoteRefInfo `json:"remote_refs,omitempty"`
	LocalVsRemoteDiffs   []MetadataDiffInfo       `json:"local_vs_remote_diffs,omitempty"`
}

// RemoteRefInfo represents information about a single remote ref
type RemoteRefInfo struct {
	SHA          string `json:"sha"`
	LastModified string `json:"last_modified,omitempty"`
	ModifiedBy   string `json:"modified_by,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// MetadataDiffInfo represents a difference between local and remote metadata
type MetadataDiffInfo struct {
	Branch      string   `json:"branch"`
	DiffFields  []string `json:"diff_fields"`
	LocalValue  string   `json:"local_value,omitempty"`
	RemoteValue string   `json:"remote_value,omitempty"`
}

// CommandSnapshot represents a single command from the undo history
type CommandSnapshot struct {
	Timestamp     time.Time `json:"timestamp"`
	Command       string    `json:"command"`
	Args          []string  `json:"args"`
	CurrentBranch string    `json:"current_branch"`
}

// StackStateInfo represents the complete stack state
type StackStateInfo struct {
	Trunk         string       `json:"trunk"`
	CurrentBranch string       `json:"current_branch"`
	Branches      []BranchInfo `json:"branches"`
}

// BranchInfo represents detailed information about a branch
type BranchInfo struct {
	Name           string         `json:"name"`
	SHA            string         `json:"sha,omitempty"`
	Parent         string         `json:"parent,omitempty"`
	ParentRevision string         `json:"parent_revision,omitempty"`
	Children       []string       `json:"children,omitempty"`
	IsTracked      bool           `json:"is_tracked"`
	IsFixed        bool           `json:"is_fixed"`
	IsTrunk        bool           `json:"is_trunk"`
	PRInfo         *engine.PrInfo `json:"pr_info,omitempty"`
	MetadataRefSHA string         `json:"metadata_ref_sha,omitempty"`
}

// ContinuationStateInfo represents continuation state
type ContinuationStateInfo struct {
	BranchesToRestack      []string `json:"branches_to_restack,omitempty"`
	BranchesToSync         []string `json:"branches_to_sync,omitempty"`
	CurrentBranchOverride  string   `json:"current_branch_override,omitempty"`
	RebasedBranchBase      string   `json:"rebased_branch_base,omitempty"`
	ExpectedBranchRevision string   `json:"expected_branch_revision,omitempty"`
}

// RepositoryInfo represents basic repository information
type RepositoryInfo struct {
	RemoteURL string `json:"remote_url,omitempty"`
	RepoRoot  string `json:"repo_root,omitempty"`
}

// Action collects and outputs debugging information
func Action(ctx context.Context, deps Deps, opts Options) error {
	eng := deps.Engine
	repoRoot := deps.RepoRoot

	snapshotInfos, err := eng.Snapshots()
	if err != nil {
		snapshotInfos = []engine.SnapshotInfo{}
	}

	limit := opts.Limit
	if limit > 0 && limit < len(snapshotInfos) {
		snapshotInfos = snapshotInfos[:limit]
	}

	recentCommands := make([]CommandSnapshot, 0, len(snapshotInfos))
	for _, snapshotInfo := range snapshotInfos {
		fullSnapshot, err := eng.LoadSnapshot(snapshotInfo.ID)
		currentBranch := ""
		if err == nil && fullSnapshot != nil {
			currentBranch = fullSnapshot.CurrentBranch
		}

		recentCommands = append(recentCommands, CommandSnapshot{
			Timestamp:     snapshotInfo.Timestamp,
			Command:       snapshotInfo.Command,
			Args:          snapshotInfo.Args,
			CurrentBranch: currentBranch,
		})
	}

	trunk := eng.Trunk()
	currentBranch := eng.CurrentBranch(ctx)
	allBranches := eng.AllBranches()

	metadataRefs, err := eng.ListMetadataRefs(ctx)
	if err != nil {
		metadataRefs = make(map[string]string)
	}

	branchNames := allBranches.Names()
	allMeta, _ := eng.BatchReadMetadataRaw(ctx, branchNames)
	revisions := eng.BatchRevisions(ctx, allBranches)
	statuses := eng.ReadBranchStatuses(ctx, allBranches)

	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	branchInfos := make([]BranchInfo, 0, len(allBranches))
	for _, branch := range allBranches {
		branchName := branch.Name()
		branchInfo := BranchInfo{
			Name:      branchName,
			IsTrunk:   branch.IsTrunk(),
			IsTracked: branch.IsTracked(),
		}

		branchObj := eng.GetBranch(branchName)
		if sha, ok := revisions[branchName]; ok {
			branchInfo.SHA = sha
		}

		parent := branchObj.Parent()
		if parent != nil {
			branchInfo.Parent = parent.Name()
		}

		children := graph.ChildBranches(branchObj)
		if len(children) > 0 {
			childNames := make([]string, len(children))
			for i, c := range children {
				childNames[i] = c.Name()
			}
			branchInfo.Children = childNames
		}

		if meta, ok := allMeta[branchName]; ok && meta != nil {
			if rev := meta.GetParentBranchRevision(); rev != nil {
				branchInfo.ParentRevision = *rev
			}

			prInfo, err := branchObj.PrInfo()
			if err == nil && prInfo != nil {
				branchInfo.PRInfo = prInfo
			}
		}

		if metadataSHA, ok := metadataRefs[branchName]; ok {
			branchInfo.MetadataRefSHA = metadataSHA
		}

		branchInfo.IsFixed = statuses.IsUpToDate(branch)

		branchInfos = append(branchInfos, branchInfo)
	}

	var continuationState *ContinuationStateInfo
	contState, err := config.GetContinuationState(repoRoot)
	if err == nil && contState != nil {
		continuationState = &ContinuationStateInfo{
			BranchesToRestack:      contState.BranchesToRestack,
			BranchesToSync:         contState.BranchesToSync,
			CurrentBranchOverride:  contState.CurrentBranchOverride,
			RebasedBranchBase:      contState.RebasedBranchBase,
			ExpectedBranchRevision: contState.ExpectedBranchRevision,
		}
	}

	repoInfo := RepositoryInfo{
		RepoRoot: repoRoot,
	}
	remoteURL, err := eng.GetRemoteURL(ctx)
	if err == nil {
		repoInfo.RemoteURL = remoteURL
	}

	var remoteMetadataState *RemoteMetadataStateInfo
	if opts.ShowRemote {
		remoteCtx, cancelRemote := git.WithRemoteOperationTimeout(ctx)
		if err := eng.EnsureRemoteMetadata(remoteCtx); err != nil {
			deps.Out.Debug("Failed to fetch remote metadata: %v", err)
			_ = eng.LoadRemoteMetadataCache(ctx)
		}
		cancelRemote()
		remoteCache := eng.RemoteMetadataCache()

		remoteRefs := make(map[string]RemoteRefInfo)
		// We need to list the actual refs to get SHAs and potentially modification info
		// But for now, let's just use the cache
		remoteCache.Range(func(branch string, meta *git.Meta) bool {
			info := RemoteRefInfo{}
			if meta.GetLastModifiedAt() != nil {
				info.LastModified = meta.GetLastModifiedAt().Format(time.RFC3339)
			}
			if modBy := meta.GetLastModifiedBy(); modBy != nil {
				info.ModifiedBy = fmt.Sprintf("%s <%s>", modBy.GitName, modBy.GitEmail)
			}
			if meta.GetScope() != nil {
				info.Scope = *meta.GetScope()
			}
			remoteRefs[branch] = info
			return true
		})

		remoteMetadataState = &RemoteMetadataStateInfo{
			RemoteStateAvailable: eng.IsRemoteSyncEnabled(ctx),
			RemoteRefs:           remoteRefs,
		}
	}

	debugInfo := Info{
		Timestamp:      time.Now(),
		RecentCommands: recentCommands,
		StackState: StackStateInfo{
			Trunk: trunk.Name(),
			CurrentBranch: func() string {
				if currentBranch != nil {
					return currentBranch.Name()
				}
				return ""
			}(),
			Branches: branchInfos,
		},
		ContinuationState:   continuationState,
		RepositoryInfo:      repoInfo,
		RemoteMetadataState: remoteMetadataState,
	}

	jsonData, err := json.MarshalIndent(debugInfo, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal debug info: %w", err)
	}

	deps.Out.Print(string(jsonData))
	deps.Out.Newline()

	return nil
}
