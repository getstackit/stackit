package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewStateCmd(t *testing.T) {
	t.Parallel()

	cmd := newStateCmd()

	require.Equal(t, "state", cmd.Use)
	require.NotEmpty(t, cmd.Short)
	require.NotEmpty(t, cmd.Long)

	jsonFlag := cmd.Flags().Lookup("json")
	require.NotNil(t, jsonFlag, "state should have a --json flag")
}
