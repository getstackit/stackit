package hooks_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/getstackit/stackit/internal/hooks"
	"github.com/stretchr/testify/require"
)

func TestRun_Success(t *testing.T) {
	t.Parallel()
	err := hooks.Run(context.Background(), "true", t.TempDir(), nil, time.Second)
	require.NoError(t, err)
}

func TestRun_NonZeroExit(t *testing.T) {
	t.Parallel()
	err := hooks.Run(context.Background(), "exit 7", t.TempDir(), nil, time.Second)
	require.Error(t, err)
}

func TestRun_Timeout(t *testing.T) {
	t.Parallel()
	err := hooks.Run(context.Background(), "sleep 5", t.TempDir(), nil, 50*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out")
}

func TestRun_EnvInjection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := hooks.Run(
		context.Background(),
		`test "$STACKIT_TEST_VAR" = "hello"`,
		dir,
		[]string{"STACKIT_TEST_VAR=hello"},
		time.Second,
	)
	require.NoError(t, err)
}

func TestRun_WorkingDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := hooks.Run(
		context.Background(),
		// `pwd -P` resolves symlinks (macOS uses /private/var symlink for /tmp).
		`test "$(pwd -P)" = "$(cd `+shellQuote(dir)+` && pwd -P)"`,
		dir,
		nil,
		time.Second,
	)
	require.NoError(t, err)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
