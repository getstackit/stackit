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

// CommitNode is identity/topology only; it cannot be mistaken for display data.
type CommitNode struct {
	SHA     string
	Parents []string
}

func (c CommitNode) node() CommitNode     { return c }
func (c CommitMetadata) node() CommitNode { return CommitNode{SHA: c.SHA, Parents: c.Parents} }

type commitRecord interface{ node() CommitNode }

// The codec keeps Git's wire format and decoder together. Callers choose the
// result type, not a mode that could return partially populated metadata.
type commitCodec[T commitRecord] struct {
	format string
	width  int
	decode func([]string) T
}

var nodeCodec = commitCodec[CommitNode]{
	format: "%H%x00%P", width: 2,
	decode: func(f []string) CommitNode { return CommitNode{SHA: f[0], Parents: strings.Fields(f[1])} },
}

var metadataCodec = commitCodec[CommitMetadata]{
	format: "%H%x00%P%x00%h%x00%s%x00%an%x00%ae%x00%aI%x00%B", width: 8,
	decode: func(f []string) CommitMetadata {
		return CommitMetadata{SHA: f[0], Parents: strings.Fields(f[1]), ShortSHA: f[2],
			Subject: f[3], AuthorName: f[4], AuthorEmail: f[5], AuthorDate: f[6], Message: f[7]}
	},
}

// readCommitRange preserves Git's ordering and range semantics, including merges.
func readCommitRange[T commitRecord](r *runner, ctx context.Context, codec commitCodec[T], rr RevRange) ([]T, error) {
	rangeArg := rr.Head
	if rr.Base != "" {
		rangeArg = rr.String()
	}
	out, err := r.RunGitCommandRawWithContext(ctx, "log", "-z",
		"--format="+codec.format, "--end-of-options", rangeArg, "--")
	if err != nil {
		return nil, err
	}
	return parseCommitRecords(out, codec)
}

func parseCommitRecords[T commitRecord](out string, codec commitCodec[T]) ([]T, error) {
	if out == "" {
		return nil, nil
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(fields)%codec.width != 0 {
		return nil, fmt.Errorf("malformed commit metadata: got %d fields", len(fields))
	}
	commits := make([]T, 0, len(fields)/codec.width)
	for i := 0; i < len(fields); i += codec.width {
		commits = append(commits, codec.decode(fields[i:i+codec.width]))
	}
	return commits, nil
}
