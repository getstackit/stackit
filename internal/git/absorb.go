package git

import (
	"strings"
)

// HunkTarget represents a hunk and its target commit
type HunkTarget struct {
	Hunk        Hunk
	CommitSHA   string
	CommitIndex int // Index in the commit list (0 = newest)
}

// Overlaps reports whether two hunks have overlapping line ranges.
// It includes a safety margin to account for git context lines.
func (h Hunk) Overlaps(other Hunk) bool {
	if h.File != other.File {
		return false
	}

	// Add safety margin of 3 lines (typical git context) to avoid conflicts
	margin := 3

	hStart := h.OldStart - margin
	hEnd := h.OldStart + h.OldCount + margin
	otherStart := other.NewStart
	otherEnd := other.NewStart + other.NewCount

	overlap := hStart <= otherEnd && otherStart <= hEnd
	return overlap
}

// parseDiffHunks parses a diff output and extracts hunks for a specific file
func parseDiffHunks(diffOutput, targetFile string) []Hunk {
	if strings.TrimSpace(diffOutput) == "" {
		return []Hunk{}
	}

	var hunks []Hunk
	lines := strings.Split(diffOutput, "\n")

	var currentHunk *Hunk
	var currentFile string
	var hunkLines []string

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "diff --git") {
			if currentHunk != nil {
				currentHunk.Content = strings.Join(hunkLines, "\n")
				if currentHunk.File == targetFile {
					hunks = append(hunks, *currentHunk)
				}
				currentHunk = nil
				hunkLines = nil
			}
			parts := strings.Split(line, " ")
			if len(parts) >= 4 {
				bPath := parts[len(parts)-1]
				if after, ok := strings.CutPrefix(bPath, "b/"); ok {
					currentFile = after
				}
			}
			continue
		}

		if match := hunkHeaderRegex.FindStringSubmatch(line); match != nil {
			if currentHunk != nil {
				currentHunk.Content = strings.Join(hunkLines, "\n")
				if currentHunk.File == targetFile {
					hunks = append(hunks, *currentHunk)
				}
			}

			oldStart := parseInt(match[1])
			oldCount := parseInt(match[2])
			if oldCount == 0 {
				oldCount = 1
			}
			newStart := parseInt(match[3])
			newCount := parseInt(match[4])
			if newCount == 0 {
				newCount = 1
			}

			currentHunk = &Hunk{
				File:     currentFile,
				OldStart: oldStart,
				OldCount: oldCount,
				NewStart: newStart,
				NewCount: newCount,
			}
			hunkLines = []string{line}
			continue
		}

		if currentHunk != nil {
			hunkLines = append(hunkLines, line)
		}
	}

	if currentHunk != nil {
		currentHunk.Content = strings.Join(hunkLines, "\n")
		if currentHunk.File == targetFile {
			hunks = append(hunks, *currentHunk)
		}
	}

	return hunks
}
