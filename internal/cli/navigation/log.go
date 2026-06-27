package navigation

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/stacklog"
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
		Short: "Show your current stack and recent trunk history",
		Long: `Show a stack-aware commit history.

With no arguments it shows the commits in your current stack from where you are
(the branch you're on and its ancestors, grouped by branch with branch and tag
decorations), a clear trunk boundary, then the recent trunk history below it with
consolidated stack-merges collapsed into a single entry (mirrors the web app's
Recently Merged view). On trunk it shows trunk history alone.

Pass a revision range to produce a plain changelog between two refs instead.

Examples:
  stackit log                      # current stack + recent trunk history
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

				// The default (no-range) view is stack-aware: it shows the
				// current stack from HEAD down, a trunk boundary, then trunk
				// history. A range/--since stays a plain changelog.
				var content string
				if req.From == "" {
					stack, err := stacklog.Gather(ctx.Engine)
					if err != nil {
						return err
					}
					content = renderDefaultView(stack, res)
				} else {
					content = renderLog(res, nil, "", "")
				}

				if ctx.Interactive {
					return displayLogPager(content, len(res.Commits))
				}
				displayLog(ctx.Output, content)
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

// displayLog prints already-rendered log content, falling back to a placeholder
// when there is nothing to show.
func displayLog(out output.Output, content string) {
	if content == "" {
		out.Println(output.Dim("No commits."))
		return
	}
	out.Print(content)
	out.Newline()
}

// stackCommitSymbol marks the HEAD commit; otherSymbol marks every other commit
// in the current stack band.
const (
	stackHeadSymbol  = "◉"
	stackOtherSymbol = "◯"
)

// renderDefaultView composes the stack-aware default view: the current stack
// from HEAD down (when not on trunk), a trunk boundary line, and the decorated
// trunk history below it.
func renderDefaultView(stack stacklog.Result, trunk trunklog.Result) string {
	var sections []string
	if band := renderStackBand(stack); band != "" {
		sections = append(sections, band)
	}
	sections = append(sections, renderTrunkDivider(stack))
	if body := renderLog(trunk, stack.Decorations, stack.TrunkName, stack.TrunkTipSHA); body != "" {
		sections = append(sections, body)
	}
	return strings.Join(sections, "\n")
}

// renderStackBand renders the current stack's commits grouped by branch, newest
// branch (HEAD) first. Each branch is a header followed by its commits; the HEAD
// commit is marked distinctly. Returns "" when there is no stack band (on trunk).
func renderStackBand(stack stacklog.Result) string {
	if len(stack.Branches) == 0 {
		return ""
	}
	var b strings.Builder
	for bi, br := range stack.Branches {
		if bi > 0 {
			b.WriteString("\n")
		}
		if br.IsCurrent {
			b.WriteString(output.Green(br.Name))
		} else {
			b.WriteString(output.Cyan(br.Name))
		}
		for ci, c := range br.Commits {
			symbol := output.Dim(stackOtherSymbol)
			if br.IsCurrent && ci == 0 {
				symbol = output.Green(stackHeadSymbol)
			}
			b.WriteString("\n  ")
			b.WriteString(symbol)
			b.WriteString(" ")
			b.WriteString(output.Yellow(shortSHA(c.SHA)))
			b.WriteString("  ")
			b.WriteString(c.Subject)
			// The branch's own head ref is the header above; omit it here, but
			// still surface tags and any other branch pointing at this commit.
			if deco := formatDecorations(stack.Decorations[c.SHA], br.Name); deco != "" {
				b.WriteString(" ")
				b.WriteString(deco)
			}
		}
	}
	return b.String()
}

// renderTrunkDivider draws the boundary between the stack and trunk history,
// labeled with the trunk name, its short tip SHA, and any tags on the tip.
func renderTrunkDivider(stack stacklog.Result) string {
	label := output.Cyan(stack.TrunkName) + " " + output.Yellow(shortSHA(stack.TrunkTipSHA))
	if deco := formatDecorations(stack.Decorations[stack.TrunkTipSHA], stack.TrunkName); deco != "" {
		label += " " + deco
	}
	return output.Dim("──────── ") + label + output.Dim(" ────────")
}

// renderLog renders trunk history, mirroring the web "Recently Merged" panel:
// one line per collapsed commit, stack-merges expanding into constituent PRs.
// When decos is non-nil each commit is annotated with the branches and tags
// pointing at it (excluding excludeBranch and the divider's tip SHA, which are
// already shown elsewhere).
func renderLog(res trunklog.Result, decos map[string][]stacklog.Decoration, excludeBranch, skipSHA string) string {
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

		if c.SHA != skipSHA {
			if deco := formatDecorations(decos[c.SHA], excludeBranch); deco != "" {
				b.WriteString(" ")
				b.WriteString(deco)
			}
		}
	}

	return b.String()
}

// formatDecorations renders git-log-style "(tag: v1.0, feature)" annotations.
// The branch named excludeBranch is omitted (it's already shown as a header or
// divider label); tags are always shown. Returns "" when nothing remains.
func formatDecorations(decos []stacklog.Decoration, excludeBranch string) string {
	if len(decos) == 0 {
		return ""
	}
	parts := make([]string, 0, len(decos))
	for _, d := range decos {
		switch {
		case d.IsTag:
			parts = append(parts, output.Yellow("tag: "+d.Name))
		case d.Name == excludeBranch:
			continue
		default:
			parts = append(parts, output.Cyan(d.Name))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return output.Dim("(") + strings.Join(parts, output.Dim(", ")) + output.Dim(")")
}

func displayLogPager(content string, commitCount int) error {
	if content == "" {
		content = output.Dim("No commits.")
	}
	model := newLogPagerModel(content, commitCount)
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
