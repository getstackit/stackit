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

	var currentHunk *Hunk
	var currentFile string
	var hunkLines []string

	for line := range strings.SplitSeq(diffOutput, "\n") {
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
			if path, ok := diffGitHeaderPath(line); ok {
				currentFile = path
			}
			continue
		}

		if header, ok := parseHunkHeader(line); ok {
			if currentHunk != nil {
				currentHunk.Content = strings.Join(hunkLines, "\n")
				if currentHunk.File == targetFile {
					hunks = append(hunks, *currentHunk)
				}
			}

			currentHunk = &Hunk{
				File:     currentFile,
				OldStart: header.OldStart,
				OldCount: header.OldCount,
				NewStart: header.NewStart,
				NewCount: header.NewCount,
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
