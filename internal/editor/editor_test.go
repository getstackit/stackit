package editor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	t.Run("GIT_EDITOR wins over EDITOR", func(t *testing.T) {
		t.Setenv("GIT_EDITOR", "git-ed")
		t.Setenv("EDITOR", "plain-ed")

		got, err := Resolve()
		require.NoError(t, err)
		require.Equal(t, "git-ed", got)
	})

	t.Run("falls back to EDITOR", func(t *testing.T) {
		t.Setenv("GIT_EDITOR", "")
		t.Setenv("EDITOR", "plain-ed")

		got, err := Resolve()
		require.NoError(t, err)
		require.Equal(t, "plain-ed", got)
	})
}
