package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildEditorCommand(t *testing.T) {
	tests := []struct {
		name    string
		editor  string
		file    string
		want    []string
		wantErr string
	}{
		{
			name:   "binary only",
			editor: "vim",
			file:   "/tmp/message.txt",
			want:   []string{"vim", "/tmp/message.txt"},
		},
		{
			name:   "editor with flags",
			editor: "code --wait --reuse-window",
			file:   "/tmp/message.txt",
			want:   []string{"code", "--wait", "--reuse-window", "/tmp/message.txt"},
		},
		{
			name:   "quoted argument",
			editor: `sed -i ''`,
			file:   "/tmp/message.txt",
			want:   []string{"sed", "-i", "/tmp/message.txt"},
		},
		{
			name:    "empty command",
			editor:  "   ",
			file:    "/tmp/message.txt",
			wantErr: "editor command is empty",
		},
		{
			name:    "unterminated quote",
			editor:  `"code --wait`,
			file:    "/tmp/message.txt",
			wantErr: "parse editor command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := BuildEditorCommand(tt.editor, tt.file)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, cmd.Args)
		})
	}
}
