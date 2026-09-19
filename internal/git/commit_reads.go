package git

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ReadCommitInfo reads author names and dates together for one or many commit
// refs. Git resolves and peels the refs in one cat-file process. Results are
// keyed by the requested ref, including aliases that point at the same commit.
func (r *runner) ReadCommitInfo(ctx context.Context, refs ...string) ReadResults[CommitInfo] {
	result := ReadResults[CommitInfo]{Values: make(map[string]CommitInfo), Errors: make(map[string]error)}
	queries := make([]string, 0, len(refs))
	names := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, name := range refs {
		if seen[name] {
			continue
		}
		seen[name] = true
		query := name + "^{commit}"
		if strings.ContainsAny(query, "\n\x00") {
			sha, err := r.ReadRevisions(ctx, query).One()
			if err != nil {
				result.Errors[name] = err
				continue
			}
			query = sha
		}
		queries = append(queries, query)
		names = append(names, name)
	}
	if len(queries) == 0 {
		return result
	}
	out, err := r.runGitInternal(ctx, strings.Join(queries, "\n")+"\n", nil, false, "cat-file", "--batch")
	if err != nil {
		for _, name := range names {
			result.Errors[name] = err
		}
		return result
	}
	reader := &objectReader{stdout: bufio.NewReader(strings.NewReader(out))}
	for _, name := range names {
		content, _, found, err := reader.readResponse(name)
		if err == nil && !found {
			err = fmt.Errorf("commit not found: %s", name)
		}
		var info CommitInfo
		if err == nil {
			info, err = parseCommitInfo(content)
		}
		if err != nil {
			result.Errors[name] = err
		} else {
			result.Values[name] = info
		}
	}
	return result
}

func parseCommitInfo(content string) (CommitInfo, error) {
	headers, _, _ := strings.Cut(content, "\n\n")
	for line := range strings.SplitSeq(headers, "\n") {
		author, ok := strings.CutPrefix(line, "author ")
		if !ok {
			continue
		}
		nameEnd, emailEnd := strings.LastIndex(author, " <"), strings.LastIndex(author, ">")
		if nameEnd < 0 || emailEnd <= nameEnd {
			break
		}
		date := strings.Fields(author[emailEnd+1:])
		if len(date) != 2 {
			break
		}
		seconds, err := strconv.ParseInt(date[0], 10, 64)
		if err != nil {
			return CommitInfo{}, err
		}
		zone, err := time.Parse("-0700", date[1])
		if err != nil {
			return CommitInfo{}, err
		}
		return CommitInfo{Author: author[:nameEnd], Date: time.Unix(seconds, 0).In(zone.Location())}, nil
	}
	return CommitInfo{}, fmt.Errorf("commit has no valid author header")
}
