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

// fakeGitHubClient is only compared by identity; no methods are called, so the
// nil embedded interface panics if that ever changes.
type fakeGitHubClient struct {
	github.Client
}
