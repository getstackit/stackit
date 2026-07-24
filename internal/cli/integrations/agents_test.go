package integrations

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	stackiterrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui"
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

// Not parallel: subtests mutate promptConfirm.
func TestPromptAndInstallWorkflowBlockConfirmDefaultsToSkip(t *testing.T) {
	t.Run("only CLAUDE.md exists uses false default", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("# Project"), 0600)
		require.NoError(t, err)

		originalPromptConfirm := promptConfirm
		t.Cleanup(func() {
			promptConfirm = originalPromptConfirm
		})

		var called bool
		promptConfirm = func(_ string, defaultValue bool) (bool, error) {
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

		originalPromptConfirm := promptConfirm
		t.Cleanup(func() {
			promptConfirm = originalPromptConfirm
		})

		var called bool
		promptConfirm = func(_ string, defaultValue bool) (bool, error) {
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
		require.Equal(t, []bool{true, true}, preSelected)
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
				require.Equal(t, "agents/templates/codex/skill/agents", g.templateDir)
				require.Equal(t, []string{"openai.yaml"}, g.files)
				found = true
				break
			}
		}
		require.True(t, found)
	})

	t.Run("codex uses dedicated template root", func(t *testing.T) {
		t.Parallel()
		target := installTargetForFormat(agentSkillFormatCodex)
		groups := buildAgentFileGroups(target)

		require.NotEmpty(t, groups)
		require.Equal(t, "agents/templates/codex/skill", groups[0].templateDir)
		require.Equal(t, []string{"SKILL.md"}, groups[0].files)
	})

	t.Run("codex does not install shared claude scripts", func(t *testing.T) {
		t.Parallel()
		target := installTargetForFormat(agentSkillFormatCodex)
		groups := buildAgentFileGroups(target)

		for _, g := range groups {
			require.NotEqual(t, filepath.Join(".codex", "skills", "stackit", "scripts"), g.destDir)
		}
	})

	t.Run("claude does not include commands file group", func(t *testing.T) {
		t.Parallel()
		target := installTargetForFormat(agentSkillFormatClaude)
		groups := buildAgentFileGroups(target)

		for _, g := range groups {
			require.NotEqual(t, filepath.Join(".claude", "commands"), g.destDir)
		}
	})

	t.Run("codex does not include commands file group", func(t *testing.T) {
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

	t.Run("detects codex body version", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".codex", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte("---\nname: stackit\ndescription: test\n---\n\n<!-- stackit-version: 2.0.0 -->\n"), 0600)
		require.NoError(t, err)

		var out bytes.Buffer
		err = checkExistingInstallation(tmpDir, agentSkillFormatCodex, "2.0.0", &out)
		require.NoError(t, err)
		require.Empty(t, out.String())
	})
}

// Not parallel: subtests mutate promptConfirm.
func TestConfirmOverwriteIfNeeded(t *testing.T) {
	t.Run("continues when user confirms overwrite", func(t *testing.T) {
		tmpDir := t.TempDir()
		skillPath := filepath.Join(tmpDir, ".claude", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte(testSkillContent("1.0.0")), 0600)
		require.NoError(t, err)

		originalPromptConfirm := promptConfirm
		t.Cleanup(func() {
			promptConfirm = originalPromptConfirm
		})

		called := false
		promptConfirm = func(prompt string, defaultValue bool) (bool, error) {
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

		originalPromptConfirm := promptConfirm
		t.Cleanup(func() {
			promptConfirm = originalPromptConfirm
		})
		promptConfirm = func(_ string, _ bool) (bool, error) {
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

		originalPromptConfirm := promptConfirm
		t.Cleanup(func() {
			promptConfirm = originalPromptConfirm
		})
		promptConfirm = func(_ string, _ bool) (bool, error) {
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

		originalPromptConfirm := promptConfirm
		t.Cleanup(func() {
			promptConfirm = originalPromptConfirm
		})
		promptConfirm = func(_ string, _ bool) (bool, error) {
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
	printSuccessMessage(&out, []agentInstallTarget{installTargetForFormat(agentSkillFormatCodex)}, false, "", len(commandTemplateFiles))

	require.Contains(t, out.String(), "Installed agent files")
	require.Contains(t, out.String(), "~/.codex/skills/stackit")
	require.Contains(t, out.String(), ".codex/skills/stack-*/")
	require.Contains(t, out.String(), "Available Codex skills:")
	require.Contains(t, out.String(), "stack-create")
	require.NotContains(t, out.String(), "Available Claude Code commands:")
	require.NotContains(t, out.String(), "/stack-create")
	require.NotContains(t, out.String(), "Slash commands:")
}

func TestPrintSuccessMessageIncludesClaudeSkills(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	printSuccessMessage(&out, []agentInstallTarget{installTargetForFormat(agentSkillFormatClaude)}, false, "", len(commandTemplateFiles))

	require.Contains(t, out.String(), "~/.claude/skills/stackit")
	require.Contains(t, out.String(), ".claude/skills/stack-*/")
	require.NotContains(t, out.String(), "Slash commands:")
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
		len(commandTemplateFiles),
	)

	require.Contains(t, out.String(), "~/.claude/skills/stackit")
	require.Contains(t, out.String(), "~/.codex/skills/stackit")
	require.Contains(t, out.String(), ".claude/skills/stack-*/")
	require.Contains(t, out.String(), ".codex/skills/stack-*/")
}

func TestRenderClaudeSkillContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		skill   string
		wantErr bool
		assert  func(t *testing.T, result string)
	}{
		{
			name:    "adds name and preserves command frontmatter",
			content: "---\ndescription: Test\nmodel: sonnet\nargument-hint: [-m \"message\"] [branch-name]\nallowed-tools: Bash(stackit:*)\n---\n\n## Arguments\n$ARGUMENTS\n",
			skill:   "stack-create",
			assert: func(t *testing.T, result string) {
				frontmatter, body := mustExtractFrontmatter(t, result)
				var meta map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &meta))
				require.Equal(t, "stack-create", meta["name"])
				require.Equal(t, "[-m \"message\"] [branch-name]", meta["argument-hint"])
				require.Contains(t, body, "$ARGUMENTS")
			},
		},
		{
			name:    "errors when frontmatter is missing",
			content: "# no frontmatter",
			skill:   "stack-sync",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := renderClaudeSkillContent([]byte(tt.content), tt.skill)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.assert(t, string(result))
		})
	}
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

func TestRenderCodexSkillContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		skill   string
		wantErr bool
		assert  func(t *testing.T, result string)
	}{
		{
			name:    "injects name and keeps codex frontmatter minimal",
			content: "---\ndescription: Use for Codex\nallowed-tools: Bash(stackit:*), Bash(git:*)\n---\n\n# Body",
			skill:   "stack-create",
			assert: func(t *testing.T, result string) {
				frontmatter, body := mustExtractFrontmatter(t, result)
				var meta map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &meta))
				require.Equal(t, "stack-create", meta["name"])
				require.Equal(t, "Use for Codex", meta["description"])
				require.Len(t, meta, 2)
				require.NotContains(t, frontmatter, "allowed-tools:")
				require.Equal(t, "\n# Body", body)
			},
		},
		{
			name:    "errors when frontmatter is missing",
			content: "# No frontmatter here",
			skill:   "stack-sync",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := renderCodexSkillContent([]byte(tt.content), tt.skill)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.assert(t, string(result))
		})
	}
}

func TestInstallClaudeSkillsMatchTemplates(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	require.NoError(t, installCommandSkills(tmpDir, filepath.Join(".claude", "skills"), commandTemplateFiles, renderClaudeSkillContent, agentSkillFormatClaude))

	for _, filename := range commandTemplateFiles {
		expectedTemplate, err := agentTemplates.ReadFile("agents/templates/commands/" + filename)
		require.NoError(t, err)

		skillName := strings.TrimSuffix(filename, ".md")
		installedPath := filepath.Join(tmpDir, ".claude", "skills", skillName, "SKILL.md")
		content, err := os.ReadFile(installedPath)
		require.NoError(t, err, "skill file should exist for %s", filename)

		expectedFrontmatter, expectedBody := mustExtractFrontmatter(t, string(expectedTemplate))
		actualFrontmatter, actualBody := mustExtractFrontmatter(t, string(content))
		var meta map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(actualFrontmatter), &meta))
		require.Equal(t, skillName, meta["name"])
		require.Equal(t, expectedBody, actualBody, "Claude skill body changed for %s", filename)
		require.Contains(t, actualFrontmatter, "description:")
		require.Contains(t, actualFrontmatter, "model:")
		require.Contains(t, actualFrontmatter, "allowed-tools:")
		if rawArgumentHint, ok := extractRawFrontmatterValue(expectedFrontmatter, "argument-hint"); ok {
			require.Equal(t, rawArgumentHint, meta["argument-hint"])
		}
	}
}

func TestInstallCodexCommandSkills(t *testing.T) {
	t.Parallel()

	files := []string{"stack-create.md", "stack-fix.md", "stack-modify.md"}
	tmpDir := t.TempDir()

	err := installCommandSkills(tmpDir, filepath.Join(".codex", "skills"), files, renderCodexSkillContent, agentSkillFormatCodex)
	require.NoError(t, err)

	for _, filename := range files {
		skillName := strings.TrimSuffix(filename, ".md")
		skillPath := filepath.Join(tmpDir, ".codex", "skills", skillName, "SKILL.md")

		content, err := os.ReadFile(skillPath)
		require.NoError(t, err, "SKILL.md should exist for %s", skillName)

		frontmatter, body := mustExtractFrontmatter(t, string(content))
		var meta map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &meta))
		require.Equal(t, skillName, meta["name"])
		require.Contains(t, frontmatter, "description:")
		require.Len(t, meta, 2)
		require.NotContains(t, frontmatter, "argument-hint:")
		require.NotContains(t, frontmatter, "allowed-tools:")
		require.NotContains(t, frontmatter, "model:")
		require.NotContains(t, body, "$ARGUMENTS")

		metadataPath := filepath.Join(tmpDir, ".codex", "skills", skillName, "agents", "openai.yaml")
		metadata, err := os.ReadFile(metadataPath)
		require.NoError(t, err, "openai.yaml should exist for %s", skillName)
		var openaiMeta map[string]map[string]string
		require.NoError(t, yaml.Unmarshal(metadata, &openaiMeta))
		require.Equal(t, displayNameForSkill(skillName), openaiMeta["interface"]["display_name"])
		require.Contains(t, openaiMeta["interface"]["default_prompt"], "$"+skillName)
	}
}

func TestRenderCodexCommandMetadata(t *testing.T) {
	t.Parallel()

	metadata, err := renderCodexCommandMetadata("stack-create")
	require.NoError(t, err)

	var parsed map[string]map[string]string
	require.NoError(t, yaml.Unmarshal(metadata, &parsed))
	require.Equal(t, "Stack Create", parsed["interface"]["display_name"])
	require.Equal(t, "Create branch with auto-naming", parsed["interface"]["short_description"])
	require.Equal(t, "Use $stack-create to create branch with auto-naming.", parsed["interface"]["default_prompt"])
}

func TestCodexUsesParallelCommandTemplate(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	err := installCommandSkills(tmpDir, filepath.Join(".codex", "skills"), []string{"stack-create.md"}, renderCodexSkillContent, agentSkillFormatCodex)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, ".codex", "skills", "stack-create", "SKILL.md"))
	require.NoError(t, err)

	frontmatter, body := mustExtractFrontmatter(t, string(content))

	require.Contains(t, body, "Create one new stacked branch from the current working tree.")
	require.NotContains(t, body, "Create a new stacked branch with the current changes.")
	require.NotContains(t, body, "AskUserQuestion")

	var meta map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &meta))
	require.Equal(t, "stack-create", meta["name"])
	require.Contains(t, meta["description"], "stack these changes")
}

func TestCodexStackPlanReferenceLink(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	err := installCommandSkills(tmpDir, filepath.Join(".codex", "skills"), []string{"stack-plan.md"}, renderCodexSkillContent, agentSkillFormatCodex)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, ".codex", "skills", "stack-plan", "SKILL.md"))
	require.NoError(t, err)

	_, body := mustExtractFrontmatter(t, string(content))
	require.Contains(t, body, "../stackit/references/stack-plan-recovery.md")
	require.NotContains(t, body, "../skill/references/stack-plan-recovery.md")
}

func TestCodexInstallHasNoClaudeIsms(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	target := installTargetForFormat(agentSkillFormatCodex)
	for _, g := range buildAgentFileGroups(target) {
		require.NoError(t, installFileGroup(tmpDir, g, "test"))
	}
	require.NoError(t, installCommandSkills(tmpDir, target.skillsBaseDir, commandTemplateFiles, renderCodexSkillContent, agentSkillFormatCodex))

	bannedSubstrings := []struct {
		needle string
		reason string
	}{
		{"AskUserQuestion", "Codex doesn't have AskUserQuestion — it's a Claude-only tool"},
		{"!`", "Claude shell-context macros must not leak into Codex output"},
		{"Claude Code", "Codex output must not describe itself as Claude Code"},
		{"~/.claude/skills", "Codex output must not reference ~/.claude paths"},
		{" using the Skill tool", "Skill tool prose must be rewritten for Codex"},
		{" using Skill tool", "Skill tool prose must be rewritten for Codex"},
	}

	codexRoot := filepath.Join(tmpDir, ".codex")
	err := filepath.Walk(codexRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		require.NoError(t, err)

		body := string(content)
		if strings.HasSuffix(path, ".md") {
			// Frontmatter `model:` line must not survive in Codex output.
			// (Match `^model:` or after newline to avoid matching e.g. "data model:" prose.)
			require.NotRegexp(t, `(?m)^model:`, body, "model: line found in %s", path)
			require.NotRegexp(t, `(?m)^allowed-tools:`, body, "allowed-tools line found in %s", path)
			require.NotContains(t, body, "`/stack-", "Markdown slash-command reference found in %s", path)
			require.NotRegexp(t, `(^|[\s(])/(stack-[a-z-]+)`, body, "plain slash-command reference found in %s", path)

			if strings.HasPrefix(body, "---\n") {
				frontmatter, _ := mustExtractFrontmatter(t, body)
				var meta map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &meta))
				require.Len(t, meta, 2, "Codex frontmatter should only include name and description in %s", path)
				require.Contains(t, meta, "name", "missing name in %s", path)
				require.Contains(t, meta, "description", "missing description in %s", path)
			}
		}

		for _, b := range bannedSubstrings {
			require.NotContains(t, body, b.needle, "%s in %s — %s", b.needle, path, b.reason)
		}
		return nil
	})
	require.NoError(t, err)
}

func TestCodexSourceTemplatesAreNative(t *testing.T) {
	t.Parallel()

	err := filepath.Walk(filepath.Join("agents", "templates", "codex"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		isRootSkill := strings.HasSuffix(path, filepath.Join("skill", "SKILL.md"))
		isCommandTemplate := strings.Contains(path, filepath.Join("codex", "commands")+string(os.PathSeparator))
		if !isRootSkill && !isCommandTemplate {
			return nil
		}

		content, err := os.ReadFile(path)
		require.NoError(t, err)
		frontmatter, _ := mustExtractFrontmatter(t, string(content))

		var meta map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &meta))
		require.Contains(t, meta, "description", "missing description in %s", path)
		require.NotContains(t, meta, "allowed-tools", "Codex source template should not include allowed-tools in %s", path)
		require.NotContains(t, meta, "model", "Codex source template should not include model in %s", path)
		require.NotContains(t, meta, "argument-hint", "Codex source template should not include argument-hint in %s", path)
		require.NotContains(t, meta, "version", "Codex source template should not include version in %s", path)

		if isRootSkill {
			require.Equal(t, "stackit", meta["name"])
			require.Len(t, meta, 2, "Codex root skill should only include name and description")
		} else {
			require.NotContains(t, meta, "name", "Codex command source names are injected during install in %s", path)
			require.Len(t, meta, 1, "Codex command sources should only include description in %s", path)
		}

		return nil
	})
	require.NoError(t, err)
}

func TestClaudeInstallUnchangedAfterCodexRefactor(t *testing.T) {
	t.Parallel()

	// Sanity: a Claude install should still produce per-command SKILL.md files
	// whose body is byte-identical to the shared template body. This guards
	// against Codex's parallel template path leaking into the Claude path.
	tmpDir := t.TempDir()
	require.NoError(t, installCommandSkills(tmpDir, filepath.Join(".claude", "skills"), commandTemplateFiles, renderClaudeSkillContent, agentSkillFormatClaude))

	for _, filename := range commandTemplateFiles {
		expectedTemplate, err := agentTemplates.ReadFile("agents/templates/commands/" + filename)
		require.NoError(t, err)

		skillName := strings.TrimSuffix(filename, ".md")
		installed, err := os.ReadFile(filepath.Join(tmpDir, ".claude", "skills", skillName, "SKILL.md"))
		require.NoError(t, err)

		_, expectedBody := mustExtractFrontmatter(t, string(expectedTemplate))
		_, actualBody := mustExtractFrontmatter(t, string(installed))
		require.Equal(t, expectedBody, actualBody, "Claude body changed for %s", filename)
	}
}

func mustExtractFrontmatter(t *testing.T, content string) (frontmatter, body string) {
	t.Helper()

	const marker = "---\n"
	require.True(t, strings.HasPrefix(content, marker), "content must start with frontmatter")

	rest := strings.TrimPrefix(content, marker)
	idx := strings.Index(rest, marker)
	require.NotEqual(t, -1, idx, "content must include closing frontmatter marker")

	return rest[:idx], rest[idx+len(marker):]
}

func extractRawFrontmatterValue(frontmatter, key string) (string, bool) {
	prefix := key + ":"
	for line := range strings.SplitSeq(frontmatter, "\n") {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(after), true
		}
	}
	return "", false
}

func testSkillContent(version string) string {
	return "---\nversion: " + version + "\n---\n"
}

// installSkillsForTarget runs the per-target install steps the way runAgentInstall
// does (file groups + command skills + prune), so prune tests exercise the real flow.
func installSkillsForTarget(t *testing.T, baseDir string, target agentInstallTarget) {
	t.Helper()
	for _, g := range buildAgentFileGroups(target) {
		require.NoError(t, installFileGroup(baseDir, g, "test"))
	}
	render := renderClaudeSkillContent
	if target.format == agentSkillFormatCodex {
		render = renderCodexSkillContent
	}
	require.NoError(t, installCommandSkills(baseDir, target.skillsBaseDir, commandTemplateFiles, render, target.format))
	require.NoError(t, pruneStaleSkillFiles(baseDir, target))
}

func seedFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0750))
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0600))
}

func TestPruneStaleSkillFilesCodexLayoutChange(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	target := installTargetForFormat(agentSkillFormatCodex)
	bundleDir := filepath.Join(baseDir, target.skillDir)

	// Seed orphans from the old codex bundle layout that shipped commands/,
	// workflows/, scripts/, subagents/, reference.md, and references/repo-defaults.md.
	orphans := []string{
		filepath.Join(bundleDir, "commands", "navigation.md"),
		filepath.Join(bundleDir, "workflows", "conflict-resolution.md"),
		filepath.Join(bundleDir, "scripts", "analyze_stack.sh"),
		filepath.Join(bundleDir, "subagents", "review-triage.md"),
		filepath.Join(bundleDir, "reference.md"),
		filepath.Join(bundleDir, "references", "repo-defaults.md"),
		// Orphan inside a current per-command skill dir (old stack-split/references/).
		filepath.Join(baseDir, target.skillsBaseDir, "stack-split", "references", "execution.md"),
	}
	for _, path := range orphans {
		seedFile(t, path)
	}

	// A sibling, non-stackit skill in the shared skills base dir must survive.
	userSkill := filepath.Join(baseDir, target.skillsBaseDir, "my-tool", "SKILL.md")
	seedFile(t, userSkill)

	installSkillsForTarget(t, baseDir, target)

	for _, path := range orphans {
		require.NoFileExists(t, path, "stale file should be pruned")
	}
	// Emptied orphan directories should be removed entirely.
	require.NoDirExists(t, filepath.Join(bundleDir, "commands"))
	require.NoDirExists(t, filepath.Join(bundleDir, "workflows"))
	require.NoDirExists(t, filepath.Join(bundleDir, "scripts"))
	require.NoDirExists(t, filepath.Join(bundleDir, "subagents"))
	require.NoDirExists(t, filepath.Join(baseDir, target.skillsBaseDir, "stack-split", "references"))

	// Current files survive.
	require.FileExists(t, filepath.Join(bundleDir, "SKILL.md"))
	require.FileExists(t, filepath.Join(bundleDir, "references", "workflows.md"))
	require.FileExists(t, filepath.Join(bundleDir, "agents", "openai.yaml"))
	require.FileExists(t, filepath.Join(baseDir, target.skillsBaseDir, "stack-split", "SKILL.md"))
	require.FileExists(t, filepath.Join(baseDir, target.skillsBaseDir, "stack-split", "agents", "openai.yaml"))

	// The unrelated user skill is untouched.
	require.FileExists(t, userSkill)
}

func TestPruneStaleSkillFilesClaudeOrphans(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	target := installTargetForFormat(agentSkillFormatClaude)
	bundleDir := filepath.Join(baseDir, target.skillDir)

	// Orphans the claude bundle stopped shipping after trimming subagentFiles
	// and skillScriptFiles.
	staleScript := filepath.Join(bundleDir, "scripts", "validate_pr.sh")
	staleSubagent := filepath.Join(bundleDir, "subagents", "pr-description.md")
	seedFile(t, staleScript)
	seedFile(t, staleSubagent)

	installSkillsForTarget(t, baseDir, target)

	require.NoFileExists(t, staleScript, "dropped script should be pruned")
	require.NoFileExists(t, staleSubagent, "dropped subagent should be pruned")

	// Files still shipped survive, and their directories aren't removed.
	require.FileExists(t, filepath.Join(bundleDir, "scripts", "analyze_stack.sh"))
	require.FileExists(t, filepath.Join(bundleDir, "subagents", "commit-message.md"))
	require.FileExists(t, filepath.Join(bundleDir, "subagents", "review-triage.md"))
	require.FileExists(t, filepath.Join(bundleDir, "SKILL.md"))
	require.FileExists(t, filepath.Join(bundleDir, "reference.md"))
}

func TestPruneStaleSkillFilesNoopOnCleanInstall(t *testing.T) {
	t.Parallel()

	for _, format := range []agentSkillFormat{agentSkillFormatClaude, agentSkillFormatCodex} {
		baseDir := t.TempDir()
		target := installTargetForFormat(format)

		// Two installs back to back: the second must leave the file set unchanged.
		installSkillsForTarget(t, baseDir, target)
		before := snapshotFiles(t, filepath.Join(baseDir, target.skillDir))
		installSkillsForTarget(t, baseDir, target)
		after := snapshotFiles(t, filepath.Join(baseDir, target.skillDir))

		require.Equal(t, before, after, "re-install should not add or drop files for %s", format)
	}
}

func snapshotFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, rel)
		}
		return nil
	}))
	return files
}
