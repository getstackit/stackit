package pr

import (
	"github.com/getstackit/stackit/internal/engine"
)

// GenerateTitle creates a PR title from branch commits.
// If existingTitle is provided and non-empty, it's used as the base.
// Otherwise, the branch's default PR title is used.
// The scope is applied to the title.
func GenerateTitle(branch engine.Branch, existingTitle string, scope engine.Scope) string {
	title := existingTitle
	if title == "" {
		title = branch.DefaultPRTitle()
	}
	return scope.ApplyToTitle(title)
}
