package navigation

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/stacklog"
	"github.com/getstackit/stackit/internal/actions/trunklog"
)

func TestBuildLogRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		since   string
		count   int
		want    trunklog.Request
		wantErr string
	}{
		{name: "default count view", count: 25, want: trunklog.Request{Count: 25}},
		{name: "explicit range", args: []string{"v1..main"}, want: trunklog.Request{From: "v1", To: "main"}},
		{name: "open-ended range to trunk", args: []string{"v1.."}, want: trunklog.Request{From: "v1"}},
		{name: "since shorthand", since: "v1", want: trunklog.Request{From: "v1"}},
		{name: "range and since conflict", args: []string{"v1..main"}, since: "v1", wantErr: "cannot combine"},
		{name: "missing range separator", args: []string{"foo"}, wantErr: "expected a range"},
		{name: "missing lower bound", args: []string{"..main"}, wantErr: "missing a lower bound"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildLogRequest(tt.args, tt.since, tt.count)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRenderLogUsesColorAndPlainStructure(t *testing.T) {
	t.Parallel()

	got := renderLog(trunklog.Result{Commits: []trunklog.Commit{
		{
			SHA:      "abcdef1234567890",
			Message:  "feat: standalone",
			PRNumber: 42,
		},
		{
			SHA:           "1234567890abcdef",
			Message:       "Sync output quality",
			PRNumber:      99,
			StackSize:     2,
			StackScope:    "CLI",
			StackPRs:      []int{10, 11},
			StackPRTitles: map[int]string{10: "first", 11: "second"},
		},
	}}, nil, "", "")

	require.Contains(t, got, "\x1b[")
	plain := ansi.Strip(got)
	require.Equal(t, strings.Join([]string{
		"abcdef1  feat: standalone (#42)",
		"1234567  Sync output quality  (#99 · 2 PRs · CLI)",
		"    #10 first",
		"    #11 second",
	}, "\n"), plain)
}

func TestRenderLogEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, renderLog(trunklog.Result{}, nil, "", ""))
}

func TestRenderDefaultViewStackBandDividerAndDecorations(t *testing.T) {
	t.Parallel()

	stack := stacklog.Result{
		TrunkName:   "main",
		TrunkTipSHA: "9f81673000000000",
		Branches: []stacklog.Branch{
			{
				Name:      "auth-api",
				IsCurrent: true,
				Commits:   []stacklog.Commit{{SHA: "a1b2c3d000000000", Subject: "feat: wire auth API"}},
			},
			{
				Name:    "auth-base",
				Commits: []stacklog.Commit{{SHA: "e4f5a6b000000000", Subject: "feat: add base types"}},
			},
		},
		Decorations: map[string][]stacklog.Decoration{
			// Branch head on the current tip plus a tag — the head is suppressed
			// (shown as the header), the tag is surfaced.
			"a1b2c3d000000000": {{Name: "auth-api"}, {Name: "v2.0.0", IsTag: true}},
			// A release tag down in trunk history.
			"89e1be9000000000": {{Name: "v1.4.0", IsTag: true}},
		},
	}
	trunk := trunklog.Result{Commits: []trunklog.Commit{
		{SHA: "89e1be9000000000", Message: "feat: configure metadata", PRNumber: 1338},
	}}

	plain := ansi.Strip(renderDefaultView(stack, trunk))

	require.Equal(t, strings.Join([]string{
		"auth-api",
		"  ◉ a1b2c3d  feat: wire auth API (tag: v2.0.0)",
		"auth-base",
		"  ◯ e4f5a6b  feat: add base types",
		"──────── main 9f81673 ────────",
		"89e1be9  feat: configure metadata (#1338) (tag: v1.4.0)",
	}, "\n"), plain)
}

func TestRenderDefaultViewOnTrunkIsTrunkOnly(t *testing.T) {
	t.Parallel()

	stack := stacklog.Result{
		TrunkName:   "main",
		TrunkTipSHA: "9f81673000000000",
		OnTrunk:     true,
	}
	trunk := trunklog.Result{Commits: []trunklog.Commit{
		{SHA: "9f81673000000000", Message: "feat: latest"},
	}}

	plain := ansi.Strip(renderDefaultView(stack, trunk))

	// No stack band; the divider (with the tip skipped from decoration) leads
	// straight into trunk history.
	require.Equal(t, strings.Join([]string{
		"──────── main 9f81673 ────────",
		"9f81673  feat: latest",
	}, "\n"), plain)
}

func TestLogPagerViewUsesAltScreen(t *testing.T) {
	t.Parallel()

	model := newLogPagerModel("abcdef1  feat: test", 1)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	view := updated.(*logPagerModel).View()

	require.True(t, view.AltScreen)
	require.Contains(t, view.Content, "Stackit Log | 1 commits")
	require.Contains(t, ansi.Strip(view.Content), "abcdef1  feat: test")
}
