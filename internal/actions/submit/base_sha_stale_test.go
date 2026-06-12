package submit

import (
	"testing"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/stretchr/testify/require"
)

func TestBaseSHAIsStale(t *testing.T) {
	t.Parallel()

	prNumber := 42
	makePrInfo := func(base, baseSHA string) *engine.PrInfo {
		return engine.NewPrInfo(&prNumber, "title", "body", "OPEN", base, "https://example.com/pr/42", false).
			WithBaseSHA(baseSHA)
	}

	tests := []struct {
		name            string
		prInfo          *engine.PrInfo
		info            Info
		trunkName       string
		parentRewritten bool
		expected        bool
	}{
		{
			name:            "nil prInfo",
			prInfo:          nil,
			info:            Info{Base: "parent", BaseSHA: "sha-new"},
			trunkName:       "main",
			parentRewritten: true,
			expected:        false,
		},
		{
			name:            "empty stored BaseSHA",
			prInfo:          makePrInfo("parent", ""),
			info:            Info{Base: "parent", BaseSHA: "sha-new"},
			trunkName:       "main",
			parentRewritten: true,
			expected:        false,
		},
		{
			name:            "fast-forward of parent is not stale",
			prInfo:          makePrInfo("parent", "sha-old"),
			info:            Info{Base: "parent", BaseSHA: "sha-new"},
			trunkName:       "main",
			parentRewritten: false,
			expected:        false,
		},
		{
			name:            "genuine rewrite of parent is stale",
			prInfo:          makePrInfo("parent", "sha-old"),
			info:            Info{Base: "parent", BaseSHA: "sha-new", HeadSHA: "sha-head"},
			trunkName:       "main",
			parentRewritten: true,
			expected:        true,
		},
		{
			name:            "empty child with head at parent tip",
			prInfo:          makePrInfo("parent", "sha-old"),
			info:            Info{Base: "parent", BaseSHA: "sha-new", HeadSHA: "sha-new"},
			trunkName:       "main",
			parentRewritten: true,
			expected:        false,
		},
		{
			name:            "base is trunk",
			prInfo:          makePrInfo("main", "sha-old"),
			info:            Info{Base: "main", BaseSHA: "sha-new"},
			trunkName:       "main",
			parentRewritten: true,
			expected:        false,
		},
		{
			name:            "base branch name changed",
			prInfo:          makePrInfo("old-parent", "sha-old"),
			info:            Info{Base: "new-parent", BaseSHA: "sha-new"},
			trunkName:       "main",
			parentRewritten: true,
			expected:        false,
		},
		{
			name:            "unchanged parent tip is not stale",
			prInfo:          makePrInfo("parent", "sha-same"),
			info:            Info{Base: "parent", BaseSHA: "sha-same"},
			trunkName:       "main",
			parentRewritten: false,
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := baseSHAIsStale(tt.prInfo, tt.info, tt.trunkName, tt.parentRewritten)
			require.Equal(t, tt.expected, result)
		})
	}
}
