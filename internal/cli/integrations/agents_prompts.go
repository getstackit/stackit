package integrations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	stackiterrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui"
)

var (
	promptSelect                 = tui.PromptSelect
	promptMultiSelectWithDefault = tui.PromptMultiSelectWithDefaults
	promptConfirm                = tui.PromptConfirm
)

func selectInstallTargets(baseDir string, formats []string) ([]agentInstallTarget, error) {
	if len(formats) > 0 {
		parsed, err := parseAgentSkillFormats(formats)
		if err != nil {
			return nil, err
		}
		return targetsForFormats(parsed), nil
	}

	hasClaudeDir := dirExists(baseDir + "/.claude")
	hasCodexDir := dirExists(baseDir + "/.codex")
	preSelected := []bool{
		hasClaudeDir,
		hasCodexDir,
	}
	if !hasClaudeDir && !hasCodexDir {
		preSelected = []bool{true, true}
	}

	selected, err := promptMultiSelectWithDefault(
		"Which skill format(s) would you like to install?",
		[]string{
			claudeFormatLabel,
			codexFormatLabel,
		},
		preSelected,
	)
	if errors.Is(err, tui.ErrInteractiveDisabled) {
		fallback := make([]agentSkillFormat, 0, 2)
		if hasClaudeDir {
			fallback = append(fallback, agentSkillFormatClaude)
		}
		if hasCodexDir {
			fallback = append(fallback, agentSkillFormatCodex)
		}
		if len(fallback) > 0 {
			return targetsForFormats(fallback), nil
		}
		return nil, fmt.Errorf("format selection requires interactive mode; use --format=claude or --format=codex")
	}
	if err != nil {
		return nil, err
	}

	selectedFormats, err := parseSelectedFormatLabels(selected)
	if err != nil {
		return nil, err
	}
	if len(selectedFormats) == 0 {
		return nil, stackiterrors.ErrCanceled
	}
	return targetsForFormats(selectedFormats), nil
}

func confirmOverwriteIfNeeded(baseDir string, targets []agentInstallTarget, force bool, version string, out io.Writer) error {
	if force {
		return nil
	}

	existing := detectExistingInstallations(baseDir, targets)
	if len(existing) == 0 {
		return nil
	}

	confirmed, err := promptOverwriteExistingInstallations(existing, version)
	if errors.Is(err, tui.ErrInteractiveDisabled) {
		var hasConflict bool
		for _, target := range targets {
			if err := checkExistingInstallation(baseDir, target.format, version, out); err != nil {
				hasConflict = true
			}
		}
		if hasConflict {
			return fmt.Errorf("existing installation found")
		}
		return nil
	}
	if errors.Is(err, stackiterrors.ErrCanceled) {
		return err
	}
	if err != nil {
		return err
	}
	if !confirmed {
		return stackiterrors.ErrCanceled
	}

	return nil
}

func detectExistingInstallations(baseDir string, targets []agentInstallTarget) []existingSkillInstallation {
	existing := make([]existingSkillInstallation, 0, len(targets))
	for _, target := range targets {
		path, existingVersion, found := firstExistingInstallation(baseDir, target.format)
		if !found {
			continue
		}
		existing = append(existing, existingSkillInstallation{
			target:  target,
			path:    path,
			version: existingVersion,
		})
	}
	return existing
}

func firstExistingInstallation(baseDir string, format agentSkillFormat) (path, version string, found bool) {
	for _, skillPath := range installedSkillManifestPathsForFormat(baseDir, format) {
		content, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		return skillPath, extractVersion(string(content)), true
	}
	return "", "", false
}

func promptOverwriteExistingInstallations(existing []existingSkillInstallation, version string) (bool, error) {
	if len(existing) == 0 {
		return true, nil
	}

	var b strings.Builder
	b.WriteString("Existing skill installations detected:\n")
	for _, installation := range existing {
		_, _ = fmt.Fprintf(&b, "- %s", installation.target.displayPath)
		if installation.version != "" {
			_, _ = fmt.Fprintf(&b, " (version %s)", installation.version)
		}
		b.WriteString("\n")
	}
	if version != "" {
		_, _ = fmt.Fprintf(&b, "\nThese files will be overwritten with version %s.\n", version)
	} else {
		b.WriteString("\nThese files will be overwritten.\n")
	}

	return promptConfirm(b.String()+"Continue?", false)
}

func parseSelectedFormatLabels(selected []string) ([]agentSkillFormat, error) {
	formats := make([]agentSkillFormat, 0, len(selected))
	for _, label := range selected {
		switch {
		case strings.HasPrefix(label, "Claude Code -"):
			formats = append(formats, agentSkillFormatClaude)
		case strings.HasPrefix(label, "Codex -"):
			formats = append(formats, agentSkillFormatCodex)
		default:
			return nil, fmt.Errorf("unsupported selected format label %q", label)
		}
	}
	return dedupeFormats(formats), nil
}

func parseAgentSkillFormats(rawValues []string) ([]agentSkillFormat, error) {
	formats := make([]agentSkillFormat, 0, len(rawValues))
	for _, raw := range rawValues {
		parts := strings.SplitSeq(raw, ",")
		for part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			parsed, err := parseAgentSkillFormat(trimmed)
			if err != nil {
				return nil, err
			}
			formats = append(formats, parsed)
		}
	}
	if len(formats) == 0 {
		return nil, fmt.Errorf("at least one format must be provided")
	}
	return dedupeFormats(formats), nil
}

func dedupeFormats(formats []agentSkillFormat) []agentSkillFormat {
	seen := map[agentSkillFormat]bool{}
	result := make([]agentSkillFormat, 0, len(formats))
	for _, format := range formats {
		if seen[format] {
			continue
		}
		seen[format] = true
		result = append(result, format)
	}
	return result
}

func parseAgentSkillFormat(raw string) (agentSkillFormat, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(agentSkillFormatClaude), "claude-code":
		return agentSkillFormatClaude, nil
	case string(agentSkillFormatCodex):
		return agentSkillFormatCodex, nil
	default:
		return "", fmt.Errorf("unsupported format %q (expected claude or codex)", raw)
	}
}

func checkExistingInstallation(baseDir string, format agentSkillFormat, version string, out io.Writer) error {
	skillPath, existingVersion, found := firstExistingInstallation(baseDir, format)
	if !found {
		return nil
	}

	// Same version already installed — nothing to do.
	if version != "" && existingVersion == version {
		return nil
	}

	_, _ = fmt.Fprintf(out, "Found existing installation at %s", skillPath)
	if existingVersion != "" {
		_, _ = fmt.Fprintf(out, " (version %s)", existingVersion)
	}
	_, _ = fmt.Fprintln(out)
	if version != "" && existingVersion != "" {
		_, _ = fmt.Fprintf(out, "New version available: %s\n", version)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Run with --force to overwrite")
	return fmt.Errorf("existing installation found")
}

// promptAndInstallWorkflowBlock prompts the user and installs the workflow block.
// Returns (installed, path, error) where installed indicates if the block was added/updated.
func promptAndInstallWorkflowBlock(repoRoot string, force bool) (bool, string, error) {
	claude, agents := discoverAgentsFiles(repoRoot)

	// Check for read errors
	if claude.readErr != nil {
		return false, claude.name, fmt.Errorf("cannot read %s: %w", claude.name, claude.readErr)
	}
	if agents.readErr != nil {
		return false, agents.name, fmt.Errorf("cannot read %s: %w", agents.name, agents.readErr)
	}

	// Determine which file(s) are available
	var targetFile string
	switch {
	case claude.exists && agents.exists:
		// Both exist - prompt user to choose
		selected, err := promptSelect(
			"Both CLAUDE.md and AGENTS.md exist. Which file should receive the stacking workflow block?",
			[]tui.SelectOption{
				{Label: "Skip (don't add workflow block)", Value: skipValue},
				{Label: claudeMDFile, Value: claudeMDFile},
				{Label: agentsMDFile, Value: agentsMDFile},
			},
			0,
		)
		if errors.Is(err, stackiterrors.ErrCanceled) || errors.Is(err, tui.ErrInteractiveDisabled) {
			// User canceled or non-interactive mode - skip silently
			return false, "", nil
		}
		if err != nil {
			return false, "", fmt.Errorf("failed to prompt for file selection: %w", err)
		}
		if selected == skipValue {
			return false, "", nil
		}
		targetFile = selected

	case claude.exists:
		// Only CLAUDE.md exists - confirm
		confirmed, err := promptConfirm(
			fmt.Sprintf("Add stacking workflow block to %s?", claude.name),
			false,
		)
		if errors.Is(err, stackiterrors.ErrCanceled) || errors.Is(err, tui.ErrInteractiveDisabled) {
			return false, "", nil
		}
		if err != nil {
			return false, "", fmt.Errorf("failed to prompt for confirmation: %w", err)
		}
		if !confirmed {
			return false, "", nil
		}
		targetFile = claude.name

	case agents.exists:
		// Only AGENTS.md exists - confirm
		confirmed, err := promptConfirm(
			fmt.Sprintf("Add stacking workflow block to %s?", agents.name),
			false,
		)
		if errors.Is(err, stackiterrors.ErrCanceled) || errors.Is(err, tui.ErrInteractiveDisabled) {
			return false, "", nil
		}
		if err != nil {
			return false, "", fmt.Errorf("failed to prompt for confirmation: %w", err)
		}
		if !confirmed {
			return false, "", nil
		}
		targetFile = agents.name

	default:
		// Neither exists - ask if they want to create CLAUDE.md
		confirmed, err := promptConfirm(
			"No CLAUDE.md or AGENTS.md found. Create CLAUDE.md with stacking workflow block?",
			false,
		)
		if errors.Is(err, stackiterrors.ErrCanceled) || errors.Is(err, tui.ErrInteractiveDisabled) {
			return false, "", nil
		}
		if err != nil {
			return false, "", fmt.Errorf("failed to prompt for confirmation: %w", err)
		}
		if !confirmed {
			return false, "", nil
		}
		targetFile = claudeMDFile
	}

	// Get the file info for the selected file
	var fileInfo agentsFileInfo
	if targetFile == claudeMDFile {
		fileInfo = claude
	} else {
		fileInfo = agents
	}

	return installWorkflowBlock(repoRoot, targetFile, fileInfo, force)
}
