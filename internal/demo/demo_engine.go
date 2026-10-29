package demo

import (
	"context"
	"time"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// Delay constants for simulating real operations
const (
	delayShort  = 150 * time.Millisecond
	delayMedium = 300 * time.Millisecond
	delayLong   = 500 * time.Millisecond
)

// simulateDelay adds a random delay around the base duration
func simulateDelay(base time.Duration) {
	// Use a fixed jitter for demo to avoid weak random number generator warnings
	// and because true randomness isn't critical for demo simulation
	jitter := time.Duration(base.Nanoseconds()%100) * time.Millisecond
	time.Sleep(base + jitter)
}

// NewDemoEngine returns an engine backed by a simulated git runner, along with
// that runner for the GitHub integration to use alongside it.
func NewDemoEngine(ctx context.Context) (engine.Engine, github.GitCommandRunner, error) {
	runner := NewDemoGitRunner()
	eng, err := newDemoEngineWithRunner(ctx, runner)
	if err != nil {
		return nil, nil, err
	}
	return eng, runner, nil
}

func newDemoEngineWithRunner(ctx context.Context, runner git.Runner) (engine.Engine, error) {
	return engine.NewEngine(ctx, engine.Options{
		RepoRoot: "/demo",
		Trunk:    GetDemoTrunk(),
		Git:      runner,
	})
}
