package integrations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	workflowBlockStart = "<!-- stackit:start -->"
	workflowBlockEnd   = "<!-- stackit:end -->"

	// Agent instruction file names
	claudeMDFile = "CLAUDE.md"
	agentsMDFile = "AGENTS.md"

	// Format label strings used in multi-select prompts
	claudeFormatLabel = "Claude Code - Claude Code CLI skill format (~/.claude/skills/stackit)"
	codexFormatLabel  = "Codex - Codex skill format (~/.codex/skills/stackit)"

	// skipValue is the prompt value for skipping workflow block installation
	skipValue = "skip"
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
	claude = checkAgentsFile(repoRoot, claudeMDFile)
	agents = checkAgentsFile(repoRoot, agentsMDFile)
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
