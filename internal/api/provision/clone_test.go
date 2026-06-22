package provision_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/getstackit/stackit/internal/api/provision"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/stretchr/testify/require"
)

func TestEnsureClone(t *testing.T) {
	t.Parallel()
	// A local source repo stands in for the remote; cloning from a path
	// exercises the same code path without a network or token.
	src := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	dest := filepath.Join(t.TempDir(), "octo", "widget")

	err := provision.EnsureClone(context.Background(), provision.CloneParams{
		CloneURL: src.Dir,
		Dest:     dest,
	})
	require.NoError(t, err)
	require.DirExists(t, filepath.Join(dest, ".git"))
}

func TestEnsureCloneIdempotent(t *testing.T) {
	t.Parallel()
	src := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	dest := filepath.Join(t.TempDir(), "octo", "widget")

	require.NoError(t, provision.EnsureClone(context.Background(), provision.CloneParams{
		CloneURL: src.Dir,
		Dest:     dest,
	}))

	// Second call with a bogus URL must be a no-op: an existing checkout is
	// detected before any clone is attempted.
	require.NoError(t, provision.EnsureClone(context.Background(), provision.CloneParams{
		CloneURL: "https://github.com/should/not-be-touched.git",
		Dest:     dest,
	}))
}
