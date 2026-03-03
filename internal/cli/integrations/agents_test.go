package integrations

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	stackiterrors "stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/tui"
)

func TestReplaceWorkflowBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		newBlock string
		expected string
	}{
		{
			name:     "replace block in middle of file",
			content:  "# Header\n\nSome content\n\n<!-- stackit:start -->\nold block\n<!-- stackit:end -->\n\n## Footer\n\nMore content",
			newBlock: "<!-- stackit:start -->\nnew block\n<!-- stackit:end -->",
			expected: "# Header\n\nSome content\n\n<!-- stackit:start -->\nnew block\n<!-- stackit:end -->\n## Footer\n\nMore content",
		},
		{
			name:     "replace block at start of file",
			content:  "<!-- stackit:start -->\nold block\n<!-- stackit:end -->\n\n## Content",
			newBlock: "<!-- stackit:start -->\nnew block\n<!-- stackit:end -->",
			expected: "<!-- stackit:start -->\nnew block\n<!-- stackit:end -->\n## Content",
		},
		{
			name:     "replace block at end of file",
			content:  "# Header\n\n<!-- stackit:start -->\nold block\n<!-- stackit:end -->",
			newBlock: "<!-- stackit:start -->\nnew block\n<!-- stackit:end -->",
			expected: "# Header\n\n<!-- stackit:start -->\nnew block\n<!-- stackit:end -->",
		},
		{
			name:     "returns original when start marker missing",
			content:  "# Header\n\nold block\n<!-- stackit:end -->\n",
			newBlock: "<!-- stackit:start -->\nnew block\n<!-- stackit:end -->",
			expected: "# Header\n\nold block\n<!-- stackit:end -->\n",
		},
		{
			name:     "returns original when end marker missing",
			content:  "# Header\n\n<!-- stackit:start -->\nold block\n",
			newBlock: "<!-- stackit:start -->\nnew block\n<!-- stackit:end -->",
			expected: "# Header\n\n<!-- stackit:start -->\nold block\n",
		},
		{
			name:     "returns original when end marker before start marker",
			content:  "# Header\n\n<!-- stackit:end -->\n\n<!-- stackit:start -->\n",
			newBlock: "<!-- stackit:start -->\nnew block\n<!-- stackit:end -->",
			expected: "# Header\n\n<!-- stackit:end -->\n\n<!-- stackit:start -->\n",
		},
		{
			name:     "handles empty before section",
			content:  "<!-- stackit:start -->\nold\n<!-- stackit:end -->\n\nAfter",
			newBlock: "<!-- stackit:start -->\nnew\n<!-- stackit:end -->",
			expected: "<!-- stackit:start -->\nnew\n<!-- stackit:end -->\nAfter",
		},
		{
			name:     "handles empty after section",
			content:  "Before\n\n<!-- stackit:start -->\nold\n<!-- stackit:end -->",
			newBlock: "<!-- stackit:start -->\nnew\n<!-- stackit:end -->",
			expected: "Before\n\n<!-- stackit:start -->\nnew\n<!-- stackit:end -->",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := replaceWorkflowBlock(tt.content, tt.newBlock)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckAgentsFile(t *testing.T) {
	t.Parallel()

	t.Run("returns not exists when file missing", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		info := checkAgentsFile(tmpDir, "CLAUDE.md")

		require.False(t, info.exists)
		require.False(t, info.hasBlock)
		require.Empty(t, info.content)
		require.NoError(t, info.readErr)
	})

	t.Run("returns exists without block", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "CLAUDE.md")
		err := os.WriteFile(filePath, []byte("# My Project\n\nSome content"), 0600)
		require.NoError(t, err)

		info := checkAgentsFile(tmpDir, "CLAUDE.md")

		require.True(t, info.exists)
		require.False(t, info.hasBlock)
		require.Equal(t, "# My Project\n\nSome content", info.content)
		require.NoError(t, info.readErr)
	})

	t.Run("returns exists with block", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "AGENTS.md")
		content := "# Project\n\n<!-- stackit:start -->\nblock\n<!-- stackit:end -->\n"
		err := os.WriteFile(filePath, []byte(content), 0600)
		require.NoError(t, err)

		info := checkAgentsFile(tmpDir, "AGENTS.md")

		require.True(t, info.exists)
		require.True(t, info.hasBlock)
		require.Equal(t, content, info.content)
		require.NoError(t, info.readErr)
	})
}

func TestDiscoverAgentsFiles(t *testing.T) {
	t.Parallel()

	t.Run("neither file exists", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		claude, agents := discoverAgentsFiles(tmpDir)

		require.False(t, claude.exists)
		require.False(t, agents.exists)
	})

	t.Run("only CLAUDE.md exists", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("content"), 0600)
		require.NoError(t, err)

		claude, agents := discoverAgentsFiles(tmpDir)

		require.True(t, claude.exists)
		require.False(t, agents.exists)
	})

	t.Run("only AGENTS.md exists", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("content"), 0600)
		require.NoError(t, err)

		claude, agents := discoverAgentsFiles(tmpDir)

		require.False(t, claude.exists)
		require.True(t, agents.exists)
	})

	t.Run("both files exist", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("claude"), 0600)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("agents"), 0600)
		require.NoError(t, err)

		claude, agents := discoverAgentsFiles(tmpDir)

		require.True(t, claude.exists)
		require.True(t, agents.exists)
		require.Equal(t, "claude", claude.content)
		require.Equal(t, "agents", agents.content)
	})
}

func TestInstallWorkflowBlock(t *testing.T) {
	t.Parallel()

	t.Run("appends to empty file", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fileInfo := agentsFileInfo{
			name:    "CLAUDE.md",
			exists:  false,
			content: "",
		}

		installed, path, err := installWorkflowBlock(tmpDir, "CLAUDE.md", fileInfo, false)

		require.NoError(t, err)
		require.True(t, installed)
		require.Equal(t, "CLAUDE.md", path)

		content, err := os.ReadFile(filepath.Join(tmpDir, "CLAUDE.md"))
		require.NoError(t, err)
		require.Contains(t, string(content), "<!-- stackit:start -->")
		require.Contains(t, string(content), "<!-- stackit:end -->")
	})

	t.Run("appends to existing file without block", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		existingContent := "# My Project\n\nSome content"
		fileInfo := agentsFileInfo{
			name:    "CLAUDE.md",
			exists:  true,
			content: existingContent,
		}

		installed, path, err := installWorkflowBlock(tmpDir, "CLAUDE.md", fileInfo, false)

		require.NoError(t, err)
		require.True(t, installed)
		require.Equal(t, "CLAUDE.md", path)

		content, err := os.ReadFile(filepath.Join(tmpDir, "CLAUDE.md"))
		require.NoError(t, err)
		require.Contains(t, string(content), "# My Project")
		require.Contains(t, string(content), "<!-- stackit:start -->")
	})

	t.Run("errors when block exists without force", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fileInfo := agentsFileInfo{
			name:     "CLAUDE.md",
			exists:   true,
			hasBlock: true,
			content:  "<!-- stackit:start -->\nold\n<!-- stackit:end -->",
		}

		installed, path, err := installWorkflowBlock(tmpDir, "CLAUDE.md", fileInfo, false)

		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
		require.Contains(t, err.Error(), "--force")
		require.False(t, installed)
		require.Equal(t, "CLAUDE.md", path)
	})

	t.Run("replaces block with force", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fileInfo := agentsFileInfo{
			name:     "AGENTS.md",
			exists:   true,
			hasBlock: true,
			content:  "# Header\n\n<!-- stackit:start -->\nold block\n<!-- stackit:end -->\n\n# Footer",
		}

		installed, path, err := installWorkflowBlock(tmpDir, "AGENTS.md", fileInfo, true)

		require.NoError(t, err)
		require.True(t, installed)
		require.Equal(t, "AGENTS.md", path)

		content, err := os.ReadFile(filepath.Join(tmpDir, "AGENTS.md"))
		require.NoError(t, err)
		require.Contains(t, string(content), "# Header")
		require.Contains(t, string(content), "# Footer")
		require.Contains(t, string(content), "<!-- stackit:start -->")
		require.NotContains(t, string(content), "old block")
	})

	t.Run("adds trailing newline when missing", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fileInfo := agentsFileInfo{
			name:    "CLAUDE.md",
			exists:  true,
			content: "# Header", // No trailing newline
		}

		installed, _, err := installWorkflowBlock(tmpDir, "CLAUDE.md", fileInfo, false)

		require.NoError(t, err)
		require.True(t, installed)

		content, err := os.ReadFile(filepath.Join(tmpDir, "CLAUDE.md"))
		require.NoError(t, err)
		// Should have proper spacing between original content and block
		require.Contains(t, string(content), "# Header\n\n<!-- stackit:start -->")
	})
}

// Not parallel: subtests mutate tui.PromptConfirm.
func TestPromptAndInstallWorkflowBlockConfirmDefaultsToSkip(t *testing.T) {
	t.Run("only CLAUDE.md exists uses false default", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("# Project"), 0600)
		require.NoError(t, err)

		originalPromptConfirm := tui.PromptConfirm
		t.Cleanup(func() {
			tui.PromptConfirm = originalPromptConfirm
		})

		var called bool
		tui.PromptConfirm = func(_ string, defaultValue bool) (bool, error) {
			called = true
			require.False(t, defaultValue)
			return false, nil
		}

		installed, path, err := promptAndInstallWorkflowBlock(tmpDir, false)
		require.NoError(t, err)
		require.False(t, installed)
		require.Empty(t, path)
		require.True(t, called)
	})

	t.Run("no agent files exist uses false default", func(t *testing.T) {
		tmpDir := t.TempDir()

		originalPromptConfirm := tui.PromptConfirm
		t.Cleanup(func() {
			tui.PromptConfirm = originalPromptConfirm
		})

		var called bool
		tui.PromptConfirm = func(_ string, defaultValue bool) (bool, error) {
			called = true
			require.False(t, defaultValue)
			return false, nil
		}

		installed, path, err := promptAndInstallWorkflowBlock(tmpDir, false)
		require.NoError(t, err)
		require.False(t, installed)
		require.Empty(t, path)
		require.True(t, called)
	})
}

// Not parallel: mutates promptSelect.
func TestPromptAndInstallWorkflowBlockBothFilesDefaultsToSkip(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("# Claude"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("# Agents"), 0600)
	require.NoError(t, err)

	originalPromptSelect := promptSelect
	t.Cleanup(func() {
		promptSelect = originalPromptSelect
	})

	var called bool
	promptSelect = func(_ string, options []tui.SelectOption, defaultIndex int) (string, error) {
		called = true
		require.Equal(t, 3, len(options))
		require.Equal(t, "skip", options[0].Value)
		require.Equal(t, "CLAUDE.md", options[1].Value)
		require.Equal(t, "AGENTS.md", options[2].Value)
		require.Equal(t, 0, defaultIndex)
		return "skip", nil
	}

	installed, path, err := promptAndInstallWorkflowBlock(tmpDir, false)
	require.NoError(t, err)
	require.False(t, installed)
	require.Empty(t, path)
	require.True(t, called)
}

// Not parallel: mutates promptMultiSelectWithDefault.
func TestSelectInstallTargetsPromptsWithoutSkipOption(t *testing.T) {
	tmpDir := t.TempDir()

	originalPromptMultiSelect := promptMultiSelectWithDefault
	t.Cleanup(func() {
		promptMultiSelectWithDefault = originalPromptMultiSelect
	})

	var called bool
	promptMultiSelectWithDefault = func(_ string, options []string, preSelected []bool) ([]string, error) {
		called = true
		require.Equal(t, []string{
			"Claude Code - Claude Code CLI skill format (~/.claude/skills/stackit)",
			"Codex - Codex skill format (~/.codex/skills/stackit)",
		}, options)
		require.Equal(t, []bool{true, false}, preSelected)
		return []string{options[0]}, nil
	}

	targets, err := selectInstallTargets(tmpDir, nil)
	require.NoError(t, err)
	require.True(t, called)
	require.Len(t, targets, 1)
	require.Equal(t, "~/.claude/skills/stackit", targets[0].displayPath)
}

func TestBuildAgentFileGroups(t *testing.T) {
	t.Parallel()

	t.Run("includes codex metadata group", func(t *testing.T) {
		t.Parallel()
		target := installTargetForFormat(agentSkillFormatCodex)
		groups := buildAgentFileGroups(target)

		var found bool
		for _, g := range groups {
			if g.destDir == filepath.Join(".codex", "skills", "stackit", "agents") {
				require.Equal(t, "agents/templates/skill/agents", g.templateDir)
				require.Equal(t, []string{"openai.yaml"}, g.files)
				found = true
				break
			}
		}
		require.True(t, found)
	})

	t.Run("includes claude slash commands group", func(t *testing.T) {
		t.Parallel()
		target := installTargetForFormat(agentSkillFormatClaude)
		groups := buildAgentFileGroups(target)

		var found bool
		for _, g := range groups {
			if g.destDir == filepath.Join(".claude", "commands") {
				require.Equal(t, "agents/templates/commands", g.templateDir)
				require.Equal(t, len(claudeCommandFiles), len(g.files))
				found = true
				break
			}
		}
		require.True(t, found)
	})

	t.Run("codex does not include claude slash commands group", func(t *testing.T) {
		t.Parallel()
		target := installTargetForFormat(agentSkillFormatCodex)
		groups := buildAgentFileGroups(target)

		for _, g := range groups {
			require.NotEqual(t, filepath.Join(".claude", "commands"), g.destDir)
		}
	})
}

func TestCheckExistingInstallation(t *testing.T) {
	t.Parallel()

	t.Run("returns error for claude version mismatch", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".claude", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte(testSkillContent("1.0.0")), 0600)
		require.NoError(t, err)

		var out bytes.Buffer
		err = checkExistingInstallation(tmpDir, agentSkillFormatClaude, "2.0.0", &out)
		require.Error(t, err)
		require.Contains(t, out.String(), "existing installation")
		require.Contains(t, out.String(), "1.0.0")
	})

	t.Run("returns error for codex version mismatch", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".codex", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte(testSkillContent("1.0.0")), 0600)
		require.NoError(t, err)

		var out bytes.Buffer
		err = checkExistingInstallation(tmpDir, agentSkillFormatCodex, "2.0.0", &out)
		require.Error(t, err)
		require.Contains(t, out.String(), "existing installation")
		require.Contains(t, out.String(), "1.0.0")
	})

	t.Run("does not error when versions match", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".codex", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte(testSkillContent("2.0.0")), 0600)
		require.NoError(t, err)

		var out bytes.Buffer
		err = checkExistingInstallation(tmpDir, agentSkillFormatCodex, "2.0.0", &out)
		require.NoError(t, err)
		require.Empty(t, out.String())
	})
}

// Not parallel: subtests mutate tui.PromptConfirm.
func TestConfirmOverwriteIfNeeded(t *testing.T) {
	t.Run("continues when user confirms overwrite", func(t *testing.T) {
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".claude", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte(testSkillContent("1.0.0")), 0600)
		require.NoError(t, err)

		originalPromptConfirm := tui.PromptConfirm
		t.Cleanup(func() {
			tui.PromptConfirm = originalPromptConfirm
		})

		called := false
		tui.PromptConfirm = func(prompt string, defaultValue bool) (bool, error) {
			called = true
			require.Contains(t, prompt, "Existing skill installations detected")
			require.Contains(t, prompt, "~/.claude/skills/stackit")
			return true, nil
		}

		err = confirmOverwriteIfNeeded(
			tmpDir,
			[]agentInstallTarget{installTargetForFormat(agentSkillFormatClaude)},
			false,
			"2.0.0",
			io.Discard,
		)
		require.NoError(t, err)
		require.True(t, called)
	})

	t.Run("aborts when user declines overwrite", func(t *testing.T) {
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".codex", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte(testSkillContent("1.0.0")), 0600)
		require.NoError(t, err)

		originalPromptConfirm := tui.PromptConfirm
		t.Cleanup(func() {
			tui.PromptConfirm = originalPromptConfirm
		})
		tui.PromptConfirm = func(_ string, _ bool) (bool, error) {
			return false, nil
		}

		err = confirmOverwriteIfNeeded(
			tmpDir,
			[]agentInstallTarget{installTargetForFormat(agentSkillFormatCodex)},
			false,
			"2.0.0",
			io.Discard,
		)
		require.ErrorIs(t, err, stackiterrors.ErrCanceled)
	})

	t.Run("requires force in non-interactive mode", func(t *testing.T) {
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".codex", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte(testSkillContent("1.0.0")), 0600)
		require.NoError(t, err)

		originalPromptConfirm := tui.PromptConfirm
		t.Cleanup(func() {
			tui.PromptConfirm = originalPromptConfirm
		})
		tui.PromptConfirm = func(_ string, _ bool) (bool, error) {
			return false, tui.ErrInteractiveDisabled
		}

		var out bytes.Buffer
		err = confirmOverwriteIfNeeded(
			tmpDir,
			[]agentInstallTarget{installTargetForFormat(agentSkillFormatCodex)},
			false,
			"2.0.0",
			&out,
		)
		require.Error(t, err)
		require.Contains(t, out.String(), "Run with --force to overwrite")
	})

	t.Run("reports all conflicts in non-interactive mode", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Install both claude and codex skills
		for _, dir := range []string{".claude", ".codex"} {
			skillPath := filepath.Join(tmpDir, dir, "skills", "stackit", "SKILL.md")
			err := os.MkdirAll(filepath.Dir(skillPath), 0750)
			require.NoError(t, err)
			err = os.WriteFile(skillPath, []byte(testSkillContent("1.0.0")), 0600)
			require.NoError(t, err)
		}

		originalPromptConfirm := tui.PromptConfirm
		t.Cleanup(func() {
			tui.PromptConfirm = originalPromptConfirm
		})
		tui.PromptConfirm = func(_ string, _ bool) (bool, error) {
			return false, tui.ErrInteractiveDisabled
		}

		var out bytes.Buffer
		err := confirmOverwriteIfNeeded(
			tmpDir,
			[]agentInstallTarget{
				installTargetForFormat(agentSkillFormatClaude),
				installTargetForFormat(agentSkillFormatCodex),
			},
			false,
			"2.0.0",
			&out,
		)
		require.Error(t, err)
		require.Contains(t, out.String(), ".claude")
		require.Contains(t, out.String(), ".codex")
	})
}

func TestIsAnySkillInstalled(t *testing.T) {
	t.Parallel()

	t.Run("detects codex skill", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".codex", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte("skill"), 0600)
		require.NoError(t, err)

		require.True(t, isAnySkillInstalled(tmpDir))
	})

	t.Run("detects legacy lowercase claude skill", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".claude", "skills", "stackit", "skill.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte("skill"), 0600)
		require.NoError(t, err)

		require.True(t, isAnySkillInstalled(tmpDir))
	})

	t.Run("returns false when no skill files exist", func(t *testing.T) {
		t.Parallel()
		require.False(t, isAnySkillInstalled(t.TempDir()))
	})
}

func TestPrintSuccessMessageIncludesCodex(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	printSuccessMessage(&out, []agentInstallTarget{installTargetForFormat(agentSkillFormatCodex)}, false, "", len(claudeCommandFiles))

	require.Contains(t, out.String(), "Installed agent files")
	require.Contains(t, out.String(), "~/.codex/skills/stackit")
	require.NotContains(t, out.String(), "Slash commands:")
}

func TestPrintSuccessMessageIncludesClaudeCommands(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	printSuccessMessage(&out, []agentInstallTarget{installTargetForFormat(agentSkillFormatClaude)}, false, "", len(claudeCommandFiles))

	require.Contains(t, out.String(), "~/.claude/skills/stackit")
	require.Contains(t, out.String(), "Slash commands:")
	require.Contains(t, out.String(), "/stack-describe")
	require.Contains(t, out.String(), "/stack-modify")
}

func TestPrintSuccessMessageIncludesMultipleTargets(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	printSuccessMessage(
		&out,
		[]agentInstallTarget{
			installTargetForFormat(agentSkillFormatClaude),
			installTargetForFormat(agentSkillFormatCodex),
		},
		false,
		"",
		len(claudeCommandFiles),
	)

	require.Contains(t, out.String(), "~/.claude/skills/stackit")
	require.Contains(t, out.String(), "~/.codex/skills/stackit")
	require.Contains(t, out.String(), "Slash commands:")
}

func TestParseAgentSkillFormat(t *testing.T) {
	t.Parallel()

	parsed, err := parseAgentSkillFormat("claude")
	require.NoError(t, err)
	require.Equal(t, agentSkillFormatClaude, parsed)

	parsed, err = parseAgentSkillFormat("codex")
	require.NoError(t, err)
	require.Equal(t, agentSkillFormatCodex, parsed)

	_, err = parseAgentSkillFormat("invalid")
	require.Error(t, err)
}

func TestParseAgentSkillFormats(t *testing.T) {
	t.Parallel()

	formats, err := parseAgentSkillFormats([]string{"claude", "codex"})
	require.NoError(t, err)
	require.Equal(t, []agentSkillFormat{agentSkillFormatClaude, agentSkillFormatCodex}, formats)

	formats, err = parseAgentSkillFormats([]string{"claude,codex", "claude"})
	require.NoError(t, err)
	require.Equal(t, []agentSkillFormat{agentSkillFormatClaude, agentSkillFormatCodex}, formats)

	_, err = parseAgentSkillFormats([]string{"bad-format"})
	require.Error(t, err)
}

func TestParseSelectedFormatLabels(t *testing.T) {
	t.Parallel()

	formats, err := parseSelectedFormatLabels([]string{
		"Claude Code - Claude Code CLI skill format (~/.claude/skills/stackit)",
		"Codex - Codex skill format (~/.codex/skills/stackit)",
	})
	require.NoError(t, err)
	require.Equal(t, []agentSkillFormat{agentSkillFormatClaude, agentSkillFormatCodex}, formats)

	_, err = parseSelectedFormatLabels([]string{"Skip (don't install agent skills)"})
	require.Error(t, err)
}

func TestTargetsForFormats(t *testing.T) {
	t.Parallel()

	targets := targetsForFormats([]agentSkillFormat{agentSkillFormatClaude, agentSkillFormatCodex})
	require.Len(t, targets, 2)
	require.Equal(t, "~/.claude/skills/stackit", targets[0].displayPath)
	require.Equal(t, "~/.codex/skills/stackit", targets[1].displayPath)
}

func TestInstalledSkillManifestPathsForFormat(t *testing.T) {
	t.Parallel()

	base := "/tmp/test"
	paths := installedSkillManifestPathsForFormat(base, agentSkillFormatClaude)
	require.Len(t, paths, 2)
	require.Contains(t, paths[0], filepath.Join(".claude", "skills", "stackit"))

	paths = installedSkillManifestPathsForFormat(base, agentSkillFormatCodex)
	require.Len(t, paths, 2)
	require.Contains(t, paths[0], filepath.Join(".codex", "skills", "stackit"))
}

// Not parallel: uses t.Setenv.
func TestResolveInstallBaseDirUsesHomeDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	baseDir, err := resolveInstallBaseDir()
	require.NoError(t, err)
	require.Equal(t, homeDir, baseDir)
}

func testSkillContent(version string) string {
	return "---\nversion: " + version + "\n---\n"
}
