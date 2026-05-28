package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeOrgChecker struct {
	members map[string]bool
	err     error
	calls   int
}

func (f *fakeOrgChecker) IsMember(_ context.Context, _, login, _ string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.members[login], nil
}

func TestNewAllowlist_RequiresAtLeastOneRule(t *testing.T) {
	t.Parallel()

	_, err := NewAllowlist(AllowlistConfig{})
	require.Error(t, err)
}

func TestAllowlist_UserExactMatchCaseInsensitive(t *testing.T) {
	t.Parallel()

	a, err := NewAllowlist(AllowlistConfig{Users: []string{"Jonnii", "  someone  ", ""}})
	require.NoError(t, err)
	a.SetOrgChecker(&fakeOrgChecker{}) // no network

	require.NoError(t, a.Check(context.Background(), "jonnii", ""))
	require.NoError(t, a.Check(context.Background(), "SOMEONE", ""))
	require.ErrorIs(t, a.Check(context.Background(), "stranger", ""), ErrDenied)
}

func TestAllowlist_OrgMembershipPath(t *testing.T) {
	t.Parallel()

	checker := &fakeOrgChecker{members: map[string]bool{"jonnii": true}}
	a, err := NewAllowlist(AllowlistConfig{Org: "getstackit"})
	require.NoError(t, err)
	a.SetOrgChecker(checker)

	require.NoError(t, a.Check(context.Background(), "jonnii", "tok"))
	require.Equal(t, 1, checker.calls)

	require.ErrorIs(t, a.Check(context.Background(), "stranger", "tok"), ErrDenied)
}

func TestAllowlist_UserMatchSkipsOrgCheck(t *testing.T) {
	t.Parallel()

	checker := &fakeOrgChecker{}
	a, err := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}, Org: "getstackit"})
	require.NoError(t, err)
	a.SetOrgChecker(checker)

	require.NoError(t, a.Check(context.Background(), "jonnii", "tok"))
	require.Equal(t, 0, checker.calls, "user-list hit must not consume an API call")
}

func TestAllowlist_OrgCheckerError(t *testing.T) {
	t.Parallel()

	checker := &fakeOrgChecker{err: errors.New("boom")}
	a, err := NewAllowlist(AllowlistConfig{Org: "getstackit"})
	require.NoError(t, err)
	a.SetOrgChecker(checker)

	err = a.Check(context.Background(), "jonnii", "tok")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDenied, "infrastructure error must surface, not silently deny")
}

func TestAllowlist_EmptyLoginDenied(t *testing.T) {
	t.Parallel()

	a, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	a.SetOrgChecker(&fakeOrgChecker{})

	require.ErrorIs(t, a.Check(context.Background(), "", "tok"), ErrDenied)
}
