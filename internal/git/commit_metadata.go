package git

import (
	"context"
	"fmt"
	"strings"
)

// CommitMetadata contains the original identity and topology needed to replay a commit.
type CommitMetadata struct {
	SHA         string
	ShortSHA    string
	Subject     string
	Parents     []string
	AuthorName  string
	AuthorEmail string
	AuthorDate  string
	Message     string
}

// CommitReadMode selects identity/topology only or full display/replay data.
type CommitReadMode int

const (
	CommitIDs CommitReadMode = iota
	CommitDetails
)

func commitReadFormat(mode CommitReadMode) string {
	if mode == CommitIDs {
		return "%H%x00%P"
	}
	return "%H%x00%P%x00%h%x00%s%x00%an%x00%ae%x00%aI%x00%B"
}

// readCommitRange preserves Git's ordering and range semantics, including merges.
func (r *runner) readCommitRange(ctx context.Context, mode CommitReadMode, rr RevRange) ([]CommitMetadata, error) {
	rangeArg := rr.Head
	if rr.Base != "" {
		rangeArg = rr.String()
	}
	out, err := r.RunGitCommandRawWithContext(ctx, "log", "-z",
		"--format="+commitReadFormat(mode), "--end-of-options", rangeArg, "--")
	if err != nil {
		return nil, err
	}
	return parseCommitRecords(out, mode)
}

func parseCommitRecords(out string, mode CommitReadMode) ([]CommitMetadata, error) {
	if out == "" {
		return nil, nil
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	width := 2
	if mode == CommitDetails {
		width = 8
	}
	if len(fields)%width != 0 {
		return nil, fmt.Errorf("malformed commit metadata: got %d fields", len(fields))
	}
	commits := make([]CommitMetadata, 0, len(fields)/width)
	for i := 0; i < len(fields); i += width {
		commit := CommitMetadata{SHA: fields[i], Parents: strings.Fields(fields[i+1])}
		if mode == CommitDetails {
			commit.ShortSHA, commit.Subject = fields[i+2], fields[i+3]
			commit.AuthorName, commit.AuthorEmail = fields[i+4], fields[i+5]
			commit.AuthorDate, commit.Message = fields[i+6], fields[i+7]
		}
		commits = append(commits, commit)
	}
	return commits, nil
}
