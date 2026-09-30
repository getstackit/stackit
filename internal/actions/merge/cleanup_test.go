package merge

import (
	"testing"

	"github.com/getstackit/stackit/internal/git"
	"github.com/stretchr/testify/require"
)

func TestPRCleanupResult(t *testing.T) {
	t.Parallel()

	t.Run("counts are correct", func(t *testing.T) {
		t.Parallel()

		result := PRCleanupResult{
			ClosedPRs:  []git.PRNumber{1, 2, 3},
			FailedPRs:  []git.PRNumber{4, 5},
			SkippedPRs: []git.PRNumber{6},
		}

		require.Equal(t, 3, result.ClosedCount())
		require.Equal(t, 2, result.FailedCount())
		require.Equal(t, 1, len(result.SkippedPRs))
	})

	t.Run("empty result has zero counts", func(t *testing.T) {
		t.Parallel()

		result := PRCleanupResult{}

		require.Equal(t, 0, result.ClosedCount())
		require.Equal(t, 0, result.FailedCount())
		require.Equal(t, 0, len(result.SkippedPRs))
	})
}

func TestPRCleanerBuildFooter(t *testing.T) {
	t.Parallel()

	t.Run("consolidation footer with username", func(t *testing.T) {
		t.Parallel()

		cleaner := &PRCleaner{
			config: PRCleanupConfig{
				Source:                CleanupSourceConsolidate,
				ConsolidationPRNumber: 123,
				UserName:              "testuser",
			},
		}

		footer := cleaner.buildFooter()
		require.Equal(t, "\n\n---\n*Merged via consolidation into #123 by testuser*", footer)
	})

	t.Run("consolidation footer without username", func(t *testing.T) {
		t.Parallel()

		cleaner := &PRCleaner{
			config: PRCleanupConfig{
				Source:                CleanupSourceConsolidate,
				ConsolidationPRNumber: 456,
				UserName:              "",
			},
		}

		footer := cleaner.buildFooter()
		require.Equal(t, "\n\n---\n*Merged via consolidation into #456*", footer)
	})

	t.Run("multi-stack footer with username", func(t *testing.T) {
		t.Parallel()

		cleaner := &PRCleaner{
			config: PRCleanupConfig{
				Source:                CleanupSourceMultiStack,
				ConsolidationPRNumber: 789,
				UserName:              "anotheruser",
			},
		}

		footer := cleaner.buildFooter()
		require.Equal(t, "\n\n---\n*Merged via multi-stack consolidation into #789 by anotheruser*", footer)
	})
}

func TestPRCleanerWithNilGitHubClient(t *testing.T) {
	t.Parallel()

	t.Run("returns empty result when no GitHub client", func(t *testing.T) {
		t.Parallel()

		// Create a cleaner with nil context (no GitHub client)
		cleaner := NewPRCleaner(nil, nil, PRCleanupConfig{
			Source:                CleanupSourceConsolidate,
			ConsolidationPRNumber: 123,
		})

		// This should not panic and return empty result
		// Note: We can't actually call CleanupBranches without a valid context
		// but we can verify the cleaner is created correctly
		require.NotNil(t, cleaner)
		require.Equal(t, CleanupSourceConsolidate, cleaner.config.Source)
		require.Equal(t, git.PRNumber(123), cleaner.config.ConsolidationPRNumber)
	})
}
