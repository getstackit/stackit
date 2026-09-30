package github

import (
	"testing"

	"github.com/getstackit/stackit/internal/git"
	"github.com/stretchr/testify/require"
)

func TestBuildPRTitlesQuery(t *testing.T) {
	t.Parallel()

	query := buildPRTitlesQuery([]git.PRNumber{42, 99})

	require.Contains(t, query, "pr_42: pullRequest(number: 42) { title }")
	require.Contains(t, query, "pr_99: pullRequest(number: 99) { title }")
	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
}

func TestBuildPRTitlesQuery_Empty(t *testing.T) {
	t.Parallel()

	query := buildPRTitlesQuery(nil)

	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
	require.NotContains(t, query, "pullRequest")
}

func TestParsePRTitlesResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"title": "feat: add auth"},
				"pr_99": {"title": "fix: resolve race condition"}
			}
		}
	}`)

	titles, err := parsePRTitlesResponse(body, []git.PRNumber{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[git.PRNumber]string{
		42: "feat: add auth",
		99: "fix: resolve race condition",
	}, titles)
}

func TestParsePRTitlesResponse_NullEntry(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"title": "feat: add auth"},
				"pr_99": null
			}
		}
	}`)

	titles, err := parsePRTitlesResponse(body, []git.PRNumber{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[git.PRNumber]string{42: "feat: add auth"}, titles)
}

func TestParsePRTitlesResponse_Empty(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {}
		}
	}`)

	titles, err := parsePRTitlesResponse(body, []git.PRNumber{42})
	require.NoError(t, err)
	require.Empty(t, titles)
}

func TestParsePRTitlesResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parsePRTitlesResponse([]byte(`{invalid`), []git.PRNumber{42})
	require.Error(t, err)
}

func TestParsePRTitlesResponse_MissingRepository(t *testing.T) {
	t.Parallel()

	body := []byte(`{"data": {}}`)
	_, err := parsePRTitlesResponse(body, []git.PRNumber{42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing repository")
}
