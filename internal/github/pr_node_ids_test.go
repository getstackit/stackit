package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPRNodeIDsQuery(t *testing.T) {
	t.Parallel()

	query := buildPRNodeIDsQuery([]int{42, 99})

	require.Contains(t, query, "pr_42: pullRequest(number: 42) { id }")
	require.Contains(t, query, "pr_99: pullRequest(number: 99) { id }")
	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
}

func TestBuildPRNodeIDsQuery_Empty(t *testing.T) {
	t.Parallel()

	query := buildPRNodeIDsQuery(nil)

	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
	require.NotContains(t, query, "pullRequest")
}

func TestParsePRNodeIDsResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"id": "PR_kwAAA42"},
				"pr_99": {"id": "PR_kwAAA99"}
			}
		}
	}`)

	ids, err := parsePRNodeIDsResponse(body, []int{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[int]string{
		42: "PR_kwAAA42",
		99: "PR_kwAAA99",
	}, ids)
}

func TestParsePRNodeIDsResponse_NullEntry(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"id": "PR_kwAAA42"},
				"pr_99": null
			}
		}
	}`)

	ids, err := parsePRNodeIDsResponse(body, []int{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[int]string{42: "PR_kwAAA42"}, ids)
}

func TestParsePRNodeIDsResponse_Empty(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {}
		}
	}`)

	ids, err := parsePRNodeIDsResponse(body, []int{42})
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestParsePRNodeIDsResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parsePRNodeIDsResponse([]byte(`{invalid`), []int{42})
	require.Error(t, err)
}

func TestParsePRNodeIDsResponse_MissingRepository(t *testing.T) {
	t.Parallel()

	body := []byte(`{"data": {}}`)
	_, err := parsePRNodeIDsResponse(body, []int{42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing repository")
}
