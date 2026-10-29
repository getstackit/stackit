package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/term"
)

// parsedOutputConfig overrides user config that injects extra text into output
// stackit parses. With log.showSignature=true, `git log` prints signature
// verification lines ahead of each record, so a -z parser reads them into the
// SHA field. The streaming and interactive paths show output to the user and
// keep the user's config.
var parsedOutputConfig = []string{"-c", "log.showSignature=false"}

// outputMode selects where a subprocess's stdout and stderr go.
type outputMode int

const (
	// outputCapture captures both streams for parsing and error reporting.
	outputCapture outputMode = iota
	// outputStream captures both streams and, in an interactive session, also
	// tees them to the terminal so hook output stays visible.
	outputStream
	// outputTerminal attaches the subprocess to the terminal, stdin included.
	// Nothing is captured.
	outputTerminal
)

// command describes one git or gh subprocess.
type command struct {
	name   string // "git" or "gh"
	args   []string
	stdin  string
	env    []string // appended to the inherited environment
	output outputMode
	raw    bool // keep surrounding whitespace in the returned stdout
}

const gitBinary = "git"

func gitCommand(args ...string) command {
	return command{name: gitBinary, args: args}
}

// Run runs git and returns its stdout with surrounding whitespace trimmed.
func (r *runner) Run(ctx context.Context, args ...string) (string, error) {
	return r.exec(ctx, gitCommand(args...))
}

// RunRaw runs git and returns its stdout untouched, for output whose leading
// or trailing whitespace is significant (porcelain status, -z records).
func (r *runner) RunRaw(ctx context.Context, args ...string) (string, error) {
	c := gitCommand(args...)
	c.raw = true
	return r.exec(ctx, c)
}

// RunWithEnv runs git with extra environment variables (e.g. GIT_INDEX_FILE).
func (r *runner) RunWithEnv(ctx context.Context, env []string, args ...string) (string, error) {
	c := gitCommand(args...)
	c.env = env
	return r.exec(ctx, c)
}

// RunGH runs the gh CLI scoped to this runner's repository.
func (r *runner) RunGH(ctx context.Context, args ...string) (string, error) {
	// GH_PROMPT_DISABLED=1 stops gh prompting for authentication or
	// confirmation, which would hang while a TUI owns the terminal.
	return r.exec(ctx, command{name: "gh", args: args, env: []string{"GH_PROMPT_DISABLED=1"}})
}

// RunInteractive runs git attached to the terminal, for commands that open an
// editor or prompt (rebase -i, add -p). It refuses in non-interactive mode.
func (r *runner) RunInteractive(ctx context.Context, args ...string) error {
	if !term.IsInteractive() {
		return fmt.Errorf("interactive git command %q not allowed in non-interactive mode", strings.Join(args, " "))
	}
	c := gitCommand(args...)
	c.output = outputTerminal
	_, err := r.exec(ctx, c)
	return err
}

// runStdin runs git with input on stdin and returns its trimmed stdout.
func (r *runner) runStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	c := gitCommand(args...)
	c.stdin = stdin
	return r.exec(ctx, c)
}

// runStdinRaw runs git with input on stdin and returns its stdout untouched.
func (r *runner) runStdinRaw(ctx context.Context, stdin string, args ...string) (string, error) {
	c := gitCommand(args...)
	c.stdin = stdin
	c.raw = true
	return r.exec(ctx, c)
}

// exec is the single place stackit spawns git or gh. It applies the default
// timeout, the repo working directory, prompt suppression, tracing, and
// CommandError wrapping uniformly across output modes.
func (r *runner) exec(ctx context.Context, c command) (string, error) {
	start := time.Now()
	op := traceOp(c.name, c.args)
	cmdAttr := slog.String("cmd", traceCmd(c.name, c.args))

	// A terminal session runs as long as the user needs (an editor, a patch
	// prompt), so it gets no default deadline.
	if _, ok := ctx.Deadline(); !ok && c.output != outputTerminal {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCommandTimeout)
		defer cancel()
	}

	switch c.output {
	case outputStream:
		r.debugLog("%s %s (streaming)", c.name, strings.Join(c.args, " "))
	case outputTerminal:
		r.debugLog("%s %s (interactive)", c.name, strings.Join(c.args, " "))
	default:
		r.debugLog("%s %s", c.name, strings.Join(c.args, " "))
	}

	args := c.args
	if c.name == gitBinary && c.output == outputCapture {
		args = slices.Concat(parsedOutputConfig, args)
	}
	cmd := exec.CommandContext(ctx, c.name, args...)
	cmd.WaitDelay = CommandWaitDelay
	// An empty Dir runs in the current working directory, which is what a
	// runner with no repo root wants.
	cmd.Dir = r.getRepoRoot()

	if c.output == outputCapture {
		// GIT_TERMINAL_PROMPT=0 stops git reading credentials or confirmations
		// from the terminal, which would hang while a TUI has it in raw mode.
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	}
	if len(c.env) > 0 {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, c.env...)
	}

	var stdout, stderr bytes.Buffer
	switch c.output {
	case outputTerminal:
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	case outputStream:
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if term.IsInteractive() {
			cmd.Stdout = io.MultiWriter(os.Stdout, &stdout)
			cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
		}
	default:
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}
	if c.stdin != "" {
		cmd.Stdin = strings.NewReader(c.stdin)
	}

	if err := cmd.Run(); err != nil {
		// Report a timeout as the timeout, not as the "signal: killed" exit it
		// caused, so callers can match context.DeadlineExceeded.
		if ctxErr := ctx.Err(); errors.Is(ctxErr, context.DeadlineExceeded) {
			err = ctxErr
		}
		cmdErr := NewCommandError(c.name, c.args, stdout.String(), stderr.String(), err)
		r.traceLog(op, time.Since(start), false, cmdErr, cmdAttr)
		return "", cmdErr
	}

	r.traceLog(op, time.Since(start), true, nil, cmdAttr)
	if c.raw {
		return stdout.String(), nil
	}
	return strings.TrimSpace(stdout.String()), nil
}
