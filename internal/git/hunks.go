package git

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Hunk represents a single hunk of changes in a diff
type Hunk struct {
	File          string // File path
	OldStart      int    // Line number in old file (1-indexed)
	OldCount      int    // Number of lines in old file
	NewStart      int    // Line number in new file (1-indexed)
	NewCount      int    // Number of lines in new file
	Content       string // The actual diff content (including header)
	IndexLine     string // The index line from the diff (e.g., "index abc123..def456 100644") for --3way merging
	Binary        bool   // True if this represents a binary file change
	IsNewFile     bool   // True if this hunk is for a newly created file
	IsDeletedFile bool   // True if this hunk is for a deleted file
	FileMode      string // File mode (e.g., "100644", "100755") for new/deleted files
}

// Hunks is an ordered collection of diff hunks.
type Hunks []Hunk

// ParseDiffOutput parses a diff output into structured hunks
func ParseDiffOutput(diffOutput string) (Hunks, error) {
	if diffOutput == "" {
		return Hunks{}, nil
	}

	var hunks Hunks
	lines := strings.Split(diffOutput, "\n")

	// Regex to match hunk headers: @@ -old_start,old_count +new_start,new_count @@
	// Example: @@ -10,5 +10,6 @@
	hunkHeaderRegex := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

	var currentHunk *Hunk
	var currentFile string
	var currentIndexLine string
	var currentIsNewFile bool
	var currentIsDeletedFile bool
	var currentFileMode string
	var hunkLines []string

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		// Check for file header (starts with "diff --git" or "--- a/" or "+++ b/")
		if strings.HasPrefix(line, "diff --git") {
			// Save previous hunk if exists
			if currentHunk != nil {
				currentHunk.Content = strings.Join(hunkLines, "\n")
				hunks = append(hunks, *currentHunk)
				currentHunk = nil
				hunkLines = nil
			}
			// Extract file path from "diff --git a/path b/path"
			// Format: "diff --git a/path/to/file b/path/to/file"
			parts := strings.Split(line, " ")
			if len(parts) >= 4 {
				// parts[2] = "a/path/to/file", parts[3] = "b/path/to/file"
				bPath := parts[len(parts)-1]
				if after, ok := strings.CutPrefix(bPath, "b/"); ok {
					currentFile = after
				}
			}
			// Reset state for new file diff
			currentIndexLine = ""
			currentIsNewFile = false
			currentIsDeletedFile = false
			currentFileMode = ""
			continue
		}

		// Detect new file mode (e.g., "new file mode 100644")
		// This line appears between "diff --git" and "index" for new files
		if after, ok := strings.CutPrefix(line, "new file mode "); ok {
			currentFileMode = after
			currentIsNewFile = true
			continue
		}

		// Detect deleted file mode (e.g., "deleted file mode 100644")
		// This line appears between "diff --git" and "index" for deleted files
		if after, ok := strings.CutPrefix(line, "deleted file mode "); ok {
			currentFileMode = after
			currentIsDeletedFile = true
			continue
		}

		// Capture the index line (e.g., "index abc123..def456 100644")
		// This is needed for --3way merge to work
		if strings.HasPrefix(line, "index ") {
			currentIndexLine = line
			continue
		}

		// Detect new file via "--- /dev/null" as fallback when "new file mode" is missing.
		// This can happen with certain git diff formats (e.g., git diff main..HEAD).
		if line == "--- /dev/null" {
			currentIsNewFile = true
			continue
		}

		// Detect deleted file via "+++ /dev/null" as fallback when "deleted file mode" is missing.
		if line == "+++ /dev/null" {
			currentIsDeletedFile = true
			continue
		}

		// Check for binary file marker
		// Format: "Binary files a/path and b/path differ"
		if strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ") {
			// Save previous hunk if exists
			if currentHunk != nil {
				currentHunk.Content = strings.Join(hunkLines, "\n")
				hunks = append(hunks, *currentHunk)
				currentHunk = nil
				hunkLines = nil
			}
			// Create a binary file hunk
			hunks = append(hunks, Hunk{
				File:      currentFile,
				Content:   line,
				IndexLine: currentIndexLine,
				Binary:    true,
			})
			continue
		}

		// Check for hunk header
		if match := hunkHeaderRegex.FindStringSubmatch(line); match != nil {
			// Save previous hunk if exists
			if currentHunk != nil {
				currentHunk.Content = strings.Join(hunkLines, "\n")
				hunks = append(hunks, *currentHunk)
			}

			// Parse hunk header
			oldStart := parseInt(match[1])
			oldCount := parseInt(match[2])
			if oldCount == 0 {
				oldCount = 1 // Default to 1 if not specified
			}
			newStart := parseInt(match[3])
			newCount := parseInt(match[4])
			if newCount == 0 {
				newCount = 1 // Default to 1 if not specified
			}

			currentHunk = &Hunk{
				File:          currentFile,
				OldStart:      oldStart,
				OldCount:      oldCount,
				NewStart:      newStart,
				NewCount:      newCount,
				IndexLine:     currentIndexLine,
				IsNewFile:     currentIsNewFile,
				IsDeletedFile: currentIsDeletedFile,
				FileMode:      currentFileMode,
			}
			hunkLines = []string{line}
			continue
		}

		// Accumulate hunk content
		if currentHunk != nil {
			hunkLines = append(hunkLines, line)
		}
	}

	// Save last hunk
	if currentHunk != nil {
		currentHunk.Content = strings.Join(hunkLines, "\n")
		hunks = append(hunks, *currentHunk)
	}

	return hunks, nil
}

// parseInt parses a string to int, returns 0 if empty or invalid
func parseInt(s string) int {
	result, _ := strconv.Atoi(s)
	return result
}

// Patch constructs a unified diff patch from the selected hunks.
// The patch can be applied using git apply --cached to stage specific hunks.
// Binary files are handled separately as they have a different diff format.
func (hunks Hunks) Patch() string {
	if len(hunks) == 0 {
		return ""
	}

	// Separate binary and text hunks, group by file
	fileHunks := make(map[string][]Hunk)
	fileBinary := make(map[string]bool)
	fileNewFile := make(map[string]bool)
	fileDeletedFile := make(map[string]bool)
	fileMode := make(map[string]string)
	fileOrder := make([]string, 0)
	for _, h := range hunks {
		if _, exists := fileHunks[h.File]; !exists {
			fileOrder = append(fileOrder, h.File)
		}
		fileHunks[h.File] = append(fileHunks[h.File], h)
		if h.Binary {
			fileBinary[h.File] = true
		}
		if h.IsNewFile {
			fileNewFile[h.File] = true
			fileMode[h.File] = h.FileMode
		}
		if h.IsDeletedFile {
			fileDeletedFile[h.File] = true
			fileMode[h.File] = h.FileMode
		}
	}

	var sb strings.Builder

	// Build patch for each file
	for _, file := range fileOrder {
		hunksForFile := fileHunks[file]
		isNewFile := fileNewFile[file]
		isDeletedFile := fileDeletedFile[file]

		// Write diff header
		fmt.Fprintf(&sb, "diff --git a/%s b/%s\n", file, file)

		// Add new file mode line if this is a new file
		if isNewFile {
			mode := fileMode[file]
			if mode == "" {
				mode = "100644" // default mode for regular files
			}
			fmt.Fprintf(&sb, "new file mode %s\n", mode)
		}

		// Add deleted file mode line if this is a deleted file
		if isDeletedFile {
			mode := fileMode[file]
			if mode == "" {
				mode = "100644" // default mode for regular files
			}
			fmt.Fprintf(&sb, "deleted file mode %s\n", mode)
		}

		// Add index line if available (needed for --3way)
		if hunksForFile[0].IndexLine != "" {
			sb.WriteString(hunksForFile[0].IndexLine + "\n")
		}

		// Binary files have a different format - no ---/+++ lines
		if fileBinary[file] {
			// Binary files just have the "Binary files ... differ" line
			for _, h := range hunksForFile {
				sb.WriteString(h.Content)
				if !strings.HasSuffix(h.Content, "\n") {
					sb.WriteString("\n")
				}
			}
			continue
		}

		// Handle ---/+++ lines based on file type
		switch {
		case isNewFile:
			// New files use /dev/null as the old file path
			sb.WriteString("--- /dev/null\n")
			fmt.Fprintf(&sb, "+++ b/%s\n", file)
		case isDeletedFile:
			// Deleted files use /dev/null as the new file path
			fmt.Fprintf(&sb, "--- a/%s\n", file)
			sb.WriteString("+++ /dev/null\n")
		default:
			// Modified files use normal paths
			fmt.Fprintf(&sb, "--- a/%s\n", file)
			fmt.Fprintf(&sb, "+++ b/%s\n", file)
		}

		// Write each hunk
		for _, h := range hunksForFile {
			sb.WriteString(h.Content)
			// Ensure content ends with newline
			if !strings.HasSuffix(h.Content, "\n") {
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// GenerateNewFileHunk creates a synthetic hunk for an untracked file.
// This allows new files to be included in hunk-based splitting.
func GenerateNewFileHunk(filePath string, content []byte) Hunk {
	lines := strings.Split(string(content), "\n")
	// Remove trailing empty line if present (file ends with newline)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		sb.WriteString("+" + line + "\n")
	}

	return Hunk{
		File:      filePath,
		OldStart:  0,
		OldCount:  0,
		NewStart:  1,
		NewCount:  len(lines),
		Content:   sb.String(),
		IsNewFile: true,
		FileMode:  "100644",
	}
}

// CountHunkLines returns the number of added and removed lines in a hunk
func (h Hunk) LineCounts() (added, removed int) {
	lines := strings.SplitSeq(h.Content, "\n")
	for line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed++
		}
	}
	return added, removed
}

// BuildPatchFromHunks is retained for callers that have not migrated to Hunks.Patch.
func BuildPatchFromHunks(hunks []Hunk) string { return Hunks(hunks).Patch() }

// CountHunkLines is retained for callers that have not migrated to Hunk.LineCounts.
func CountHunkLines(h Hunk) (added, removed int) { return h.LineCounts() }
