package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadFromStdin(t *testing.T) {
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r

	expected := "my commit message"
	go func() {
		_, _ = w.Write([]byte(expected))
		_ = w.Close()
	}()

	msg, err := ReadFromStdin()
	require.NoError(t, err)
	require.Equal(t, expected, msg)
}
