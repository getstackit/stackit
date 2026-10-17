package dashboard

import (
	"context"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/shippable"
	"github.com/getstackit/stackit/internal/tui/core"
)

type fakeRefWatcher struct {
	stops int
}

func (w *fakeRefWatcher) Start() error { return nil }
func (w *fakeRefWatcher) Stop()        { w.stops++ }

type fakeStatusReader struct {
	output string
}

func (f fakeStatusReader) GetStatusPorcelain(context.Context) (string, error) {
	return f.output, nil
}

// newTestCompanion builds a panel whose commands can be constructed but are
// never executed, so no repository is needed.
func newTestCompanion() (*companionModel, *fakeRefWatcher) {
	w := &fakeRefWatcher{}
	return &companionModel{
		ctx:     &app.Context{},
		git:     fakeStatusReader{},
		watcher: w,
	}, w
}

func TestCompanionReloadMessageUpdatesLocalState(t *testing.T) {
	t.Parallel()

	m, _ := newTestCompanion()
	m.loading = true
	workingTree := workingTreeSummary{staged: 2, unstaged: 1, untracked: 3}
	analysis := &shippable.AnalysisResult{}

	updated, cmd := m.Update(companionReloadMsg{
		analysis:    analysis,
		workingTree: workingTree,
	})

	require.Same(t, m, updated)
	require.Nil(t, cmd)
	require.False(t, m.loading)
	require.Same(t, analysis, m.analysis)
	require.Equal(t, workingTree, m.workingTree)
	require.False(t, m.lastRefresh.IsZero())
}

func TestCompanionSpinnerTickDoesNotStopWatcher(t *testing.T) {
	t.Parallel()

	m, w := newTestCompanion()

	// A tick left over from a closed ship view's spinner.
	updated, _ := m.Update(spinner.TickMsg{ID: 42})

	require.Same(t, m, updated)
	require.Zero(t, w.stops, "only quitting the program may stop the watcher")
}

func TestCompanionQuitKeyQuitsWithoutStoppingWatcherInUpdate(t *testing.T) {
	t.Parallel()

	m, w := newTestCompanion()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: core.KeyQuit})

	require.NotNil(t, cmd)
	require.IsType(t, tea.QuitMsg{}, cmd())
	require.Zero(t, w.stops, "RunCompanion stops the watcher after the program exits")
}

func TestCompanionRefChangeDuringReloadRerunsReload(t *testing.T) {
	t.Parallel()

	m, _ := newTestCompanion()
	m.loading = true

	_, cmd := m.Update(companionRefChangedMsg{})
	require.Nil(t, cmd, "a reload is already in flight")
	require.True(t, m.pendingReload)

	_, cmd = m.Update(companionReloadMsg{analysis: &shippable.AnalysisResult{}})
	require.NotNil(t, cmd, "the deferred ref change must trigger another reload")
	require.True(t, m.loading)
	require.False(t, m.pendingReload)

	_, cmd = m.Update(companionReloadMsg{analysis: &shippable.AnalysisResult{}})
	require.Nil(t, cmd, "no further change arrived, so reloading stops")
	require.False(t, m.loading)
}

func TestCompanionStaysLiveWhileShipViewIsOpen(t *testing.T) {
	t.Parallel()

	m, w := newTestCompanion()
	ship := &shippableModel{companion: m, state: stateMain}

	// The steps share one panel and run in order, so they are not subtests.
	updated, cmd := ship.Update(companionTickMsg(time.Now()))
	require.Same(t, ship, updated)
	require.NotNil(t, cmd, "the working-tree tick must keep re-arming")

	summary := workingTreeSummary{staged: 1}
	_, _ = ship.Update(companionWorkingTreeMsg{workingTree: summary})
	require.Equal(t, summary, m.workingTree, "working-tree results must reach the panel")

	_, cmd = ship.Update(companionRefChangedMsg{})
	require.NotNil(t, cmd, "ref changes must reload the panel")
	require.True(t, m.loading)
	_, _ = ship.Update(companionReloadMsg{analysis: &shippable.AnalysisResult{}})
	require.False(t, m.loading)

	_, _ = ship.Update(spinner.TickMsg{ID: 42})
	require.Zero(t, w.stops, "spinner ticks must not stop the watcher")

	updated, cmd = ship.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.Same(t, m, updated, "escape returns to the panel")
	require.NotNil(t, cmd)
}

func TestReadWorkingTreeParsesPorcelain(t *testing.T) {
	t.Parallel()

	output := "MM both.go\n" +
		"M  staged.go\n" +
		" M unstaged.go\n" +
		"R  old.go -> new.go\n" +
		"A  added.go\n" +
		"?? untracked.go\n" +
		"?? other.go\n"

	summary, err := readWorkingTree(context.Background(), fakeStatusReader{output: output})
	require.NoError(t, err)
	require.Equal(t, workingTreeSummary{staged: 4, unstaged: 2, untracked: 2}, summary)
}

func TestEscapeStaysInShipViewWhileBusy(t *testing.T) {
	t.Parallel()

	for _, state := range []dashboardState{stateLoading, stateShipping} {
		m, _ := newTestCompanion()
		ship := &shippableModel{companion: m, state: state}
		updated, _ := ship.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		require.Same(t, ship, updated, "an operation's result must not be orphaned by leaving its view")
	}
}
