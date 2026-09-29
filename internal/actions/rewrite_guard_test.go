package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
)

type fakeRewriteEngine struct {
	calls       []string
	checkoutErr error
	refErr      error
}

func (f *fakeRewriteEngine) DetachAndResetBranchChanges(_ context.Context, branchName string) error {
	f.calls = append(f.calls, "detach "+branchName)
	return nil
}

func (f *fakeRewriteEngine) ForceCheckoutBranch(_ context.Context, branch engine.Branch) error {
	f.calls = append(f.calls, "checkout "+branch.GetName())
	return f.checkoutErr
}

func (f *fakeRewriteEngine) UpdateBranchRef(_ context.Context, branchName, revision string) error {
	f.calls = append(f.calls, "ref "+branchName+" "+revision)
	return f.refErr
}

func TestRewriteGuard(t *testing.T) {
	t.Parallel()
	branch := engine.NewBranch("feat", nil)
	opErr := errors.New("boom")

	t.Run("released guard does not restore", func(t *testing.T) {
		t.Parallel()
		eng := &fakeRewriteEngine{}
		run := func() (err error) {
			guard, err := DetachForRewrite(t.Context(), eng, branch)
			require.NoError(t, err)
			defer guard.RestoreUnlessReleased(t.Context(), &err)
			guard.Release()
			return nil
		}
		require.NoError(t, run())
		require.Equal(t, []string{"detach feat"}, eng.calls)
	})

	t.Run("failure runs hooks in reverse, resets ref, then checks out", func(t *testing.T) {
		t.Parallel()
		eng := &fakeRewriteEngine{}
		run := func() (err error) {
			guard, err := DetachForRewrite(t.Context(), eng, branch)
			require.NoError(t, err)
			defer guard.RestoreUnlessReleased(t.Context(), &err)
			guard.OnRestore(func() { eng.calls = append(eng.calls, "hook1") })
			guard.OnRestore(func() { eng.calls = append(eng.calls, "hook2") })
			guard.RestoreRefTo("abc123")
			return opErr
		}
		require.ErrorIs(t, run(), opErr)
		require.Equal(t, []string{"detach feat", "hook2", "hook1", "ref feat abc123", "checkout feat"}, eng.calls)
	})

	t.Run("restore failure wraps rather than masks the error", func(t *testing.T) {
		t.Parallel()
		eng := &fakeRewriteEngine{checkoutErr: errors.New("locked")}
		err := NewRewriteGuard(eng, branch).Restore(t.Context(), opErr)
		require.ErrorIs(t, err, opErr)
		require.ErrorContains(t, err, "run 'git checkout feat' to recover")
	})

	t.Run("ref restore failure skips checkout and gives recovery command", func(t *testing.T) {
		t.Parallel()
		eng := &fakeRewriteEngine{refErr: errors.New("cas")}
		guard := NewRewriteGuard(eng, branch)
		guard.RestoreRefTo("abc123")
		err := guard.Restore(t.Context(), opErr)
		require.ErrorIs(t, err, opErr)
		require.ErrorContains(t, err, "run 'git branch -f feat abc123' to recover")
		require.Equal(t, []string{"ref feat abc123"}, eng.calls)
	})
}
