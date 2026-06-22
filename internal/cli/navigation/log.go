package navigation

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/trunklog"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
)

// NewLogCmd creates the log command: a stack-aware view of trunk commit history.
func NewLogCmd() *cobra.Command {
	var (
		since   string
		jsonOut bool
		count   int
	)

	cmd := &cobra.Command{
		Use:   "log [<from>..<to>]",
		Short: "Show trunk commit history with stacks collapsed",
		Long: `Show the trunk commit history, collapsing each consolidated stack-merge into a
single entry that lists its constituent PRs (mirrors the web app's Recently
Merged view).

With no arguments it shows the most recent trunk commits. Pass a revision range
to produce a changelog between two refs.

Examples:
  stackit log                      # recent trunk history (stacks collapsed)
  stackit log v1.4.0..main         # changelog between two refs
  stackit log --since v1.4.0       # everything since v1.4.0 up to the trunk tip
  stackit log --json v1.4.0..main  # machine-readable (used by release tooling)

The --json output is a stability contract consumed by release tooling; treat
field changes as breaking.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildLogRequest(args, since, count)
			if err != nil {
				return err
			}
			return common.Run(cmd, func(ctx *app.Context) error {
				var titles trunklog.TitleResolver
				if gh := ctx.GitHub(); gh != nil {
					titles = gh
				}
				res, err := trunklog.Gather(ctx.Context, ctx.Engine, titles, req)
				if err != nil {
					return err
				}
				if jsonOut {
					return printLogJSON(ctx.Output, res)
				}
				if ctx.Interactive {
					return displayLogPager(res)
				}
				displayLog(ctx.Output, res)
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "Show commits since <ref>, up to the trunk tip (shorthand for <ref>..)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON (stable contract for release tooling)")
	cmd.Flags().IntVarP(&count, "count", "n", 25, "Max commits for the default view (ignored when a range is given)")

	return cmd
}

// buildLogRequest resolves CLI args/flags into a trunklog.Request. A positional
// argument is a "<from>..<to>" range; --since is shorthand for "<ref>..trunk".
// The two are mutually exclusive.
func buildLogRequest(args []string, since string, count int) (trunklog.Request, error) {
	switch {
	case len(args) == 1 && since != "":
		return trunklog.Request{}, fmt.Errorf("cannot combine a range argument with --since")
	case len(args) == 1:
		from, to, ok := strings.Cut(args[0], "..")
		if !ok {
			return trunklog.Request{}, fmt.Errorf("expected a range like <from>..<to>, got %q", args[0])
		}
		if from == "" {
			return trunklog.Request{}, fmt.Errorf("range %q is missing a lower bound", args[0])
		}
		return trunklog.Request{From: from, To: to}, nil
	case since != "":
		// To left empty so the engine anchors the upper bound at the trunk tip.
		return trunklog.Request{From: since}, nil
	default:
		return trunklog.Request{Count: count}, nil
	}
}

// displayLog renders the gathered trunk history as a terminal list, mirroring the
// web "Recently Merged" panel: one line per collapsed commit, with stack-merges
// expanding into their constituent PRs.
func displayLog(out output.Output, res trunklog.Result) {
	rendered := renderLog(res)
	if rendered == "" {
		out.Println(output.Dim("No commits."))
		return
	}
	out.Print(rendered)
	out.Newline()
}

func renderLog(res trunklog.Result) string {
	if len(res.Commits) == 0 {
		return ""
	}

	var b strings.Builder
	for i, c := range res.Commits {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(output.Yellow(shortSHA(c.SHA)))
		b.WriteString("  ")
		b.WriteString(c.Message)

		switch {
		case c.StackSize > 0:
			meta := []string{}
			if c.PRNumber != 0 {
				meta = append(meta, fmt.Sprintf("#%d", c.PRNumber))
			}
			meta = append(meta, fmt.Sprintf("%d PRs", c.StackSize))
			if c.StackScope != "" {
				meta = append(meta, c.StackScope)
			}
			b.WriteString("  ")
			b.WriteString(output.Dim("(" + strings.Join(meta, " · ") + ")"))
			for _, pr := range c.StackPRs {
				b.WriteString("\n    ")
				b.WriteString(output.Cyan(fmt.Sprintf("#%d", pr)))
				if title, ok := c.StackPRTitles[pr]; ok && title != "" {
					b.WriteString(" ")
					b.WriteString(output.Dim(title))
				}
			}
		case c.PRNumber != 0 && !strings.Contains(c.Message, fmt.Sprintf("(#%d)", c.PRNumber)):
			b.WriteString(" ")
			b.WriteString(output.Cyan(fmt.Sprintf("(#%d)", c.PRNumber)))
		}
	}

	return b.String()
}

func displayLogPager(res trunklog.Result) error {
	content := renderLog(res)
	if content == "" {
		content = output.Dim("No commits.")
	}
	model := newLogPagerModel(content, len(res.Commits))
	_, err := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run()
	return err
}

type logPagerModel struct {
	content     string
	commitCount int
	viewport    viewport.Model
	ready       bool
}

func newLogPagerModel(content string, commitCount int) *logPagerModel {
	vp := viewport.New()
	vp.MouseWheelEnabled = true
	return &logPagerModel{
		content:     content,
		commitCount: commitCount,
		viewport:    vp,
	}
}

func (m *logPagerModel) Init() tea.Cmd {
	return nil
}

func (m *logPagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		viewportHeight := max(1, msg.Height-2)
		if !m.ready {
			m.viewport = viewport.New(viewport.WithWidth(msg.Width), viewport.WithHeight(viewportHeight))
			m.viewport.MouseWheelEnabled = true
			m.viewport.SetContent(m.content)
			m.ready = true
		} else {
			m.viewport.SetWidth(msg.Width)
			m.viewport.SetHeight(viewportHeight)
		}
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "g", "home":
			m.viewport.GotoTop()
			return m, nil
		case "G", "end":
			m.viewport.GotoBottom()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *logPagerModel) View() tea.View {
	title := output.Dim(fmt.Sprintf(" Stackit Log | %d commits | q quit, ↑/k ↓/j scroll, f/space page, b back, g/G top/bottom", m.commitCount))
	content := m.content
	if m.ready {
		content = m.viewport.View()
	}
	v := tea.NewView(title + "\n\n" + content)
	v.AltScreen = true
	return v
}

// shortSHA truncates a commit SHA to the conventional 7-character form.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
