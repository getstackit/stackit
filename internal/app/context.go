// Package app provides the execution context for stackit commands.
//
// It encapsulates shared dependencies and configuration needed by actions,
// such as the engine instance, logger, and repository root path. This avoids
// passing multiple parameters throughout the application.
package app

import (
	"context"
	"fmt"
	"io"
	"os"

	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/utils"
)

// Context provides access to engine and output for commands
type Context struct {
	context.Context
	Engine       engine.Engine
	Splog        *tui.Splog
	RepoRoot     string
	GitHubClient github.Client

	// Global settings from flags
	Interactive bool
	Verify      bool
	Debug       bool
	Quiet       bool
}

// Git returns the git runner from the engine.
// Panics if the Engine is nil, which indicates a programming error.
func (c *Context) Git() git.Runner {
	if c.Engine == nil {
		panic("Context.Git() called with nil Engine - this is a programming error")
	}
	return c.Engine.Git()
}

// Navigator returns the stack navigator from the engine.
func (c *Context) Navigator() engine.StackNavigator { return c.Engine }

// Status returns the branch status provider from the engine.
func (c *Context) Status() engine.BranchStatus { return c.Engine }

// Info returns the branch info provider from the engine.
func (c *Context) Info() engine.BranchInfo { return c.Engine }

// Reader returns the branch reader from the engine.
func (c *Context) Reader() engine.BranchReader { return c.Engine }

// Writer returns the branch writer from the engine.
func (c *Context) Writer() engine.BranchWriter { return c.Engine }

// Sync returns the sync manager from the engine.
func (c *Context) Sync() engine.SyncManager { return c.Engine }

// PR returns the PR manager from the engine.
func (c *Context) PR() engine.PRManager { return c.Engine }

// History returns the history rewriter from the engine.
func (c *Context) History() engine.StackRewriter { return c.Engine }

// Absorb returns the absorb manager from the engine.
func (c *Context) Absorb() engine.Absorber { return c.Engine }

// Undo returns the undo manager from the engine.
func (c *Context) Undo() engine.UndoManager { return c.Engine }

// RemoteMetadata returns the remote metadata manager from the engine.
func (c *Context) RemoteMetadata() engine.RemoteMetadataManager { return c.Engine }

// GlobalOptions holds settings from global flags
type GlobalOptions struct {
	Interactive bool
	Verify      bool
	Debug       bool
	Quiet       bool
	Cwd         string
}

// GetDefaultGlobalOptions returns default options
func GetDefaultGlobalOptions() GlobalOptions {
	return GlobalOptions{
		Interactive: true,
		Verify:      true,
		Debug:       os.Getenv("DEBUG") != "",
		Quiet:       false,
		Cwd:         "",
	}
}

// NewContext creates a new context with the given engine
func NewContext(eng engine.Engine) *Context {
	return NewContextWithOptions(eng, GetDefaultGlobalOptions())
}

// NewContextWithOptions creates a new context with the given engine and options
func NewContextWithOptions(eng engine.Engine, opts GlobalOptions) *Context {
	return NewContextWithRepoRootOptionsAndWriter(eng, "", opts, tui.DefaultConsoleWriter)
}

// NewContextWithRepoRoot creates a new context with the given engine and repo root
func NewContextWithRepoRoot(eng engine.Engine, repoRoot string) *Context {
	return NewContextWithRepoRootAndOptions(eng, repoRoot, GetDefaultGlobalOptions())
}

// NewContextWithRepoRootAndOptions creates a new context with the given engine, repo root and options
func NewContextWithRepoRootAndOptions(eng engine.Engine, repoRoot string, opts GlobalOptions) *Context {
	return NewContextWithRepoRootOptionsAndWriter(eng, repoRoot, opts, os.Stdout)
}

// NewContextWithRepoRootOptionsAndWriter creates a new context with the given engine, repo root, options and writer
func NewContextWithRepoRootOptionsAndWriter(eng engine.Engine, repoRoot string, opts GlobalOptions, writer io.Writer) *Context {
	if eng == nil {
		panic("NewContextWithRepoRootAndOptions called with nil engine")
	}

	var splog *tui.Splog
	var err error

	// Update global TUI interactivity
	utils.SetInteractive(opts.Interactive)

	// Skip file logging when STACKIT_NO_LOGGING is set (e.g., during tests or CI)
	if os.Getenv("STACKIT_NO_LOGGING") != "" {
		splog = tui.NewSplogToWriter(writer) // Console-only logging
	} else {
		logPath := tui.GetLogFilePath()
		splog, err = tui.NewSplogWithFlagsAndWriter(logPath, opts.Debug, opts.Quiet, writer)
		if err != nil {
			// If file logging fails, fall back to console-only
			splog = tui.NewSplogToWriter(writer)
		}
	}

	return &Context{
		Context:     context.Background(),
		Engine:      eng,
		Splog:       splog,
		RepoRoot:    repoRoot,
		Interactive: opts.Interactive,
		Verify:      opts.Verify,
		Debug:       opts.Debug,
		Quiet:       opts.Quiet,
	}
}

// DemoEngineFactory is a function that creates a demo engine.
// This is set by the demo package to avoid circular imports.
var DemoEngineFactory func() engine.Engine

// DemoGitHubClientFactory is a function that creates a demo GitHub client.
// This is set by the demo package to avoid circular imports.
var DemoGitHubClientFactory func() github.Client

// NewContextAuto creates a context automatically based on the environment.
// In demo mode, it creates a demo engine. Otherwise, it creates a real engine
// using the provided repoRoot.
func NewContextAuto(ctx context.Context, repoRoot string, opts GlobalOptions) (*Context, error) {
	return NewContextAutoWithWriter(ctx, repoRoot, opts, os.Stdout)
}

// NewContextAutoWithWriter is like NewContextAuto but allows specifying the output writer.
func NewContextAutoWithWriter(ctx context.Context, repoRoot string, opts GlobalOptions, writer io.Writer) (*Context, error) {
	if utils.IsDemoMode() && DemoEngineFactory != nil {
		eng := DemoEngineFactory()
		runtimeCtx := NewContextWithRepoRootOptionsAndWriter(eng, repoRoot, opts, writer)
		runtimeCtx.Context = ctx
		if DemoGitHubClientFactory != nil {
			runtimeCtx.GitHubClient = DemoGitHubClientFactory()
		}
		return runtimeCtx, nil
	}

	// Read config and create engine options
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	trunk := cfg.Trunk()
	maxUndoDepth := cfg.UndoStackDepth()

	// Create real engine
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:          repoRoot,
		Trunk:             trunk,
		MaxUndoStackDepth: maxUndoDepth,
		Writer:            writer,
	})
	if err != nil {
		return nil, err
	}

	runtimeCtx := NewContextWithRepoRootOptionsAndWriter(eng, repoRoot, opts, writer)
	runtimeCtx.Context = ctx

	// Try to create real GitHub client (may fail if no token)
	ghClient, err := github.NewGitHubClient(ctx, runtimeCtx.Git())
	if err == nil {
		runtimeCtx.GitHubClient = ghClient
	}

	return runtimeCtx, nil
}

// GetContext returns the appropriate context (demo or real) based on the environment.
// This handles git initialization and config checks for real mode.
func GetContext(ctx context.Context, opts GlobalOptions) (*Context, error) {
	return GetContextWithWriter(ctx, opts, os.Stdout)
}

// GetContextWithWriter is like GetContext but allows specifying the output writer.
func GetContextWithWriter(ctx context.Context, opts GlobalOptions, writer io.Writer) (*Context, error) {
	// Check for demo mode first
	if utils.IsDemoMode() {
		return NewContextAutoWithWriter(ctx, "", opts, writer)
	}

	// Get repo root using a runner
	runner := git.NewRunner()
	if opts.Cwd != "" {
		runner = git.NewRunnerWithPath(opts.Cwd)
	}
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}

	// Check if initialized
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if !cfg.IsInitialized() {
		return nil, fmt.Errorf("stackit not initialized. Run 'stackit init' first")
	}

	return NewContextAutoWithWriter(ctx, repoRoot, opts, writer)
}
