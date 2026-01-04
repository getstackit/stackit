package integrations

import "embed"

// TemplateVersion is deprecated and no longer used.
// Version is now passed as a parameter to avoid data races in concurrent test execution.
// Kept for backward compatibility but should not be referenced.
var TemplateVersion = "dev"

//go:embed agents/templates/skill/*.md agents/templates/skill/commands/*.md agents/templates/skill/workflows/*.md agents/templates/skill/scripts/*.sh agents/templates/commands/*.md agents/templates/cursor/*.md
var agentTemplates embed.FS
