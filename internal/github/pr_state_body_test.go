package github

import (
	"testing"

	"github.com/getstackit/stackit/internal/git"
	"github.com/stretchr/testify/require"
)

func TestBuildPRStateBodyQuery(t *testing.T) {
	t.Parallel()

	query := buildPRStateBodyQuery([]git.PRNumber{42, 99})

	require.Contains(t, query, "pr_42: pullRequest(number: 42) { state body }")
	require.Contains(t, query, "pr_99: pullRequest(number: 99) { state body }")
	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
}

func TestParsePRStateBodyResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"state": "OPEN", "body": "hello"},
				"pr_99": {"state": "MERGED", "body": ""}
			}
		}
	}`)

	results, err := parsePRStateBodyResponse(body, []git.PRNumber{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[git.PRNumber]PRStateBody{
		42: {State: "OPEN", Body: "hello"},
		99: {State: "MERGED", Body: ""},
	}, results)
}

func TestParsePRStateBodyResponse_NullEntry(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"state": "OPEN", "body": "hello"},
				"pr_99": null
			}
		}
	}`)

	results, err := parsePRStateBodyResponse(body, []git.PRNumber{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[git.PRNumber]PRStateBody{42: {State: "OPEN", Body: "hello"}}, results)
}

func TestParsePRStateBodyResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parsePRStateBodyResponse([]byte(`{invalid`), []git.PRNumber{42})
	require.Error(t, err)
}

func TestParsePRStateBodyResponse_MissingRepository(t *testing.T) {
	t.Parallel()

	body := []byte(`{"data": {}}`)
	_, err := parsePRStateBodyResponse(body, []git.PRNumber{42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing repository")
}
