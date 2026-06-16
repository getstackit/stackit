package actions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShareLabel(t *testing.T) {
	t.Parallel()

	ptr := func(n int) *int { return &n }

	tests := []struct {
		name     string
		branch   string
		prNumber *int
		prTitle  string
		prURL    string
		want     string
	}{
		{
			name:   "no PR falls back to branch name",
			branch: "feature-x",
			want:   "`feature-x` _(no PR)_",
		},
		{
			name:     "PR with url and title renders a Slack link",
			branch:   "feature-x",
			prNumber: ptr(123),
			prTitle:  "Add the thing",
			prURL:    "https://github.com/o/r/pull/123",
			want:     "<https://github.com/o/r/pull/123|#123 Add the thing>",
		},
		{
			name:     "PR with url but no title links the number only",
			branch:   "feature-x",
			prNumber: ptr(7),
			prURL:    "https://github.com/o/r/pull/7",
			want:     "<https://github.com/o/r/pull/7|#7>",
		},
		{
			name:     "PR with title but no url is bare text",
			branch:   "feature-x",
			prNumber: ptr(9),
			prTitle:  "Title here",
			want:     "#9 Title here",
		},
		{
			name:     "PR with neither title nor url is bare number",
			branch:   "feature-x",
			prNumber: ptr(9),
			want:     "#9",
		},
		{
			name:     "angle brackets in title are escaped",
			branch:   "feature-x",
			prNumber: ptr(42),
			prTitle:  "Add <T> generic support",
			prURL:    "https://github.com/o/r/pull/42",
			want:     "<https://github.com/o/r/pull/42|#42 Add &lt;T&gt; generic support>",
		},
		{
			name:     "ampersand in title is escaped",
			branch:   "feature-x",
			prNumber: ptr(43),
			prTitle:  "Fix auth & session handling",
			prURL:    "https://github.com/o/r/pull/43",
			want:     "<https://github.com/o/r/pull/43|#43 Fix auth &amp; session handling>",
		},
		{
			name:     "multiple special chars in title are all escaped",
			branch:   "feature-x",
			prNumber: ptr(44),
			prTitle:  "Support <A> & <B> types",
			prURL:    "https://github.com/o/r/pull/44",
			want:     "<https://github.com/o/r/pull/44|#44 Support &lt;A&gt; &amp; &lt;B&gt; types>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, shareLabel(tt.branch, tt.prNumber, tt.prTitle, tt.prURL))
		})
	}
}
