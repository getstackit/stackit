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
	"strconv"
	"sync"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/utils"
)

// loggingDisabled reports whether STACKIT_NO_LOGGING is set to a truthy value.
// Empty / unset / explicitly false-y values (0, false, ...) keep logging on,
// so STACKIT_NO_LOGGING=0 means what it looks like it means. Unrecognized
// non-empty values are treated as truthy for backward compatibility with the
// previous "any non-empty value disables logging" behavior.
func loggingDisabled() bool {
	raw := os.Getenv("STACKIT_NO_LOGGING")
	if raw == "" {
		return false
	}
	if v, err := strconv.ParseBool(raw); err == nil {
		return v
	}
	return true
}

// Context provides access to engine and output for commands
type Context struct {
	context.Context
	Engine       engine.Engine
	Output       output.Output
	Logger       output.Logger
	RepoRoot     string
	GitHubClient github.Client
	Config       config.Configurer // Cached config to avoid repeated loading

	// Lazy GitHub client initialization (pointer so Context is safe to copy)
	githubLazy *githubLazy

	// Global settings from flags
	Interactive bool
	Verify      bool
	Debug       bool
	Quiet       bool

	// Worktree context
	InManagedWorktree bool                 // True if running from a stackit-managed worktree
	WorktreeInfo      *engine.WorktreeInfo // Info about current worktree (nil if not in managed worktree)
}

// githubLazy holds the lazy-initialization state for the GitHub client.
// Stored as a pointer in Context so copying Context is safe (no sync.Once copy).
type githubLazy struct {
	once     sync.Once
	initFunc func() (github.Client, error)
	client   github.Client
	initErr  error
}

// Git returns the git runner from the engine.
// Panics if the Engine is nil, which indicates a programming error.
func (c *Context) Git() git.Runner {
	if c.Engine == nil {
		panic("Context.Git() called with nil Engine - this is a programming error")
	}
	return c.Engine.Git()
}

// GitHub returns the GitHub client, lazily initializing it on first access.
// If GitHubClient was set directly (e.g. in tests), it is returned immediately.
// Returns nil if initialization failed; use GitHubError() to get the reason.
func (c *Context) GitHub() github.Client {
	if c.GitHubClient != nil {
		return c.GitHubClient
	}
	if c.githubLazy != nil {
		c.githubLazy.once.Do(func() {
			client, err := c.githubLazy.initFunc()
			if err != nil {
				c.githubLazy.initErr = err
				return
			}
			c.githubLazy.client = client
		})
		// Cache on this specific Context instance for compatibility with
		// existing direct field reads, while source of truth stays shared.
		if c.githubLazy.client != nil {
			c.GitHubClient = c.githubLazy.client
		}
	}
	return c.GitHubClient
}

// GitHubError returns the error from lazy GitHub client initialization, if any.
func (c *Context) GitHubError() error {
	if c.githubLazy != nil {
		return c.githubLazy.initErr
	}
	return nil
}

// RequireGitHub returns the GitHub client or an error explaining why it's unavailable.
// Use this instead of checking GitHub() == nil manually.
func (c *Context) RequireGitHub() (github.Client, error) {
	client := c.GitHub()
	if client != nil {
		return client, nil
	}
	if err := c.GitHubError(); err != nil {
		return nil, fmt.Errorf("GitHub client not available: %w", err)
	}
	return nil, fmt.Errorf("GitHub client not available — check your GITHUB_TOKEN or run 'gh auth login'")
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

// Worktree returns the worktree registry from the engine.
func (c *Context) Worktree() engine.WorktreeRegistry { return c.Engine }

// GlobalOptions holds settings from global flags
type GlobalOptions struct {
	Interactive              bool
	Verify                   bool
	Debug                    bool
	Quiet                    bool
	Cwd                      string
	EngineLoadMode           *engine.LoadMode
	SkipManagedWorktreeCheck bool
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

// ContextOption is a function that configures a Context
type ContextOption func(*contextOptions)

type contextOptions struct {
	repoRoot string
	global   GlobalOptions
	writer   io.Writer
	logger   output.Logger
}

// WithRepoRoot sets the repository root path
func WithRepoRoot(repoRoot string) ContextOption {
	return func(o *contextOptions) {
		o.repoRoot = repoRoot
	}
}

// WithGlobalOptions sets the global options
func WithGlobalOptions(opts GlobalOptions) ContextOption {
	return func(o *contextOptions) {
		o.global = opts
	}
}

// WithWriter sets the output writer
func WithWriter(writer io.Writer) ContextOption {
	return func(o *contextOptions) {
		o.writer = writer
	}
}

// WithLogger sets the logger
func WithLogger(logger output.Logger) ContextOption {
	return func(o *contextOptions) {
		o.logger = logger
	}
}

// WithInteractive sets the interactive mode
func WithInteractive(interactive bool) ContextOption {
	return func(o *contextOptions) {
		o.global.Interactive = interactive
	}
}

// WithVerify sets the verify mode
func WithVerify(verify bool) ContextOption {
	return func(o *contextOptions) {
		o.global.Verify = verify
	}
}

// WithDebug sets the debug mode
func WithDebug(debug bool) ContextOption {
	return func(o *contextOptions) {
		o.global.Debug = debug
	}
}

// WithQuiet sets the quiet mode
func WithQuiet(quiet bool) ContextOption {
	return func(o *contextOptions) {
		o.global.Quiet = quiet
	}
}

// NewContext creates a new context with the given engine and options
func NewContext(eng engine.Engine, opts ...ContextOption) *Context {
	if eng == nil {
		panic("NewContext called with nil engine")
	}

	options := contextOptions{
		global: GetDefaultGlobalOptions(),
		writer: os.Stdout,
	}

	for _, opt := range opts {
		opt(&options)
	}

	// Update global TUI interactivity
	utils.SetInteractive(options.global.Interactive)

	// Create console output
	consoleOutput := output.NewConsoleOutput(options.writer, options.global.Debug)
	if options.global.Quiet {
		consoleOutput.SetQuiet(true)
	}

	// If no logger provided, create one
	logger := options.logger
	if logger == nil {
		if loggingDisabled() {
			logger = output.NewNullLogger()
		} else {
			logPath := output.GetLogFilePath()
			fileLogger, err := output.NewFileLogger(logPath)
			if err != nil {
				// If file logging fails, use null logger
				logger = output.NewNullLogger()
			} else {
				logger = fileLogger
			}
		}
	}

	// Enable git command debug logging on the engine's git runner
	eng.Git().SetLogger(logger)

	return &Context{
		Context:     context.Background(),
		Engine:      eng,
		Output:      consoleOutput,
		Logger:      logger,
		RepoRoot:    options.repoRoot,
		Interactive: options.global.Interactive,
		Verify:      options.global.Verify,
		Debug:       options.global.Debug,
		Quiet:       options.global.Quiet,
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
		runtimeCtx := NewContext(eng, WithRepoRoot(repoRoot), WithGlobalOptions(opts), WithWriter(writer))
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
	maxConcurrency := cfg.MaxConcurrency()

	// Create file logger first so git commands during engine init are logged
	var logger output.Logger
	if loggingDisabled() {
		logger = output.NewNullLogger()
	} else {
		logPath := output.GetLogFilePath()
		fileLogger, err := output.NewFileLogger(logPath)
		if err != nil {
			logger = output.NewNullLogger()
		} else {
			logger = fileLogger
		}
	}

	// Create git runner with logger for command logging, wrapped with tracing
	gitRunner := git.NewTracingRunner(git.NewRunnerWithPath(repoRoot, logger), logger)

	loadMode := engine.LoadModeShared
	if opts.EngineLoadMode != nil {
		loadMode = *opts.EngineLoadMode
	}


	// Create real engine with configured runner. LoadModeShared skips
	// BatchReadLocalMetadata at bootstrap — local metadata (Frozen, etc.)
	// loads on first accessor call. For commands that never read local-only
	// fields this cuts metadata I/O roughly in half on big repos.
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:          repoRoot,
		Trunk:             trunk,
		MaxUndoStackDepth: maxUndoDepth,
		MaxConcurrency:    maxConcurrency,
		Git:               gitRunner,
		LoadMode:          loadMode,
	})
	if err != nil {
		return nil, err
	}

	runtimeCtx := NewContext(eng, WithRepoRoot(repoRoot), WithGlobalOptions(opts), WithWriter(writer), WithLogger(logger))
	runtimeCtx.Context = ctx
	runtimeCtx.Config = cfg // Store config for reuse

	// Lazy-initialize GitHub client on first access via GitHub()
	runtimeCtx.githubLazy = &githubLazy{
		initFunc: func() (github.Client, error) {
			return github.NewGitHubClient(ctx, gitRunner)
		},
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
	runner := git.NewRunner(nil)
	if opts.Cwd != "" {
		runner = git.NewRunnerWithPath(opts.Cwd, nil)
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
