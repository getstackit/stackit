// Package tui provides terminal UI utilities.
package tui

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"stackit.dev/stackit/internal/output"
)

// Runner manages async bubbletea program lifecycle with panic recovery.
// It handles signal handling, terminal cleanup, and crash logging.
type Runner struct {
	model   tea.Model
	output  output.Output
	logger  output.Logger
	program *tea.Program
	mu      sync.Mutex
	started bool
	stopped bool
	sigChan chan os.Signal
}

// NewRunner creates a new TUI runner for async program execution.
func NewRunner(model tea.Model, out output.Output, logger output.Logger) *Runner {
	return &Runner{
		model:  model,
		output: out,
		logger: logger,
	}
}

// Start begins running the tea.Program in a background goroutine.
// It sets up signal handling and panic recovery.
func (r *Runner) Start() {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true

	// Quiet console output while TUI is active
	r.output.SetQuiet(true)

	r.program = tea.NewProgram(r.model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))

	// Set up signal handler to ensure terminal is restored on interrupt
	r.sigChan = make(chan os.Signal, 1)
	signal.Notify(r.sigChan, os.Interrupt, syscall.SIGTERM)
	r.mu.Unlock()

	go func() {
		<-r.sigChan
		r.Cleanup()
		signal.Stop(r.sigChan)
	}()

	// Run program in background with panic recovery
	go func() {
		defer func() {
			if p := recover(); p != nil {
				stack := string(debug.Stack())
				r.logger.Error("TUI panic: %v\n%s", p, stack)
				// Print to stderr so user sees something
				fmt.Fprintf(os.Stderr, "\nstackit TUI crashed: %v\n", p)
				r.Cleanup()
			}
		}()

		if _, err := r.program.Run(); err != nil {
			r.logger.Error("TUI error: %v", err)
		}
		r.Cleanup()
	}()
}

// Cleanup ensures the terminal is restored to normal mode.
// This is safe to call multiple times and on nil receivers.
func (r *Runner) Cleanup() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}

	p := r.program
	r.mu.Unlock()

	if p != nil {
		// Quit the program and wait for it to restore terminal state
		p.Quit()
		p.Wait()
	}

	r.mu.Lock()
	r.program = nil
	r.output.SetQuiet(false)
	r.stopped = true
	r.mu.Unlock()
}

// Pause releases the terminal for interactive prompts.
// This is safe to call on nil receivers.
func (r *Runner) Pause() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.program != nil {
		_ = r.program.ReleaseTerminal()
		r.output.SetQuiet(false)
	}
}

// Resume restores the TUI after Pause.
// This is safe to call on nil receivers.
func (r *Runner) Resume() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.program != nil {
		r.output.SetQuiet(true)
		_ = r.program.RestoreTerminal()
	}
}

// Send sends a message to the running program.
// Safe to call on nil receivers or when program is not running (no-op).
func (r *Runner) Send(msg tea.Msg) {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.program
	r.mu.Unlock()

	if p != nil {
		p.Send(msg)
	}
}

// Wait blocks until the program exits.
// Safe to call on nil receivers.
func (r *Runner) Wait() {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.program
	r.mu.Unlock()

	if p != nil {
		p.Wait()
	}
}

// IsRunning returns true if the program is currently running.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started && !r.stopped
}
