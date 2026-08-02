package git

import (
	"fmt"
	"regexp"
	"strings"
)

// hunkHeaderWithSuffixRegex matches hunk headers and captures any trailing
// section-heading text after the closing "@@" (e.g. "@@ -1,2 +1,3 @@ func Foo()").
var hunkHeaderWithSuffixRegex = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// CanSplit returns true if the hunk has context lines between changes,
// meaning it can be split into multiple smaller hunks.
// A hunk is splittable only when there's at least one context line that separates
// two distinct groups of changes (additions/deletions).
func (h Hunk) CanSplit() bool {
	lines := strings.Split(h.Content, "\n")

	// Track state: we need to find the pattern:
	// change(s) -> context(s) -> change(s)
	inChangeBlock := false
	foundContextAfterChange := false

	for _, line := range lines {
		// Skip empty lines
		if line == "" {
			continue
		}
		// Skip hunk header
		if strings.HasPrefix(line, "@@") {
			continue
		}

		isChange := strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")
		isContext := strings.HasPrefix(line, " ")

		if isChange {
			if foundContextAfterChange {
				// We found: change -> context -> change
				// This means the hunk is splittable
				return true
			}
			inChangeBlock = true
		} else if isContext {
			if inChangeBlock {
				// We're transitioning from changes to context
				foundContextAfterChange = true
			}
		}
	}
	return false
}

// Split divides a hunk at the first context line after changes.
// Returns the split hunks or an error if the hunk cannot be split.
func (h Hunk) Split() (Hunks, error) {
	if !h.CanSplit() {
		return Hunks{h}, nil
	}

	lines := strings.Split(h.Content, "\n")

	var headerLine string
	var headerSuffix string
	var contentLines []string
	origOldStart, origNewStart := h.OldStart, h.NewStart

	for i, line := range lines {
		if strings.HasPrefix(line, "@@") {
			headerLine = line
			match := hunkHeaderWithSuffixRegex.FindStringSubmatch(line)
			if len(match) > 5 {
				headerSuffix = match[5]
			}
			contentLines = lines[i+1:]
			break
		}
	}

	if headerLine == "" {
		return Hunks{h}, nil
	}

	// Find the split point - first context line after changes that has more changes after it
	splitIndex := findSplitPoint(contentLines)
	if splitIndex < 0 {
		return Hunks{h}, nil
	}

	// Split the content
	firstContent := contentLines[:splitIndex]
	secondContent := contentLines[splitIndex:]

	// Calculate line counts for first hunk
	firstOldCount, firstNewCount := countLines(firstContent)
	firstOldStart := origOldStart
	firstNewStart := origNewStart

	// Calculate line counts for second hunk
	secondOldCount, secondNewCount := countLines(secondContent)
	secondOldStart := firstOldStart + firstOldCount
	secondNewStart := firstNewStart + firstNewCount

	// Build first hunk
	firstHeader := fmt.Sprintf("@@ -%d,%d +%d,%d @@%s", firstOldStart, firstOldCount, firstNewStart, firstNewCount, headerSuffix)
	firstHunk := Hunk{
		File:      h.File,
		OldStart:  firstOldStart,
		OldCount:  firstOldCount,
		NewStart:  firstNewStart,
		NewCount:  firstNewCount,
		Content:   firstHeader + "\n" + strings.Join(firstContent, "\n"),
		IndexLine: h.IndexLine,
	}

	// Build second hunk
	secondHeader := fmt.Sprintf("@@ -%d,%d +%d,%d @@", secondOldStart, secondOldCount, secondNewStart, secondNewCount)
	secondHunk := Hunk{
		File:      h.File,
		OldStart:  secondOldStart,
		OldCount:  secondOldCount,
		NewStart:  secondNewStart,
		NewCount:  secondNewCount,
		Content:   secondHeader + "\n" + strings.Join(secondContent, "\n"),
		IndexLine: h.IndexLine,
	}

	// Recursively split if possible
	moreHunks, _ := secondHunk.Split()
	result := make(Hunks, 0, 1+len(moreHunks))
	result = append(result, firstHunk)
	result = append(result, moreHunks...)

	return result, nil
}

// findSplitPoint finds the index of the first context line that should start the second hunk
func findSplitPoint(lines []string) int {
	hasSeenChange := false
	contextStartIndex := -1

	for i, line := range lines {
		if line == "" {
			continue
		}

		isChange := strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")
		isContext := strings.HasPrefix(line, " ")

		if isChange {
			hasSeenChange = true
			contextStartIndex = -1 // Reset since we found another change
		} else if isContext && hasSeenChange {
			if contextStartIndex < 0 {
				contextStartIndex = i
			}
			// Look ahead for more changes
			for j := i + 1; j < len(lines); j++ {
				nextLine := lines[j]
				if nextLine == "" {
					continue
				}
				nextIsChange := strings.HasPrefix(nextLine, "+") || strings.HasPrefix(nextLine, "-")
				if nextIsChange {
					// Found changes after context - split here
					return contextStartIndex
				}
			}
		}
	}

	return -1
}

// countLines counts the old and new line counts for hunk content
func countLines(lines []string) (oldCount, newCount int) {
	for _, line := range lines {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			newCount++
		case strings.HasPrefix(line, "-"):
			oldCount++
		case strings.HasPrefix(line, " "):
			oldCount++
			newCount++
		}
	}
	return oldCount, newCount
}

// Preview returns a short preview of the hunk content.
// maxLines specifies the maximum number of content lines to show.
func (h Hunk) Preview(maxLines int) (preview string, totalLines int, hasMore bool) {
	lines := strings.Split(h.Content, "\n")
	contentLines := make([]string, 0, len(lines))

	for _, line := range lines {
		// Skip empty lines
		if line == "" {
			continue
		}
		// Skip hunk header
		if strings.HasPrefix(line, "@@") {
			continue
		}
		contentLines = append(contentLines, line)
	}

	totalLines = len(contentLines)
	if totalLines <= maxLines {
		return strings.Join(contentLines, "\n"), totalLines, false
	}

	return strings.Join(contentLines[:maxLines], "\n"), totalLines, true
}

// Header extracts just the @@ header line from the hunk.
func (h Hunk) Header() string {
	lines := strings.SplitSeq(h.Content, "\n")
	for line := range lines {
		if strings.HasPrefix(line, "@@") {
			return line
		}
	}
	// Fallback: generate header from hunk metadata
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
}
