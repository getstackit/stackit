// Package integrations provides commands for managing various integrations
package integrations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	stackiterrors "stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui"
)

// NewAgentsCmd creates the agent command
func NewAgentsCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agent integration files for Claude Code and Codex",
		Long: `Manage agent integration files that help AI assistants use stackit effectively.

This command generates configuration files that enable AI agents (like Claude Code and Codex)
to understand how to use stackit commands for managing stacked branches.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(newAgentInstallCmd(version))

	return cmd
}

// newAgentInstallCmd creates the agent install command
func newAgentInstallCmd(version string) *cobra.Command {
	var local bool
	var force bool
	var formats []string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install agent integration files",
		Long: `Install agent integration files for AI assistants.

By default, this command installs files in your home directory and prompts
you to select one or more skill folder formats.

This will create one or both:
  - ~/.claude/skills/stackit/ (Claude Code skill format)
  - ~/.codex/skills/stackit/ (Codex skill format)

These files contain instructions for AI agents on how to use stackit commands
to manage stacked branches, create commits, submit PRs, and more.

When run in a git repository, you will be prompted to add a stacking workflow
block to your project's CLAUDE.md or AGENTS.md file.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			return runAgentInstall(runner, local, force, formats, version, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "Deprecated: ignored (agent skills are always installed globally)")
	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite existing files")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "Skill format(s) to install (claude,codex). Repeat flag or use comma-separated values")
	_ = cmd.Flags().MarkDeprecated("local", "ignored: agent skills are always installed globally")

	return cmd
}

// fileGroup defines a set of files to install from templates.
type fileGroup struct {
	templateDir string   // source directory in embedded fs (e.g., "agents/templates/skill/commands")
	destDir     string   // destination directory relative to baseDir
	files       []string // list of filenames
	executable  bool     // whether to make files executable
	replaceVer  bool     // whether to replace {{VERSION}} placeholder
}

type agentSkillFormat string

const (
	agentSkillFormatClaude agentSkillFormat = "claude"
	agentSkillFormatCodex  agentSkillFormat = "codex"
)

type agentInstallTarget struct {
	format               agentSkillFormat
	skillDir             string
	displayPath          string
	includeAgentsMeta    bool
	includeClaudeCommand bool
}

type existingSkillInstallation struct {
	target  agentInstallTarget
	path    string
	version string
}

var (
	skillRootFiles = []string{"SKILL.md", "reference.md"}

	skillCommandFiles = []string{"navigation.md", "branch.md", "stack.md", "recovery.md"}

	skillWorkflowFiles = []string{"absorb-conflict.md", "conflict-resolution.md", "fix-absorb.md", "stack-fold.md"}

	skillScriptFiles = []string{"analyze_stack.sh"}

	subagentFiles = []string{"commit-message.md", "review-triage.md"}

	claudeCommandFiles = []string{
		"stack-absorb.md", "stack-create.md", "stack-describe.md", "stack-extract.md",
		"stack-fix.md", "stack-fold.md", "stack-modify.md", "stack-plan.md", "stack-restack.md",
		"stack-review.md", "stack-split.md", "stack-status.md", "stack-submit.md",
		"stack-sync.md", "stack-verify.md",
	}

	promptSelect                 = tui.PromptSelect
	promptMultiSelectWithDefault = tui.PromptMultiSelectWithDefaults
)

func runAgentInstall(runner git.Runner, local, force bool, formats []string, version string, out io.Writer) error {
	_ = local // Deprecated flag retained for compatibility.

	repoRoot, _ := runner.DiscoverRepoRoot()

	baseDir, err := resolveInstallBaseDir()
	if err != nil {
		return err
	}

	targets, err := selectInstallTargets(baseDir, formats)
	if err != nil {
		if errors.Is(err, stackiterrors.ErrCanceled) {
			return nil
		}
		return err
	}

	if err := confirmOverwriteIfNeeded(baseDir, targets, force, version, out); err != nil {
		if errors.Is(err, stackiterrors.ErrCanceled) {
			return nil
		}
		return err
	}

	for _, target := range targets {
		groups := buildAgentFileGroups(target)
		for _, g := range groups {
			if err := installFileGroup(baseDir, g, version); err != nil {
				return err
			}
		}
	}

	// Install workflow block to CLAUDE.md or AGENTS.md if in a git repo
	var workflowBlockInstalled bool
	var workflowBlockPath string
	if repoRoot != "" {
		workflowBlockInstalled, workflowBlockPath, err = promptAndInstallWorkflowBlock(repoRoot, force)
		if err != nil {
			return err
		}
	}

	printSuccessMessage(out, targets, workflowBlockInstalled, workflowBlockPath, len(claudeCommandFiles))
	return nil
}

func selectInstallTargets(baseDir string, formats []string) ([]agentInstallTarget, error) {
	if len(formats) > 0 {
		parsed, err := parseAgentSkillFormats(formats)
		if err != nil {
			return nil, err
		}
		return targetsForFormats(parsed), nil
	}

	hasClaudeDir := dirExists(filepath.Join(baseDir, ".claude"))
	hasCodexDir := dirExists(filepath.Join(baseDir, ".codex"))
	preSelected := []bool{
		hasClaudeDir,
		hasCodexDir,
	}
	if !hasClaudeDir && !hasCodexDir {
		preSelected = []bool{true, false}
	}

	selected, err := promptMultiSelectWithDefault(
		"Which skill format(s) would you like to install?",
		[]string{
			"Claude Code - Claude Code CLI skill format (~/.claude/skills/stackit)",
			"Codex - Codex skill format (~/.codex/skills/stackit)",
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

	return tui.PromptConfirm(b.String()+"Continue?", false)
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

func targetsForFormats(formats []agentSkillFormat) []agentInstallTarget {
	targets := make([]agentInstallTarget, 0, len(formats))
	for _, format := range formats {
		targets = append(targets, installTargetForFormat(format))
	}
	return targets
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

func installTargetForFormat(format agentSkillFormat) agentInstallTarget {
	switch format {
	case agentSkillFormatCodex:
		return agentInstallTarget{
			format:            agentSkillFormatCodex,
			skillDir:          filepath.Join(".codex", "skills", "stackit"),
			displayPath:       "~/.codex/skills/stackit",
			includeAgentsMeta: true,
		}
	default:
		return agentInstallTarget{
			format:               agentSkillFormatClaude,
			skillDir:             filepath.Join(".claude", "skills", "stackit"),
			displayPath:          "~/.claude/skills/stackit",
			includeClaudeCommand: true,
		}
	}
}

func buildAgentFileGroups(target agentInstallTarget) []fileGroup {
	groups := buildSkillFileGroups(target.skillDir, target.includeAgentsMeta)
	if target.includeClaudeCommand {
		groups = append(groups, fileGroup{
			templateDir: "agents/templates/commands",
			destDir:     filepath.Join(".claude", "commands"),
			files:       claudeCommandFiles,
		})
	}
	return groups
}

func resolveInstallBaseDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return homeDir, nil
}

func buildSkillFileGroups(skillDir string, includeAgentsMetadata bool) []fileGroup {
	groups := []fileGroup{
		{
			templateDir: "agents/templates/skill",
			destDir:     skillDir,
			files:       skillRootFiles,
			replaceVer:  true,
		},
		{
			templateDir: "agents/templates/skill/commands",
			destDir:     filepath.Join(skillDir, "commands"),
			files:       skillCommandFiles,
		},
		{
			templateDir: "agents/templates/skill/workflows",
			destDir:     filepath.Join(skillDir, "workflows"),
			files:       skillWorkflowFiles,
		},
		{
			templateDir: "agents/templates/skill/scripts",
			destDir:     filepath.Join(skillDir, "scripts"),
			files:       skillScriptFiles,
			executable:  true,
		},
		{
			templateDir: "agents/templates/subagents",
			destDir:     filepath.Join(skillDir, "subagents"),
			files:       subagentFiles,
		},
	}

	if includeAgentsMetadata {
		groups = append(groups, fileGroup{
			templateDir: "agents/templates/skill/agents",
			destDir:     filepath.Join(skillDir, "agents"),
			files:       []string{"openai.yaml"},
		})
	}

	return groups
}

func installFileGroup(baseDir string, g fileGroup, version string) error {
	destPath := filepath.Join(baseDir, g.destDir)
	if err := os.MkdirAll(destPath, 0750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", g.destDir, err)
	}

	for _, filename := range g.files {
		templatePath := g.templateDir + "/" + filename
		content, err := agentTemplates.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", templatePath, err)
		}

		if g.replaceVer {
			content = []byte(strings.ReplaceAll(string(content), "{{VERSION}}", version))
		}

		filePath := filepath.Join(destPath, filename)
		if err := os.WriteFile(filePath, content, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}

		if g.executable {
			// #nosec G302 - Scripts need to be executable
			if err := os.Chmod(filePath, 0700); err != nil {
				return fmt.Errorf("failed to make %s executable: %w", filename, err)
			}
		}
	}
	return nil
}

func printSuccessMessage(out io.Writer, targets []agentInstallTarget, workflowBlockInstalled bool, workflowBlockPath string, commandCount int) {
	_, _ = fmt.Fprintln(out, "✓ Installed agent files")

	installedClaudeCommands := false
	for _, target := range targets {
		_, _ = fmt.Fprintf(out, "✓ Created %s\n", target.displayPath)
		if target.includeClaudeCommand {
			installedClaudeCommands = true
		}
	}

	if installedClaudeCommands {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Slash commands:")
		_, _ = fmt.Fprintf(out, "✓ Created ~/.claude/commands/stack-*.md (%d commands)\n", commandCount)
	}

	if workflowBlockInstalled {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Stacking workflow documentation:")
		_, _ = fmt.Fprintf(out, "✓ Added stacking workflow block to %s\n", workflowBlockPath)
	}

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Available Claude Code commands:")
	_, _ = fmt.Fprintln(out, "  /stack-absorb  - Intelligently absorb changes into commits")
	_, _ = fmt.Fprintln(out, "  /stack-create  - Create branch with auto-naming")
	_, _ = fmt.Fprintln(out, "  /stack-describe - Generate PR descriptions from git history")
	_, _ = fmt.Fprintln(out, "  /stack-extract - Extract commits/files to independent branch")
	_, _ = fmt.Fprintln(out, "  /stack-fix     - Diagnose and fix stack issues")
	_, _ = fmt.Fprintln(out, "  /stack-fold    - Fold granular branches into parents")
	_, _ = fmt.Fprintln(out, "  /stack-modify  - Amend current branch commit")
	_, _ = fmt.Fprintln(out, "  /stack-plan    - Plan and create stack from uncommitted changes")
	_, _ = fmt.Fprintln(out, "  /stack-resolve - Resolve rebase conflicts with AI assistance")
	_, _ = fmt.Fprintln(out, "  /stack-restack - Rebase all branches in stack")
	_, _ = fmt.Fprintln(out, "  /stack-review  - Apply PR review comments and mark resolved")
	_, _ = fmt.Fprintln(out, "  /stack-split   - Split changes between current and new child branch")
	_, _ = fmt.Fprintln(out, "  /stack-status  - View stack state and health")
	_, _ = fmt.Fprintln(out, "  /stack-submit  - Submit PRs with generated descriptions")
	_, _ = fmt.Fprintln(out, "  /stack-sync    - Sync with trunk and cleanup")
	_, _ = fmt.Fprintln(out, "  /stack-verify  - Verify stack health by running checks")
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

func installedSkillManifestPaths(baseDir string) []string {
	paths := installedSkillManifestPathsForFormat(baseDir, agentSkillFormatClaude)
	paths = append(paths, installedSkillManifestPathsForFormat(baseDir, agentSkillFormatCodex)...)
	return paths
}

func installedSkillManifestPathsForFormat(baseDir string, format agentSkillFormat) []string {
	switch format {
	case agentSkillFormatCodex:
		return []string{
			filepath.Join(baseDir, ".codex", "skills", "stackit", "SKILL.md"),
			filepath.Join(baseDir, ".codex", "skills", "stackit", "skill.md"), // legacy path compatibility
		}
	default:
		return []string{
			filepath.Join(baseDir, ".claude", "skills", "stackit", "SKILL.md"),
			filepath.Join(baseDir, ".claude", "skills", "stackit", "skill.md"), // legacy path compatibility
		}
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func isAnySkillInstalled(baseDir string) bool {
	for _, skillPath := range installedSkillManifestPaths(baseDir) {
		if _, err := os.Stat(skillPath); err == nil {
			return true
		}
	}
	return false
}

func extractVersion(content string) string {
	// Look for version in frontmatter
	lines := strings.Split(content, "\n")
	inFrontmatter := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break
		}

		if inFrontmatter && strings.HasPrefix(line, "version:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}

	return ""
}

const (
	workflowBlockStart = "<!-- stackit:start -->"
	workflowBlockEnd   = "<!-- stackit:end -->"
)

// agentsFileInfo holds information about a potential agents file
type agentsFileInfo struct {
	name     string
	exists   bool
	hasBlock bool
	readErr  error // non-nil if file exists but couldn't be read (permission error, etc.)
	content  string
}

// discoverAgentsFiles checks for CLAUDE.md and AGENTS.md in the repo root.
func discoverAgentsFiles(repoRoot string) (claude, agents agentsFileInfo) {
	claude = checkAgentsFile(repoRoot, "CLAUDE.md")
	agents = checkAgentsFile(repoRoot, "AGENTS.md")
	return claude, agents
}

// checkAgentsFile checks if a specific agents file exists and its state.
func checkAgentsFile(repoRoot, filename string) agentsFileInfo {
	info := agentsFileInfo{name: filename}
	filePath := filepath.Join(repoRoot, filename)

	content, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File doesn't exist - that's fine
			return info
		}
		// File exists but we can't read it (permission error, etc.)
		info.exists = true
		info.readErr = err
		return info
	}

	info.exists = true
	info.content = string(content)
	info.hasBlock = strings.Contains(info.content, workflowBlockStart)
	return info
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
				{Label: "Skip (don't add workflow block)", Value: "skip"},
				{Label: "CLAUDE.md", Value: "CLAUDE.md"},
				{Label: "AGENTS.md", Value: "AGENTS.md"},
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
		if selected == "skip" {
			return false, "", nil
		}
		targetFile = selected

	case claude.exists:
		// Only CLAUDE.md exists - confirm
		confirmed, err := tui.PromptConfirm(
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
		confirmed, err := tui.PromptConfirm(
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
		confirmed, err := tui.PromptConfirm(
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
		targetFile = "CLAUDE.md"
	}

	// Get the file info for the selected file
	var fileInfo agentsFileInfo
	if targetFile == "CLAUDE.md" {
		fileInfo = claude
	} else {
		fileInfo = agents
	}

	return installWorkflowBlock(repoRoot, targetFile, fileInfo, force)
}

// installWorkflowBlock installs the workflow block to the specified file.
func installWorkflowBlock(repoRoot, targetFile string, fileInfo agentsFileInfo, force bool) (bool, string, error) {
	// Read the block template
	blockContent, err := agentTemplates.ReadFile("agents/templates/agents-block.md")
	if err != nil {
		return false, "", fmt.Errorf("failed to read workflow block template: %w", err)
	}

	targetPath := filepath.Join(repoRoot, targetFile)
	contentStr := fileInfo.content

	// Check for existing block
	if fileInfo.hasBlock {
		if !force {
			return false, targetFile, fmt.Errorf("stackit block already exists in %s, use --force to update", targetFile)
		}
		// Replace existing block
		contentStr = replaceWorkflowBlock(contentStr, string(blockContent))
	} else {
		// Append block
		if len(contentStr) > 0 && !strings.HasSuffix(contentStr, "\n") {
			contentStr += "\n"
		}
		if len(contentStr) > 0 {
			contentStr += "\n"
		}
		contentStr += string(blockContent)
	}

	if err := os.WriteFile(targetPath, []byte(contentStr), 0600); err != nil {
		return false, targetFile, fmt.Errorf("failed to write %s: %w", targetFile, err)
	}

	return true, targetFile, nil
}

// replaceWorkflowBlock replaces the existing stackit block with new content.
func replaceWorkflowBlock(content, newBlock string) string {
	startIdx := strings.Index(content, workflowBlockStart)
	endIdx := strings.Index(content, workflowBlockEnd)

	// Handle missing or malformed markers
	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		return content
	}

	endIdx += len(workflowBlockEnd)

	// Preserve content before and after the block
	before := content[:startIdx]
	after := content[endIdx:]

	// Trim trailing newlines from before and leading from after to avoid double spacing
	before = strings.TrimRight(before, "\n")
	after = strings.TrimLeft(after, "\n")

	var result strings.Builder
	result.WriteString(before)
	if len(before) > 0 {
		result.WriteString("\n\n")
	}
	result.WriteString(newBlock)
	if len(after) > 0 {
		result.WriteString("\n")
		result.WriteString(after)
	}

	return result.String()
}
