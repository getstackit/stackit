package github

import (
	"github.com/getstackit/stackit/internal/git"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPRInfoByBranchQuery(t *testing.T) {
	t.Parallel()

	query, variables := buildPRInfoByBranchQuery("octo", "repo", []string{"feature", "jonnii/long/branch-name"})

	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
	require.Contains(t, query, "b0: ref(qualifiedName: $b0)")
	require.Contains(t, query, "b1: ref(qualifiedName: $b1)")
	require.Contains(t, query, "associatedPullRequests(first: 1, orderBy: {field: CREATED_AT, direction: DESC})")
	require.Contains(t, query, "number title body state url isDraft baseRefName headRefName")

	require.Equal(t, "octo", variables["owner"])
	require.Equal(t, "repo", variables["repo"])
	// Branch names are passed as variables (qualified) so slashes need no escaping.
	require.Equal(t, "refs/heads/feature", variables["b0"])
	require.Equal(t, "refs/heads/jonnii/long/branch-name", variables["b1"])
}

func TestParsePRInfoByBranchResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"b0": {
					"associatedPullRequests": {
						"nodes": [
							{"number": 42, "title": "feat: auth", "body": "do auth", "state": "OPEN", "url": "https://gh/pr/42", "isDraft": true, "baseRefName": "main", "headRefName": "feature"}
						]
					}
				},
				"b1": {
					"associatedPullRequests": {
						"nodes": [
							{"number": 7, "title": "fix: merged", "body": "", "state": "MERGED", "url": "https://gh/pr/7", "isDraft": false, "baseRefName": "feature", "headRefName": "child"}
						]
					}
				}
			}
		}
	}`)

	infos, err := parsePRInfoByBranchResponse(body, []string{"feature", "child"})
	require.NoError(t, err)
	require.Len(t, infos, 2)

	feature := infos["feature"]
	require.NotNil(t, feature)
	require.Equal(t, 42, feature.Number)
	require.Equal(t, "feat: auth", feature.Title)
	require.Equal(t, "do auth", feature.Body)
	require.Equal(t, git.PRStateOpen, feature.State)
	require.Equal(t, "https://gh/pr/42", feature.HTMLURL)
	require.True(t, feature.Draft)
	require.Equal(t, "main", feature.Base)
	require.Equal(t, "feature", feature.Head)

	require.Equal(t, git.PRStateMerged, infos["child"].State)
}

func TestParsePRInfoByBranchResponse_NullRefAndNoPR(t *testing.T) {
	t.Parallel()

	// b0 ref does not exist (null); b1 exists but has no associated PR.
	body := []byte(`{
		"data": {
			"repository": {
				"b0": null,
				"b1": {"associatedPullRequests": {"nodes": []}}
			}
		}
	}`)

	infos, err := parsePRInfoByBranchResponse(body, []string{"gone", "no-pr"})
	require.NoError(t, err)
	require.Empty(t, infos, "branches without an associated PR are skipped")
}

func TestParsePRInfoByBranchResponse_MissingRepository(t *testing.T) {
	t.Parallel()

	body := []byte(`{"data": {}, "errors": [{"message": "Could not resolve to a Repository"}]}`)
	_, err := parsePRInfoByBranchResponse(body, []string{"feature"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Could not resolve to a Repository")
}

func TestParsePRInfoByBranchResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parsePRInfoByBranchResponse([]byte(`{invalid`), []string{"feature"})
	require.Error(t, err)
}
