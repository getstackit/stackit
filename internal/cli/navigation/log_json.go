package navigation

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/actions/trunklog"
	"github.com/getstackit/stackit/internal/output"
)

// logCommitJSON is the per-commit shape emitted by `stackit log --json`. It is a
// CLI-owned contract (deliberately not the HTTP DTO) consumed by release tooling;
// field renames/removals are breaking. Field names mirror the web DTO for
// recognizability.
type logCommitJSON struct {
	SHA           string         `json:"sha"`
	Message       string         `json:"message"`
	Author        string         `json:"author"`
	Date          string         `json:"date"` // RFC3339
	Kind          string         `json:"kind"` // "regular" | "stack-merge"
	PRNumber      int            `json:"prNumber,omitempty"`
	StackSize     int            `json:"stackSize,omitempty"`
	StackPRs      []int          `json:"stackPRs,omitempty"`
	StackPRTitles map[int]string `json:"stackPRTitles,omitempty"`
	StackScope    string         `json:"stackScope,omitempty"`
}

// logJSON is the top-level `stackit log --json` payload.
type logJSON struct {
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Commits []logCommitJSON `json:"commits"`
}

// printLogJSON marshals the gathered trunk history to indented JSON.
func printLogJSON(out output.Output, res trunklog.Result) error {
	payload := logJSON{
		From:    res.From,
		To:      res.To,
		Commits: make([]logCommitJSON, 0, len(res.Commits)),
	}
	for _, c := range res.Commits {
		payload.Commits = append(payload.Commits, logCommitJSON{
			SHA:           shortSHA(c.SHA),
			Message:       c.Message,
			Author:        c.Author,
			Date:          c.Date.Format(time.RFC3339),
			Kind:          string(c.Kind),
			PRNumber:      c.PRNumber,
			StackSize:     c.StackSize,
			StackPRs:      c.StackPRs,
			StackPRTitles: c.StackPRTitles,
			StackScope:    c.StackScope,
		})
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	out.Print(string(data))
	out.Newline()
	return nil
}
