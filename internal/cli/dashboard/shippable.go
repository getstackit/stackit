// Package dashboard provides the interactive shippable work TUI.
package dashboard

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/tui"
)

// RunShippable starts the shippable work dashboard.
func RunShippable(ctx *app.Context, opts ShippableOptions) error {
	if !tui.IsTTY() {
		return fmt.Errorf("dashboard requires an interactive terminal")
	}

	// Load config for CI settings
	cfg, err := config.LoadConfig(ctx.RepoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	m := newShippableModel(ctx, cfg, opts)

	// Note: The dashboard uses alt-screen mode which is not directly supported
	// by tui.Runner. We use tea.NewProgram directly with appropriate options.
	// The model embeds core.BaseModel for ready signaling and common handling.
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	_, err = p.Run()
	return err
}

// RunCompanion starts the live local-state companion panel.
func RunCompanion(ctx *app.Context, opts CompanionOptions) error {
	if !tui.IsTTY() {
		return fmt.Errorf("dashboard requires an interactive terminal")
	}

	cfg, err := config.LoadConfig(ctx.RepoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	m := newCompanionModel(ctx, cfg, opts)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	m.startWatcher(p)
	_, err = p.Run()
	m.stopWatcher()
	return err
}
