// Package editor opens the user's configured text editor on a temporary file
// and returns the edited content. It is intentionally separate from internal/tui
// so that callers needing only an editor (not a Bubble Tea UI) don't depend on
// the heavier TUI package.
package editor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/getstackit/stackit/internal/utils"
)

// Open opens the user's preferred editor with the given initial content and
// returns the edited content, or an error.
//
// Editor precedence: GIT_EDITOR > EDITOR > git config core.editor > vi.
// When no editor is set in the environment, it first checks that interactive
// use is allowed (so it doesn't hang in CI / non-interactive contexts).
func Open(initialContent, filenamePattern string) (string, error) {
	// Get editor from environment first. Precedence: GIT_EDITOR > EDITOR.
	editor := os.Getenv("GIT_EDITOR")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}

	// If no editor is explicitly set in the environment, check whether we're
	// allowed to proceed with interactive defaults. This prevents hangs in
	// non-interactive environments (like CI) while allowing tests to provide a
	// non-interactive editor script via the environment.
	if editor == "" {
		if err := utils.CheckInteractiveAllowed(); err != nil {
			return "", err
		}

		// Try to get from git config.
		output, err := exec.Command("git", "config", "--get", "core.editor").Output()
		if err == nil && len(output) > 0 {
			editor = strings.TrimSpace(string(output))
		}
	}

	if editor == "" {
		editor = "vi" // Default to vi.
	}

	// Create temporary file.
	tmpFile, err := os.CreateTemp("", filenamePattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Write initial content.
	if _, err := tmpFile.WriteString(initialContent); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// Open editor.
	cmd, err := utils.BuildEditorCommand(editor, tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("failed to build editor command: %w", err)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	// Read edited content.
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}

	return string(content), nil
}
