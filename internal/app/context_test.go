package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/github"
)

func TestGitHubLazyClientIsSharedAcrossContextCopies(t *testing.T) {
	t.Parallel()

	client := &fakeGitHubClient{}
	initCalls := 0

	ctx := &Context{
		githubLazy: &githubLazy{
			initFunc: func() (github.Client, error) {
				initCalls++
				return client, nil
			},
		},
	}

	ctxCopy := *ctx

	require.Same(t, client, ctx.GitHub())
	require.Same(t, client, ctxCopy.GitHub())
	require.Equal(t, 1, initCalls)
	require.Same(t, client, ctxCopy.GitHubClient)
}

func TestGitHubLazyErrorIsSharedAcrossContextCopies(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("init failed")
	initCalls := 0

	ctx := &Context{
		githubLazy: &githubLazy{
			initFunc: func() (github.Client, error) {
				initCalls++
				return nil, expectedErr
			},
		},
	}

	ctxCopy := *ctx

	require.Nil(t, ctx.GitHub())
	require.ErrorIs(t, ctx.GitHubError(), expectedErr)
	require.Nil(t, ctxCopy.GitHub())
	require.ErrorIs(t, ctxCopy.GitHubError(), expectedErr)
	require.Equal(t, 1, initCalls)
}

func TestRemoteOperationContextAddsDefaultDeadline(t *testing.T) {
	t.Parallel()

	ctx := &Context{Context: context.Background()}
	remoteCtx, cancel := ctx.RemoteOperationContext()
	defer cancel()

	deadline, ok := remoteCtx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(DefaultRemoteOperationTimeout), deadline, time.Second)
}

func TestRemoteOperationContextPreservesExistingDeadline(t *testing.T) {
	t.Parallel()

	parentDeadline := time.Now().Add(30 * time.Second)
	parent, parentCancel := context.WithDeadline(context.Background(), parentDeadline)
	defer parentCancel()

	remoteCtx, cancel := WithRemoteOperationTimeout(parent)
	defer cancel()

	deadline, ok := remoteCtx.Deadline()
	require.True(t, ok)
	require.Equal(t, parentDeadline, deadline)
}

type fakeGitHubClient struct{}

func (f *fakeGitHubClient) CreatePullRequest(_ context.Context, _, _ string, _ github.CreatePROptions) (*github.PullRequestInfo, error) {
	return nil, nil
}

func (f *fakeGitHubClient) UpdatePullRequest(_ context.Context, _, _ string, _ int, _ github.UpdatePROptions) ([]string, error) {
	return nil, nil
}

func (f *fakeGitHubClient) GetPullRequestByBranch(_ context.Context, _, _, _ string) (*github.PullRequestInfo, error) {
	return nil, nil
}

func (f *fakeGitHubClient) GetPullRequest(_ context.Context, _, _ string, _ int) (*github.PullRequestInfo, error) {
	return nil, nil
}

func (f *fakeGitHubClient) MergePullRequest(_ context.Context, _ string, _ github.MergePROptions) error {
	return nil
}

func (f *fakeGitHubClient) GetAllowedMergeMethods(_ context.Context) (*github.MergeMethodSettings, error) {
	return nil, nil
}

func (f *fakeGitHubClient) GetPRChecksStatus(_ context.Context, _ string) (*github.CheckStatus, error) {
	return nil, nil
}

func (f *fakeGitHubClient) BatchGetPRChecksStatus(_ context.Context, _ []string) (map[string]*github.CheckStatus, error) {
	return nil, nil
}

func (f *fakeGitHubClient) BatchGetPRTitles(_ context.Context, _, _ string, _ []int) (map[int]string, error) {
	return nil, nil
}

func (f *fakeGitHubClient) GetOwnerRepo() (string, string) {
	return "", ""
}

func (f *fakeGitHubClient) ClosePullRequest(_ context.Context, _, _ string, _ int) error {
	return nil
}

func (f *fakeGitHubClient) CreatePRComment(_ context.Context, _, _ string, _ int, _ string) (int64, error) {
	return 0, nil
}

func (f *fakeGitHubClient) UpdatePRComment(_ context.Context, _, _ string, _ int64, _ string) error {
	return nil
}

func (f *fakeGitHubClient) DeletePRComment(_ context.Context, _, _ string, _ int64) error {
	return nil
}

func (f *fakeGitHubClient) ListPRComments(_ context.Context, _, _ string, _ int) ([]github.PRComment, error) {
	return nil, nil
}

func (f *fakeGitHubClient) GetCurrentUser(_ context.Context) (string, error) {
	return "", nil
}
