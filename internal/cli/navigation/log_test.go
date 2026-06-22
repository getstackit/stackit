package navigation

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/trunklog"
)

func TestBuildLogRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		since   string
		count   int
		want    trunklog.Request
		wantErr string
	}{
		{name: "default count view", count: 25, want: trunklog.Request{Count: 25}},
		{name: "explicit range", args: []string{"v1..main"}, want: trunklog.Request{From: "v1", To: "main"}},
		{name: "open-ended range to trunk", args: []string{"v1.."}, want: trunklog.Request{From: "v1"}},
		{name: "since shorthand", since: "v1", want: trunklog.Request{From: "v1"}},
		{name: "range and since conflict", args: []string{"v1..main"}, since: "v1", wantErr: "cannot combine"},
		{name: "missing range separator", args: []string{"foo"}, wantErr: "expected a range"},
		{name: "missing lower bound", args: []string{"..main"}, wantErr: "missing a lower bound"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildLogRequest(tt.args, tt.since, tt.count)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
