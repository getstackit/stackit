package stack

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/foreach"
	"github.com/getstackit/stackit/internal/output"
)

func TestNewForeachUI_ParallelUsesSimpleHandler(t *testing.T) {
	t.Parallel()

	out := output.NewConsoleOutput(&bytes.Buffer{}, false)
	runner, handler := NewForeachUI(out, output.NewNullLogger(), true)

	require.Nil(t, runner)
	_, ok := handler.(*SimpleForeachHandler)
	require.True(t, ok, "parallel mode should always use simple handler")
}

func TestSimpleForeachHandler_ParallelOutputUsesConfiguredWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := output.NewConsoleOutput(&buf, false)
	handler := NewSimpleForeachHandler(out, true)

	handler.OnEvent(foreach.ExecutionStartEvent{
		Branches: []foreach.BranchInfo{
			{Name: "branch-1"},
		},
	})
	handler.OnEvent(foreach.BranchProgressEvent{
		BranchName: "branch-1",
		Status:     foreach.StatusDone,
	})

	require.Equal(t, "Executing in parallel: .", buf.String())
}
