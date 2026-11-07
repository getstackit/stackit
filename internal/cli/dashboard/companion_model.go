package dashboard

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
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

// companionKeyMap defines the companion panel's keyboard shortcuts.
type companionKeyMap struct {
	Ship key.Binding
	Quit key.Binding
}

var companionKeys = companionKeyMap{
	Ship: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "open ship dashboard"),
	),
	Quit: key.NewBinding(
		key.WithKeys(core.KeyQuit, core.KeyCtrlC),
		key.WithHelp("q", "quit"),
	),
}

// refWatcher is the ref watcher surface the panel drives; RefWatcher.Stop is
// idempotent.
type refWatcher interface {
	Start() error
	Stop()
}

// companionModel owns the local state displayed by the read-mostly companion
// panel. Every ref-triggered refresh rebuilds the engine from Git so stale
// graph state is never patched in memory.
//
// The panel stays live while the ship dashboard is open: shippableModel
// forwards companion messages to handleBackgroundMsg, so the working-tree tick
// chain and ref-triggered reloads keep running and the panel is current when
// the user returns.
type companionModel struct {
	core.BaseModel

	ctx     *app.Context
	cfg     config.Configurer
	options CompanionOptions

	engine      engine.Engine
	git         statusReader
	analysis    *shippable.AnalysisResult
	workingTree workingTreeSummary
	renderer    *tree.StackTreeRenderer

	watcher refWatcher

	loading bool
	// pendingReload records a ref change that arrived while a reload was in
	// flight. That reload may have read refs before the change, so another
	// one runs as soon as it completes.
	pendingReload bool
	lastRefresh   time.Time
	errorMessage  string
}

type (
	companionReloadMsg struct {
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
	return &companionModel{
		ctx:     ctx,
		cfg:     cfg,
		options: opts,
		engine:  ctx.Engine,
		// The engine no longer exposes its runner, so the panel keeps its own
		// to read working-tree status.
		git: git.NewRunnerWithPath(ctx.RepoRoot, ctx.Logger),
	}
}

// startWatcher starts the ref watcher after the Bubble Tea program exists so
// its callback can enqueue refresh messages without touching model state.
func (m *companionModel) startWatcher(program *tea.Program) {
	w := watcher.NewRefWatcher(m.ctx.RepoRoot, companionWatcherDebounce, func() {
		program.Send(companionRefChangedMsg{})
	})
	m.watcher = w
	go func() {
		program.Send(companionWatcherStoppedMsg{err: w.Start()})
	}()
}

// stopWatcher stops the ref watcher. It is called once the program has exited,
// never from Update: the panel must keep watching while the ship view is open.
func (m *companionModel) stopWatcher() {
	if m.watcher != nil {
		m.watcher.Stop()
	}
}

func (m *companionModel) Init() tea.Cmd {
	m.SignalReady()
	return tea.Batch(m.requestReload(), m.tick())
}

func (m *companionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.handleBackgroundMsg(msg); handled {
		return m, cmd
	}

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, companionKeys.Quit):
			return m, tea.Quit
		case key.Matches(keyMsg, companionKeys.Ship):
			ship := newShippableModel(m.ctx, m.cfg, ShippableOptions{RunLocalCI: m.options.RunLocalCI}).withCompanion(m)
			// The new view has seen no resize yet, so ask for the size.
			return ship, tea.Batch(ship.Init(), tea.RequestWindowSize)
		}
		return m, nil
	}

	// Window sizes and spinner ticks. A spinner tick left over from a closed
	// ship view lands here; the panel's spinner ignores the foreign ID, which
	// ends that tick chain.
	_, cmd := m.HandleCommonMsg(msg)
	return m, cmd
}

// handleBackgroundMsg applies the panel's own asynchronous messages. It runs
// whichever view is active, so reload results, ref changes, and the
// working-tree tick are never dropped while the ship dashboard is open.
func (m *companionModel) handleBackgroundMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case companionRefChangedMsg:
		return m.requestReload(), true

	case companionWatcherStoppedMsg:
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("watcher: %v", msg.err)
		}
		return nil, true

	case companionReloadMsg:
		m.loading = false
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
		} else {
			m.analysis = msg.analysis
			m.workingTree = msg.workingTree
			m.renderer = msg.renderer
			m.lastRefresh = time.Now()
			m.errorMessage = ""
		}
		if m.pendingReload {
			m.pendingReload = false
			return m.requestReload(), true
		}
		return nil, true

	case companionWorkingTreeMsg:
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			return nil, true
		}
		m.workingTree = msg.workingTree
		return nil, true

	case companionTickMsg:
		return tea.Batch(m.readWorkingTree(), m.tick()), true
	}
	return nil, false
}

// requestReload starts a reload, or defers one behind the reload already in
// flight so ref changes are never coalesced into a stale result.
func (m *companionModel) requestReload() tea.Cmd {
	if m.loading {
		m.pendingReload = true
		return nil
	}
	m.loading = true
	return m.reload()
}

func (m *companionModel) reload() tea.Cmd {
	ctx := m.ctx.Context
	if ctx == nil {
		ctx = context.Background()
	}
	eng := m.engine
	runner := m.git
	return func() tea.Msg {
		if err := eng.Rebuild(eng.Trunk().GetName()); err != nil {
			return companionReloadMsg{err: fmt.Errorf("reload stack state: %w", err)}
		}

		analysis, err := shippable.NewAnalyzer(eng, nil).AnalyzeAllLocal()
		if err != nil {
			return companionReloadMsg{err: fmt.Errorf("analyze local stack state: %w", err)}
		}
		analysis = filterCompanionStacks(eng, analysis, m.options.StackOnly)

		workingTree, err := readWorkingTree(ctx, runner)
		if err != nil {
			return companionReloadMsg{err: err}
		}
		return companionReloadMsg{
			analysis:    analysis,
			workingTree: workingTree,
			renderer:    buildCompanionRenderer(eng),
		}
	}
}

func filterCompanionStacks(eng engine.Engine, analysis *shippable.AnalysisResult, stackOnly bool) *shippable.AnalysisResult {
	currentBranch := eng.CurrentBranchName()
	if !stackOnly || analysis == nil || currentBranch == eng.Trunk().GetName() {
		return analysis
	}
	return analysis.Filter(func(stack shippable.Stack) bool {
		return slices.Contains(stack.Stack.AllBranches, currentBranch)
	})
}

func buildCompanionRenderer(eng engine.Engine) *tree.StackTreeRenderer {
	branches := eng.AllBranches()
	stats := eng.BatchBranchStats(branches)
	statuses := eng.ReadBranchStatuses(branches)
	annotations := make(map[string]tree.BranchAnnotation, len(branches))
	for _, branch := range branches {
		annotation := tui.TreeAnnotation(stackview.BaseAnnotation(eng, branch, stats[branch.GetName()], stackview.AnnotationOptions{
			SkipCommitMessages: true,
		}))
		annotation.NeedsRestack = !statuses.IsUpToDate(branch)
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
		workingTree, err := readWorkingTree(ctx, m.git)
		return companionWorkingTreeMsg{workingTree: workingTree, err: err}
	}
}

func (m *companionModel) tick() tea.Cmd {
	return tea.Tick(companionWorkingTreeTick, func(t time.Time) tea.Msg {
		return companionTickMsg(t)
	})
}

// statusReader is the git surface the panel needs to summarize the working tree.
type statusReader interface {
	GetStatusPorcelain(ctx context.Context) (string, error)
}

func readWorkingTree(ctx context.Context, runner statusReader) (workingTreeSummary, error) {
	output, err := runner.GetStatusPorcelain(ctx)
	if err != nil {
		return workingTreeSummary{}, fmt.Errorf("read working tree: %w", err)
	}

	// Porcelain v1: "XY path", X is the staged status, Y the unstaged one. A
	// file can be both staged and modified again, so it counts in both.
	var summary workingTreeSummary
	for line := range strings.SplitSeq(strings.TrimSuffix(output, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		x, y := line[0], line[1]
		switch {
		case x == '?' && y == '?':
			summary.untracked++
			continue
		case x != ' ':
			summary.staged++
		}
		if y != ' ' {
			summary.unstaged++
		}
	}
	return summary, nil
}
