package github

import (
	"github.com/getstackit/stackit/internal/git"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPRInfoByBranchQuery(t *testing.T) {
	t.Parallel()

	query, variables := buildPRInfoByBranchQuery(Repo{Owner: "octo", Name: "repo"}, []string{"feature", "jonnii/long/branch-name"})

	require.Contains(t, query, "repository(owner: $owner, name: $repo)")
	require.Contains(t, query, "b0: ref(qualifiedName: $b0)")
	require.Contains(t, query, "b1: ref(qualifiedName: $b1)")
	require.Contains(t, query, "associatedPullRequests(first: 10)")
	require.Contains(t, query, "number title body state url isDraft baseRefName headRefName")

	require.Equal(t, "octo", variables["owner"])
	require.Equal(t, "repo", variables["repo"])
	// Branch names are passed as variables (qualified) so slashes need no escaping.
	require.Equal(t, "refs/heads/feature", variables["b0"])
	require.Equal(t, "refs/heads/jonnii/long/branch-name", variables["b1"])
}

func TestBuildPRInfoByNumberQuery(t *testing.T) {
	t.Parallel()

	query := buildPRNumberQuery([]int{42, 99}, pullRequestInfoFields)

	require.Contains(t, query, "pr_42: pullRequest(number: 42) { number title body state url isDraft baseRefName headRefName }")
	require.Contains(t, query, "pr_99: pullRequest(number: 99) { number title body state url isDraft baseRefName headRefName }")
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

func TestParsePRInfoByNumberResponseAndSupplementMissingBranch(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"pr_42": {"number": 42, "title": "closed PR", "body": "", "state": "CLOSED", "url": "https://gh/pr/42", "isDraft": false, "baseRefName": "main", "headRefName": "deleted-branch"}
			}
		}
	}`)

	byNumber, err := parsePRInfoByNumberResponse(body, []int{42})
	require.NoError(t, err)
	require.Equal(t, git.PRStateClosed, byNumber[42].State)

	infos := map[string]*PullRequestInfo{
		"active-branch": {Number: 7, State: git.PRStateOpen},
	}
	addPRInfoForKnownBranches(infos, map[string]int{
		"deleted-branch": 42,
		"active-branch":  42,
	}, byNumber)

	require.Equal(t, git.PRStateClosed, infos["deleted-branch"].State)
	// An existing ref-based result remains authoritative for active branches.
	require.Equal(t, 7, infos["active-branch"].Number)
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

// TestParsePRInfoByBranchResponse_PrefersLivePR covers the shape GitHub
// actually returns for a resubmitted branch: several associated PRs, with a
// closed one first because orderBy is not honored on this connection.
// Adopting that closed PR made submit try to create a PR that already existed
// and made sync treat a live branch as landed work.
func TestParsePRInfoByBranchResponse_PrefersLivePR(t *testing.T) {
	t.Parallel()

	body := []byte(`{"data":{"repository":{
		"b0":{"associatedPullRequests":{"nodes":[
			{"number":1677,"state":"CLOSED","baseRefName":"old-base","headRefName":"feature"},
			{"number":1680,"state":"OPEN","baseRefName":"new-base","headRefName":"feature"}
		]}}
	}}}`)

	infos, err := parsePRInfoByBranchResponse(body, []string{"feature"})
	require.NoError(t, err)
	require.Equal(t, 1680, infos["feature"].Number)
	require.Equal(t, git.PRStateOpen, infos["feature"].State)
	require.Equal(t, "new-base", infos["feature"].Base)
}

func TestSelectPullRequestInfo(t *testing.T) {
	t.Parallel()

	open1 := &PullRequestInfo{Number: 10, State: git.PRStateOpen}
	open2 := &PullRequestInfo{Number: 12, State: git.PRStateOpen}
	merged := &PullRequestInfo{Number: 20, State: git.PRStateMerged}
	closedOld := &PullRequestInfo{Number: 3, State: git.PRStateClosed}
	closedNew := &PullRequestInfo{Number: 30, State: git.PRStateClosed}

	tests := []struct {
		name  string
		infos []*PullRequestInfo
		want  int
	}{
		{"open beats closed regardless of number", []*PullRequestInfo{closedNew, open1}, 10},
		{"open beats merged regardless of number", []*PullRequestInfo{merged, open1}, 10},
		{"newest open wins among open", []*PullRequestInfo{open1, open2}, 12},
		{"merged beats closed", []*PullRequestInfo{closedNew, merged}, 20},
		{"newest closed when nothing else", []*PullRequestInfo{closedOld, closedNew}, 30},
		{"nil entries ignored", []*PullRequestInfo{nil, open1}, 10},
		{"empty yields nothing", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := selectPullRequestInfo(tt.infos)
			if tt.want == 0 {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tt.want, got.Number)
		})
	}
}
