package utils

import (
	"fmt"
	"os/exec"
	"strings"

	shlex "github.com/anmitsu/go-shlex"
)

// BuildEditorCommand parses an editor command and appends the target file path.
func BuildEditorCommand(editor, filePath string) (*exec.Cmd, error) {
	editor = strings.TrimSpace(editor)
	if editor == "" {
		return nil, fmt.Errorf("editor command is empty")
	}

	parts, err := shlex.Split(editor, true)
	if err != nil {
		return nil, fmt.Errorf("parse editor command: %w", err)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("editor command is empty")
	}

	args := make([]string, 0, len(parts))
	args = append(args, parts[1:]...)
	args = append(args, filePath)
	return exec.Command(parts[0], args...), nil //nolint:gosec
}
