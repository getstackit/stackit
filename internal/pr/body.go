package pr

import (
	"github.com/getstackit/stackit/internal/engine"
)

// GenerateBody creates a PR body from branch commits.
// If existingBody is provided and non-empty, it's returned as-is.
// Otherwise, the branch's default PR body is used.
func GenerateBody(branch engine.Branch, existingBody string) string {
	if existingBody != "" {
		return existingBody
	}
	return branch.DefaultPRBody()
}
