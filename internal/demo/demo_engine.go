package demo

import (
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
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

func init() {
	// Register the demo engine factory with runtime package
	app.DemoEngineFactory = func() engine.Engine {
		eng, _ := NewDemoEngine()
		return eng
	}
}

// NewDemoEngine creates a new demo engine using the standard engine implementation
// but with a simulated Git runner.
func NewDemoEngine() (engine.Engine, error) {
	opts := engine.Options{
		RepoRoot: "/demo",
		Trunk:    GetDemoTrunk(),
		Git:      NewDemoGitRunner(),
	}

	return engine.NewEngine(opts)
}
