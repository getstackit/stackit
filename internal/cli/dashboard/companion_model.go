package dashboard

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/shippable"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/watcher"
)

const (
	companionWatcherDebounce = 200 * time.Millisecond
	companionWorkingTreeTick = 2 * time.Second
)

// CompanionOptions configure the companion panel and are passed through when
// it opens the existing shipping dashboard.
type CompanionOptions struct {
	RunLocalCI bool
	StackOnly  bool
}

type workingTreeSummary struct {
	staged    int
	unstaged  int
	untracked int
}

// companionModel owns the local state displayed by the read-mostly companion
// panel. Every ref-triggered refresh constructs a fresh engine so stale graph
// state is never patched in memory.
type companionModel struct {
	core.BaseModel

	ctx     *app.Context
	cfg     config.Configurer
	options CompanionOptions

	engine      engine.Engine
	analysis    *shippable.AnalysisResult
	workingTree workingTreeSummary
	renderer    *tree.StackTreeRenderer

	watcher     *watcher.RefWatcher
	watcherOnce sync.Once

	loading      bool
	lastRefresh  time.Time
	errorMessage string
}

type (
	companionReloadMsg struct {
		engine      engine.Engine
		analysis    *shippable.AnalysisResult
		workingTree workingTreeSummary
		renderer    *tree.StackTreeRenderer
		err         error
	}
	companionWorkingTreeMsg struct {
		workingTree workingTreeSummary
		err         error
	}
	companionRefChangedMsg     struct{}
	companionWatcherStoppedMsg struct{ err error }
	companionTickMsg           time.Time
)

func newCompanionModel(ctx *app.Context, cfg config.Configurer, opts CompanionOptions) *companionModel {
	m := &companionModel{
		ctx:     ctx,
		cfg:     cfg,
		options: opts,
		engine:  ctx.Engine,
		loading: true,
	}
	m.watcher = watcher.NewRefWatcher(ctx.RepoRoot, companionWatcherDebounce, func() {})
	return m
}

// startWatcher starts the ref watcher after the Bubble Tea program exists so
// its callback can enqueue refresh messages without touching model state.
func (m *companionModel) startWatcher(program *tea.Program) {
	m.watcher = watcher.NewRefWatcher(m.ctx.RepoRoot, companionWatcherDebounce, func() {
		program.Send(companionRefChangedMsg{})
	})
	go func() {
		program.Send(companionWatcherStoppedMsg{err: m.watcher.Start()})
	}()
}

func (m *companionModel) stopWatcher() {
	m.watcherOnce.Do(m.watcher.Stop)
}

func (m *companionModel) Init() tea.Cmd {
	m.SignalReady()
	return tea.Batch(m.reload(), m.tick())
}

func (m *companionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := m.HandleCommonMsg(msg); handled {
		m.stopWatcher()
		return m, cmd
	}

	switch msg := msg.(type) {
	case companionRefChangedMsg:
		if m.loading {
			return m, nil
		}
		m.loading = true
		return m, m.reload()

	case companionWatcherStoppedMsg:
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("watcher: %v", msg.err)
		}
		return m, nil

	case companionReloadMsg:
		m.loading = false
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.engine = msg.engine
		m.analysis = msg.analysis
		m.workingTree = msg.workingTree
		m.renderer = msg.renderer
		m.lastRefresh = time.Now()
		m.errorMessage = ""
		return m, nil

	case companionWorkingTreeMsg:
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return m, nil
		}
		m.workingTree = msg.workingTree
		return m, nil

	case companionTickMsg:
		return m, tea.Batch(m.readWorkingTree(), m.tick())

	case tea.KeyPressMsg:
		if msg.String() == "s" {
			shipModel := newShippableModel(m.ctx, m.cfg, ShippableOptions{RunLocalCI: m.options.RunLocalCI}).withCompanion(m)
			return shipModel, shipModel.Init()
		}
	}

	return m, nil
}

func (m *companionModel) reload() tea.Cmd {
	ctx := m.ctx.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		fresh, err := engine.NewEngine(engine.Options{
			RepoRoot: m.ctx.RepoRoot,
			Trunk:    m.engine.Trunk().GetName(),
			Git:      m.engine.Git(),
		})
		if err != nil {
			return companionReloadMsg{err: fmt.Errorf("reload stack state: %w", err)}
		}

		analysis, err := shippable.NewAnalyzer(fresh, nil).AnalyzeAllLocal()
		if err != nil {
			return companionReloadMsg{err: fmt.Errorf("analyze local stack state: %w", err)}
		}
		analysis = filterCompanionStacks(fresh, analysis, m.options.StackOnly)

		workingTree, err := readWorkingTree(ctx, fresh)
		if err != nil {
			return companionReloadMsg{err: err}
		}
		return companionReloadMsg{
			engine:      fresh,
			analysis:    analysis,
			workingTree: workingTree,
			renderer:    buildCompanionRenderer(fresh),
		}
	}
}

func filterCompanionStacks(eng engine.Engine, analysis *shippable.AnalysisResult, stackOnly bool) *shippable.AnalysisResult {
	if !stackOnly || analysis == nil || eng.CurrentBranchName() == eng.Trunk().GetName() {
		return analysis
	}

	currentBranch := eng.CurrentBranchName()
	result := &shippable.AnalysisResult{Stacks: make([]shippable.Stack, 0, 1)}
	for _, stack := range analysis.Stacks {
		for _, branchName := range stack.Stack.AllBranches {
			if branchName != currentBranch {
				continue
			}
			result.Stacks = append(result.Stacks, stack)
			switch stack.Status {
			case shippable.StatusShippable:
				result.ShippableCount++
			case shippable.StatusPending:
				result.PendingCount++
			case shippable.StatusBlocked:
				result.BlockedCount++
			case shippable.StatusIncomplete:
				result.IncompleteCount++
			}
			return result
		}
	}
	return result
}

func buildCompanionRenderer(eng engine.Engine) *tree.StackTreeRenderer {
	branches := eng.AllBranches()
	stats := eng.BatchBranchStats(branches)
	annotations := make(map[string]tree.BranchAnnotation, len(branches))
	for _, branch := range branches {
		annotation := tui.GetBranchAnnotation(eng, branch, stats[branch.GetName()], tui.AnnotationOptions{
			SkipCommitMessages: true,
		})
		annotation.NeedsRestack = branch.NeedsRestack()
		annotations[branch.GetName()] = annotation
	}

	renderer := tui.NewStackTreeRenderer(eng)
	renderer.SetAnnotations(annotations)
	return renderer
}

func (m *companionModel) readWorkingTree() tea.Cmd {
	ctx := m.ctx.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		workingTree, err := readWorkingTree(ctx, m.engine)
		return companionWorkingTreeMsg{workingTree: workingTree, err: err}
	}
}

func (m *companionModel) tick() tea.Cmd {
	return tea.Tick(companionWorkingTreeTick, func(t time.Time) tea.Msg {
		return companionTickMsg(t)
	})
}

func readWorkingTree(ctx context.Context, eng engine.WorkingTree) (workingTreeSummary, error) {
	changes, err := eng.GetPendingChanges(ctx)
	if err != nil {
		return workingTreeSummary{}, fmt.Errorf("read working tree: %w", err)
	}

	var summary workingTreeSummary
	for _, change := range changes {
		switch {
		case change.Staged:
			summary.staged++
		case change.Status == "??":
			summary.untracked++
		default:
			summary.unstaged++
		}
	}
	return summary, nil
}
