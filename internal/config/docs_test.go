package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateConfigDocs(t *testing.T) {
	t.Parallel()

	docs := GenerateConfigDocs()

	// Should have markdown headers
	assert.Contains(t, docs, "## Branch naming")
	assert.Contains(t, docs, "## Stack topology")
	assert.Contains(t, docs, "## PR submission")
	assert.Contains(t, docs, "## PR navigation")

	// Should have option documentation
	assert.Contains(t, docs, "### branch.pattern")
	assert.Contains(t, docs, "### stack.shape")
	assert.Contains(t, docs, "### submit.footer")
	assert.Contains(t, docs, "### navigation.when")

	// Should have examples
	assert.Contains(t, docs, "```bash")
	assert.Contains(t, docs, "stackit config set")
}

func TestGenerateConfigDocs_AllOptionsIncluded(t *testing.T) {
	t.Parallel()

	docs := GenerateConfigDocs()

	// All options should be documented
	for _, opt := range Options {
		// Skip hooks since they're documented separately in the manual section
		if opt.Section == "hooks" {
			continue
		}

		assert.Contains(t, docs, "### "+opt.YAMLPath,
			"Docs should include option: %s", opt.YAMLPath)
	}
}

func TestGenerateConfigDocs_ContainsDefaults(t *testing.T) {
	t.Parallel()

	docs := GenerateConfigDocs()

	// Check some key defaults are documented
	assert.Contains(t, docs, "**Default**: `true`", "Should document boolean default")
	assert.Contains(t, docs, "**Default**: `10`", "Should document integer default")
}

func TestGenerateConfigDocs_ContainsValidValues(t *testing.T) {
	t.Parallel()

	docs := GenerateConfigDocs()

	// Check that enum options have their valid values listed
	assert.Contains(t, docs, "`squash`", "Should list merge method options")
	assert.Contains(t, docs, "`always`", "Should list navigation.when options")
	assert.Contains(t, docs, "`body`", "Should list navigation.location options")
}

func TestGenerateConfigDocs_Deterministic(t *testing.T) {
	t.Parallel()

	docs1 := GenerateConfigDocs()
	docs2 := GenerateConfigDocs()

	require.Equal(t, docs1, docs2, "Docs generation should be deterministic")
}

func TestGenerateConfigDocs_ValidMarkdown(t *testing.T) {
	t.Parallel()

	docs := GenerateConfigDocs()

	// Check markdown structure is valid
	lines := strings.Split(docs, "\n")
	inCodeBlock := false

	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
		}

		// Headers outside code blocks should be properly formatted
		if !inCodeBlock && strings.HasPrefix(line, "#") {
			// Should have space after #
			if len(line) > 1 && line[1] != '#' && line[1] != ' ' {
				t.Errorf("Line %d has invalid header format: %q", i+1, line)
			}
		}
	}

	// Code blocks should be balanced
	assert.False(t, inCodeBlock, "Unbalanced code blocks in generated docs")
}
