package git

import (
	"context"
	"fmt"
	"strings"
)

// CommitMetadata contains the original identity and topology needed to replay a commit.
type CommitMetadata struct {
	SHA         string
	Parents     []string
	AuthorName  string
	AuthorEmail string
	AuthorDate  string
	Message     string
}

// GetCommitRangeMetadata reads a range newest-first in one Git process. NUL
// delimiters preserve multiline messages and empty fields without line parsing.
func (r *runner) GetCommitRangeMetadata(ctx context.Context, rr RevRange) ([]CommitMetadata, error) {
	rangeArg := rr.Head
	if rr.Base != "" {
		rangeArg = rr.String()
	}
	out, err := r.RunGitCommandRawWithContext(ctx, "log", "-z",
		"--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%B", rangeArg, "--")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(fields)%6 != 0 {
		return nil, fmt.Errorf("malformed commit metadata: got %d fields", len(fields))
	}
	commits := make([]CommitMetadata, 0, len(fields)/6)
	for i := 0; i < len(fields); i += 6 {
		commits = append(commits, CommitMetadata{
			SHA: fields[i], Parents: strings.Fields(fields[i+1]),
			AuthorName: fields[i+2], AuthorEmail: fields[i+3],
			AuthorDate: fields[i+4], Message: fields[i+5],
		})
	}
	return commits, nil
}
