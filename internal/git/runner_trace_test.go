package git_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/getstackit/stackit/internal/git"
)

type traceCaptureLogger struct {
	op      string
	cmd     string
	success bool
}

func (l *traceCaptureLogger) Debug(string, ...any) {}
func (l *traceCaptureLogger) Info(string, ...any)  {}

func (l *traceCaptureLogger) Trace(op string, _ int64, success bool, _ error, attrs ...slog.Attr) {
	l.op = op
	l.success = success
	for _, attr := range attrs {
		if attr.Key == "cmd" {
			l.cmd = attr.Value.String()
		}
	}
}

func TestRunnerTracesGitSubprocesses(t *testing.T) {
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(t.TempDir(), logger)

	if _, err := runner.RunGitCommandWithContext(context.Background(), "version"); err != nil {
		t.Fatalf("git version: %v", err)
	}

	if logger.op != "git.version" {
		t.Fatalf("trace op = %q, want git.version", logger.op)
	}
	if logger.cmd != "git version" {
		t.Fatalf("trace cmd = %q, want git version", logger.cmd)
	}
	if !logger.success {
		t.Fatal("trace success = false, want true")
	}
}
