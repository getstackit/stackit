package term

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadStdin(t *testing.T) {
	// Set to non-interactive mode
	SetInteractive(false)

	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r

	expected := "my commit message"
	go func() {
		_, _ = w.WriteString(expected)
		_ = w.Close()
	}()

	msg, err := ReadStdin()
	require.NoError(t, err)
	require.Equal(t, expected, msg)
}
