package git

import "strings"

// Commits keeps identity, message and authorship together until a consumer
// projects the particular fields it needs.
type Commits []CommitMetadata

// Subjects returns nonempty, trimmed subjects in history order.
func (c Commits) Subjects() []string {
	return c.lines(func(commit CommitMetadata) string { return commit.Subject })
}

// Messages returns nonempty, trimmed messages in history order.
func (c Commits) Messages() []string {
	return c.lines(func(commit CommitMetadata) string { return commit.Message })
}

// Onelines formats abbreviated identities and subjects for terminal output.
func (c Commits) Onelines() []string {
	return c.lines(func(commit CommitMetadata) string { return commit.ShortSHA + " " + commit.Subject })
}

func (c Commits) lines(project func(CommitMetadata) string) []string {
	var result []string
	for _, commit := range c {
		if line := strings.TrimSpace(project(commit)); line != "" {
			result = append(result, line)
		}
	}
	return result
}
