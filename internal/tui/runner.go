// Package tui provides terminal UI utilities.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/core"
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
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewRunner creates a new TUI runner for async program execution.
// Use NewRunnerWithContext if you need cancellation support.
func NewRunner(model tea.Model, out output.Output, logger output.Logger) *Runner {
	return &Runner{
		model:  model,
		output: out,
		logger: logger,
	}
}

// NewRunnerWithContext creates a new TUI runner with context support.
// The context will be canceled when Cleanup is called, allowing
// background operations to be properly terminated.
func NewRunnerWithContext(ctx context.Context, model tea.Model, out output.Output, logger output.Logger) *Runner {
	ctx, cancel := context.WithCancel(ctx)
	return &Runner{
		model:  model,
		output: out,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Context returns the runner's context for use in background operations.
// If the runner was created without a context (using NewRunner), this returns
// context.Background().
func (r *Runner) Context() context.Context {
	if r == nil || r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// Start begins running the tea.Program in a background goroutine.
// It sets up signal handling and panic recovery.
// If the model implements ReadySignaler, Start waits for the model to signal
// readiness before returning, preventing race conditions with Send().
func (r *Runner) Start() {
	startTime := time.Now()
	r.logger.Debug("tui.Runner.Start entering")

	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		r.logger.Debug("tui.Runner.Start already started, returning")
		return
	}
	r.started = true

	// Quiet console output while TUI is active
	r.output.SetQuiet(true)

	// Set up ready channel if model supports it
	var readyChan chan struct{}
	if signaler, ok := r.model.(core.ReadySignaler); ok {
		readyChan = make(chan struct{})
		signaler.SetReadyChan(readyChan)
		r.logger.Debug("tui.Runner.Start ready channel configured")
	}

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
				// Log to dedicated panic file for easy debugging
				output.LogPanic(p, stack)
				r.logger.Error("TUI panic: %v\n%s", p, stack)
				// Print to stderr so user sees something
				fmt.Fprintf(os.Stderr, "\nstackit TUI crashed: %v\nDetails logged to: %s\n", p, output.GetPanicLogPath())
				r.Cleanup()
			}
		}()

		if _, err := r.program.Run(); err != nil {
			r.logger.Error("TUI error: %v", err)
		}
		r.Cleanup()
	}()

	// Wait for ready signal if model supports it
	// This prevents Send() calls from blocking on an uninitialized event loop
	if readyChan != nil {
		select {
		case <-readyChan:
			r.logger.Debug("tui.Runner.Start ready signal received durationMs=%v", time.Since(startTime).Milliseconds())
		case <-time.After(2 * time.Second):
			r.logger.Warn("tui.Runner.Start ready timeout, proceeding anyway durationMs=%v", time.Since(startTime).Milliseconds())
		}
	}

	r.logger.Debug("tui.Runner.Start completed durationMs=%v", time.Since(startTime).Milliseconds())
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
	// Cancel context to signal background operations to stop
	if r.cancel != nil {
		r.cancel()
	}
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
// Safe to call on nil receivers.
func (r *Runner) IsRunning() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started && !r.stopped
}

// IsHealthy returns true if the TUI is running and responsive.
// This is a more comprehensive check than IsRunning, verifying the program is not nil.
func (r *Runner) IsHealthy() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started && !r.stopped && r.program != nil
}

// SendWithTimeout sends a message with a timeout.
// Returns error if the send doesn't complete within the timeout or if the
// runner's context is canceled (e.g., during Cleanup).
// This is useful for detecting hangs in the TUI event loop.
func (r *Runner) SendWithTimeout(msg tea.Msg, timeout time.Duration) error {
	if r == nil {
		return nil
	}

	// Check if already stopped
	ctx := r.Context()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	done := make(chan struct{})
	go func() {
		r.Send(msg)
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Runner was cleaned up, don't log as error
		return ctx.Err()
	case <-time.After(timeout):
		r.logger.Error("TUI send timed out msgType=%v timeout=%v", reflect.TypeOf(msg).String(), timeout)
		return fmt.Errorf("send timed out after %v", timeout)
	}
}

// MustSend sends a message and logs error if timeout occurs.
// Uses a default 5-second timeout. This is the recommended way to send messages
// when you want hang detection without blocking indefinitely.
func (r *Runner) MustSend(msg tea.Msg) {
	if err := r.SendWithTimeout(msg, 5*time.Second); err != nil {
		r.logger.Error("MustSend failed error=%v", err)
	}
}

// PanicError is sent when a tea.Cmd panics during execution.
// Models can handle this to show an error message or recover gracefully.
type PanicError struct {
	Source string // Name of the operation that panicked
	Err    error  // The panic value wrapped as an error
	Stack  string // Stack trace at the time of panic
}

func (p PanicError) Error() string {
	return fmt.Sprintf("%s panicked: %v", p.Source, p.Err)
}

// SafeCmd wraps a tea.Cmd with panic recovery.
// If the command panics, it logs the error and returns a PanicError message.
// This is useful for commands that perform IO or call external APIs.
func SafeCmd(name string, logger output.Logger, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		defer func() {
			if p := recover(); p != nil {
				stack := string(debug.Stack())
				err := fmt.Errorf("%v", p)
				// Log to dedicated panic file for easy debugging
				output.LogPanic(p, stack)
				if logger != nil {
					logger.Error("%s panicked: %v\n%s", name, p, stack)
				}
				// We can't return from inside defer, so we re-panic with a wrapped error
				// that will be caught by the outer recovery
				panic(PanicError{Source: name, Err: err, Stack: stack})
			}
		}()
		return cmd()
	}
}

// SafeCmdFunc wraps a function that returns tea.Msg with panic recovery.
// If the function panics, it logs the error and returns a PanicError message.
func SafeCmdFunc(name string, logger output.Logger, fn func() tea.Msg) tea.Cmd {
	return func() (result tea.Msg) {
		defer func() {
			if p := recover(); p != nil {
				stack := string(debug.Stack())
				err := fmt.Errorf("%v", p)
				// Log to dedicated panic file for easy debugging
				output.LogPanic(p, stack)
				if logger != nil {
					logger.Error("%s panicked: %v\n%s", name, p, stack)
				}
				result = PanicError{Source: name, Err: err, Stack: stack}
			}
		}()
		return fn()
	}
}
