package merge

import (
	"context"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

// CIWaiter handles waiting for CI checks to pass on GitHub PRs.
// It extracts the common CI polling logic used by both consolidation and step execution.
type CIWaiter struct {
	client       github.Client
	output       output.Output
	timeout      time.Duration
	pollInterval time.Duration

	// Optional progress reporting
	handler   EventHandler
	stepIndex int
}

// CIWaiterOptions configures a CIWaiter
type CIWaiterOptions struct {
	Client       github.Client
	Output       output.Output
	Timeout      time.Duration // Default: 10 minutes
	PollInterval time.Duration // Default: 15 seconds
}

// NewCIWaiter creates a new CIWaiter with the given options
func NewCIWaiter(opts CIWaiterOptions) *CIWaiter {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultCITimeout
	}

	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultCIPollInterval
	}

	return &CIWaiter{
		client:       opts.Client,
		output:       opts.Output,
		timeout:      timeout,
		pollInterval: pollInterval,
	}
}

// SetProgressHandler sets the handler and step index for progress reporting
func (w *CIWaiter) SetProgressHandler(handler EventHandler, stepIndex int) {
	w.handler = handler
	w.stepIndex = stepIndex
}

// WaitResult contains the result of waiting for CI
type WaitResult struct {
	Passed      bool
	MaxDuration time.Duration // Maximum duration of any check (for estimating future runs)
}

// WaitForChecks waits for CI checks to pass on a branch.
// Returns an error if checks fail, timeout, or context is canceled.
func (w *CIWaiter) WaitForChecks(ctx context.Context, branchName string, prNumber int, expectChecks bool) (*WaitResult, error) {
	if w.client == nil {
		return nil, fmt.Errorf("GitHub client not available")
	}

	startTime := time.Now()
	deadline := startTime.Add(w.timeout)
	lastProgressReport := startTime
	progressInterval := 1 * time.Second

	// If no checks are expected, skip waiting entirely
	if !expectChecks {
		if w.output != nil {
			w.output.Info("   No CI checks configured, skipping wait.")
		}
		return &WaitResult{Passed: true}, nil
	}

	// Give GitHub a moment to register checks
	if w.output != nil {
		w.output.Info("   Waiting for CI checks to register...")
	}
	time.Sleep(CIRegistrationDelay)

	if w.output != nil {
		w.output.Info("   Waiting for CI checks (timeout: %v)...", w.timeout)
	}

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for CI checks on PR #%d after %v", prNumber, w.timeout)
		}

		now := time.Now()
		status, err := w.client.GetPRChecksStatus(ctx, branchName)

		// Report progress to handler if available
		if w.handler != nil && status != nil && now.Sub(lastProgressReport) >= progressInterval {
			elapsed := now.Sub(startTime)
			w.handler.EmitEvent(Event{
				Phase:     PhaseWaiting,
				Type:      EventWaiting,
				StepIndex: w.stepIndex,
				Elapsed:   elapsed,
				Timeout:   w.timeout,
				Checks:    status.Checks,
			})
			lastProgressReport = now
		}

		if err != nil {
			if w.output != nil {
				w.output.Debug("Error checking CI status: %v", err)
			}
		} else if status != nil {
			if !status.Passing {
				return &WaitResult{Passed: false}, fmt.Errorf("CI checks failed on PR #%d", prNumber)
			}

			// If we expect checks but none have appeared yet, treat as pending
			isReallyPending := status.Pending
			if expectChecks && len(status.Checks) == 0 {
				isReallyPending = true
				if w.output != nil && now.Sub(startTime) > 5*time.Second {
					w.output.Debug("No checks found yet for PR #%d, still waiting...", prNumber)
				}
			}

			if !isReallyPending {
				// Calculate max duration for future estimates
				var maxDuration time.Duration
				for _, check := range status.Checks {
					if !check.FinishedAt.IsZero() && !check.StartedAt.IsZero() {
						d := check.FinishedAt.Sub(check.StartedAt)
						if d > maxDuration {
							maxDuration = d
						}
					}
				}

				elapsed := time.Since(startTime)
				if w.output != nil {
					w.output.Info("✅ CI checks passed on PR #%d after %v", prNumber, elapsed.Round(time.Second))
				}
				return &WaitResult{Passed: true, MaxDuration: maxDuration}, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(w.pollInterval):
		}
	}
}

// WaitAndMerge waits for CI checks to pass and then merges the PR.
// This is used for consolidation PRs that should be auto-merged.
func (w *CIWaiter) WaitAndMerge(ctx context.Context, branchName string, pr *github.PullRequestInfo, expectChecks bool, mergeOpts github.MergePROptions) error {
	if w.output != nil {
		w.output.Info("Consolidation PR:")
		w.output.Info("  ◉ %s PR #%d ⏳", branchName, pr.Number)
		w.output.Info("     %s", pr.HTMLURL)
		w.output.Info("     Waiting for CI checks to pass...")
	}

	_, err := w.WaitForChecks(ctx, branchName, pr.Number, expectChecks)
	if err != nil {
		return fmt.Errorf("CI checks failed for consolidation PR: %w", err)
	}

	if w.output != nil {
		w.output.Info("Consolidation PR:")
		w.output.Info("  ◉ %s PR #%d ✓", branchName, pr.Number)
		w.output.Info("     Auto-merging...")
	}

	if err := w.client.MergePullRequest(ctx, branchName, mergeOpts); err != nil {
		return fmt.Errorf("failed to auto-merge consolidation PR #%d: %w", pr.Number, err)
	}

	if w.output != nil {
		w.output.Info("✅ Consolidation PR #%d has been merged automatically!", pr.Number)
		w.output.Info("Consolidation complete:")
		w.output.Info("  ✓ %s (merged)", branchName)
	}

	return nil
}
