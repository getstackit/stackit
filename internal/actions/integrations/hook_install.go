// Package integrations provides functionality for integrating Stackit with external tools and hooks.
package integrations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/output"
)

// installHook installs a git hook at .git/hooks/<hookName>, appending the
// marker command to an existing hook file or writing the template if none
// exists yet. displayName is used in log messages (e.g. "Pre-commit").
func installHook(repoRoot, hookName, marker, template, displayName string, out output.Output) error {
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, hookName)

	if err := os.MkdirAll(hooksDir, 0750); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	if _, err := os.Stat(hookPath); err == nil {
		content, err := os.ReadFile(hookPath)
		if err == nil && strings.Contains(string(content), marker) {
			out.Info(fmt.Sprintf("%s hook is already installed.", displayName))
			return nil
		}

		// If it exists but doesn't have our command, append
		f, err := os.OpenFile(hookPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("failed to open existing %s hook: %w", hookName, err)
		}
		defer func() { _ = f.Close() }()

		if _, err := fmt.Fprintf(f, "\n# Added by Stackit\n%s\n", marker); err != nil {
			return fmt.Errorf("failed to append to %s hook: %w", hookName, err)
		}
		out.Info(fmt.Sprintf("Appended Stackit verification to existing %s hook.", strings.ToLower(displayName)))
	} else {
		// #nosec G306 - Git hooks need to be executable
		if err := os.WriteFile(hookPath, []byte(template), 0750); err != nil {
			return fmt.Errorf("failed to write %s hook: %w", hookName, err)
		}
		out.Info(fmt.Sprintf("Installed Stackit %s hook.", strings.ToLower(displayName)))
	}

	return nil
}

// uninstallHook removes the Stackit marker command from .git/hooks/<hookName>,
// deleting the file entirely if only a shebang would remain. displayName is
// used in log messages (e.g. "Pre-commit").
func uninstallHook(out output.Output, repoRoot, hookName, marker, displayName string) error {
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, hookName)

	if _, err := os.Stat(hookPath); os.IsNotExist(err) {
		out.Info(fmt.Sprintf("%s hook is not installed.", displayName))
		return nil
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		return fmt.Errorf("failed to read %s hook: %w", hookName, err)
	}

	lines := strings.Split(string(content), "\n")
	newLines := make([]string, 0, len(lines))
	removed := false

	for i := range lines {
		line := lines[i]
		if strings.Contains(line, marker) {
			removed = true
			// Check if previous line was our comment or if it's the "# Added by Stackit" comment
			if i > 0 && len(newLines) > 0 && (strings.Contains(lines[i-1], "Installed by Stackit") || strings.Contains(lines[i-1], "Added by Stackit")) {
				newLines = newLines[:len(newLines)-1]
			}
			continue
		}
		newLines = append(newLines, line)
	}

	if !removed {
		out.Info(fmt.Sprintf("Stackit verification not found in %s hook.", strings.ToLower(displayName)))
		return nil
	}

	// Check if only shebang is left or if it's empty
	isOnlyShebang := true
	for _, line := range newLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#!") {
			continue
		}
		isOnlyShebang = false
		break
	}

	if isOnlyShebang {
		if err := os.Remove(hookPath); err != nil {
			return fmt.Errorf("failed to remove %s hook: %w", hookName, err)
		}
		out.Info(fmt.Sprintf("Removed Stackit %s hook.", strings.ToLower(displayName)))
	} else {
		// Write back modified content
		newContent := strings.Join(newLines, "\n")
		// Clean up trailing/leading newlines that might have been left behind
		newContent = strings.TrimSpace(newContent) + "\n"
		// #nosec G306 - Git hooks need to be executable
		if err := os.WriteFile(hookPath, []byte(newContent), 0750); err != nil {
			return fmt.Errorf("failed to write %s hook: %w", hookName, err)
		}
		out.Info(fmt.Sprintf("Removed Stackit verification from existing %s hook.", strings.ToLower(displayName)))
	}

	return nil
}
