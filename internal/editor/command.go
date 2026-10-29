package editor

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	shlex "github.com/anmitsu/go-shlex"
)

// Command parses an editor command and appends the target file path.
func Command(editor, filePath string) (*exec.Cmd, error) {
	editor = strings.TrimSpace(editor)
	if editor == "" {
		return nil, errors.New("editor command is empty")
	}

	parts, err := shlex.Split(editor, true)
	if err != nil {
		return nil, fmt.Errorf("parse editor command: %w", err)
	}
	if len(parts) == 0 {
		return nil, errors.New("editor command is empty")
	}

	args := make([]string, 0, len(parts))
	args = append(args, parts[1:]...)
	args = append(args, filePath)
	return exec.Command(parts[0], args...), nil //nolint:gosec
}

// CleanMessage removes comment lines and surrounding blank lines from a
// message edited in the user's editor.
func CleanMessage(message string) string {
	lines := strings.Split(message, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r\n")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		result = append(result, trimmed)
	}

	// Remove trailing empty lines
	for len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}

	// Remove leading empty lines
	for len(result) > 0 && result[0] == "" {
		result = result[1:]
	}

	return strings.Join(result, "\n")
}
