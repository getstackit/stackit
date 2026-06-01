package hooks_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/hooks"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/testhelpers"
)

// fakePrompter is a handler.PromptHandler that returns canned answers in
// order. Useful for driving ResolveApproved through its prompt branch in
// tests without setting up a TTY.
type fakePrompter struct {
	answers []bool
	calls   []string
	err     error
}

func (p *fakePrompter) PromptConfirm(message string) (bool, error) {
	p.calls = append(p.calls, message)
	if p.err != nil {
		return false, p.err
	}
	if len(p.answers) == 0 {
		return false, nil
	}
	a := p.answers[0]
	p.answers = p.answers[1:]
	return a, nil
}

func TestResolveApproved_PromptApproveAndPersist(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, nil)
	cfg, err := config.LoadConfig(scene.Dir)
	require.NoError(t, err)

	prompter := &fakePrompter{answers: []bool{true, false}}

	approved, err := hooks.ResolveApproved(hooks.ResolveRequest{
		Phase:    "pre-modify",
		Commands: []string{"scripts/ok.sh", "scripts/declined.sh"},
		Config:   cfg,
		Prompter: prompter,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"scripts/ok.sh"}, approved, "approved hook should be returned; declined should be filtered out")
	require.Len(t, prompter.calls, 2, "prompter should be called once per unapproved hook")

	// Approval is persisted: a fresh Configurer sees the approval without
	// prompting again, and the declined hook is still unapproved.
	cfg2, err := config.LoadConfig(scene.Dir)
	require.NoError(t, err)
	require.True(t, cfg2.IsHookApproved("pre-modify", "scripts/ok.sh"))
	require.False(t, cfg2.IsHookApproved("pre-modify", "scripts/declined.sh"))

	// Re-running should skip the prompt entirely for the approved hook.
	prompter2 := &fakePrompter{answers: []bool{true}}
	approved2, err := hooks.ResolveApproved(hooks.ResolveRequest{
		Phase:    "pre-modify",
		Commands: []string{"scripts/ok.sh"},
		Config:   cfg2,
		Prompter: prompter2,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"scripts/ok.sh"}, approved2)
	require.Empty(t, prompter2.calls, "previously approved hook should not re-prompt")
}

func TestResolveApproved_EmptyCommandsIsZeroCost(t *testing.T) {
	t.Parallel()

	prompter := &fakePrompter{}
	approved, err := hooks.ResolveApproved(hooks.ResolveRequest{
		Phase:    "pre-modify",
		Commands: []string{"   ", "\t"}, // whitespace-only entries
		Config:   nil,                   // intentionally nil — fast path shouldn't read it
		Prompter: nil,
	})
	require.NoError(t, err)
	require.Empty(t, approved)
	require.Empty(t, prompter.calls)
}

func TestResolveApproved_RequiredHookFailsClosedOnPromptFailure(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, nil)
	cfg, err := config.LoadConfig(scene.Dir)
	require.NoError(t, err)

	prompter := &fakePrompter{err: errors.New("interactive disabled")}

	approved, err := hooks.ResolveApproved(hooks.ResolveRequest{
		Phase:    "pre-submit",
		Commands: []string{"scripts/check.sh"},
		Config:   cfg,
		Prompter: prompter,
		Required: true,
	})
	require.Error(t, err)
	require.Empty(t, approved)
	require.Contains(t, err.Error(), "hook \"scripts/check.sh\" at pre-submit is not approved")
	require.Contains(t, err.Error(), "--no-verify")
	require.Len(t, prompter.calls, 1)
}

func TestResolveApproved_RequiredHookFailsClosedOnDecline(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, nil)
	cfg, err := config.LoadConfig(scene.Dir)
	require.NoError(t, err)

	prompter := &fakePrompter{answers: []bool{false}}

	approved, err := hooks.ResolveApproved(hooks.ResolveRequest{
		Phase:    "pre-modify",
		Commands: []string{"scripts/check.sh"},
		Config:   cfg,
		Prompter: prompter,
		Required: true,
	})
	require.Error(t, err)
	require.Empty(t, approved)
	require.Contains(t, err.Error(), "hook \"scripts/check.sh\" at pre-modify is not approved")
	require.Len(t, prompter.calls, 1)
}
