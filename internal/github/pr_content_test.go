package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPRContentQuery(t *testing.T) {
	t.Parallel()

	query := buildPRContentQuery([]int{42, 99})

	require.Contains(t, query, "pr_42: pullRequest(number: 42) { title body }")
	require.Contains(t, query, "pr_99: pullRequest(number: 99) { title body }")
	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
}

func TestParsePRContentResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"title": "feat: auth", "body": "body 42"},
				"pr_99": {"title": "fix: race", "body": ""}
			}
		}
	}`)

	content, err := parsePRContentResponse(body, []int{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[int]PRContent{
		42: {Title: "feat: auth", Body: "body 42"},
		99: {Title: "fix: race", Body: ""},
	}, content)
}

func TestParsePRContentResponse_NullEntry(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"title": "feat: auth", "body": "body 42"},
				"pr_99": null
			}
		}
	}`)

	content, err := parsePRContentResponse(body, []int{42, 99})
	require.NoError(t, err)
	require.Equal(t, map[int]PRContent{42: {Title: "feat: auth", Body: "body 42"}}, content)
}

func TestParsePRContentResponse_MissingRepository(t *testing.T) {
	t.Parallel()

	body := []byte(`{"data": {}, "errors": [{"message": "Bad credentials"}]}`)
	_, err := parsePRContentResponse(body, []int{42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Bad credentials")
}

func TestParsePRContentResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parsePRContentResponse([]byte(`{bad`), []int{42})
	require.Error(t, err)
}
