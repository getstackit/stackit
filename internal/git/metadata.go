package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const (
	// MetadataRefPrefix is the prefix for Git refs where branch metadata is stored
	MetadataRefPrefix = "refs/stackit/metadata/"
)

// Meta represents branch metadata stored in Git refs
type Meta struct {
	ParentBranchName     *string `json:"parentBranchName,omitempty"`
	ParentBranchRevision *string `json:"parentBranchRevision,omitempty"`
	PrInfo               *PrInfo `json:"prInfo,omitempty"`
	Scope                *string `json:"scope,omitempty"`
}

// PrInfo represents PR information
type PrInfo struct {
	Number  *int    `json:"number,omitempty"`
	Base    *string `json:"base,omitempty"`
	URL     *string `json:"url,omitempty"`
	Title   *string `json:"title,omitempty"`
	Body    *string `json:"body,omitempty"`
	State   *string `json:"state,omitempty"`
	IsDraft *bool   `json:"isDraft,omitempty"`
}

// ReadMetadataRef reads metadata for a branch from Git refs
func ReadMetadataRef(branchName string) (*Meta, error) {
	return ReadMetadataRefInDir("", branchName)
}

// ReadMetadataRefInDir reads metadata for a branch from Git refs in a specific directory
func ReadMetadataRefInDir(dir, branchName string) (*Meta, error) {
	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)

	// Get the content of the ref using git cat-file
	// This is more reliable than go-git for parallel reads and "read-your-own-writes"
	var content string
	var err error
	if dir == "" {
		content, err = RunGitCommand("cat-file", "-p", refName)
	} else {
		content, err = RunGitCommandInDir(dir, "cat-file", "-p", refName)
	}
	if err != nil {
		// If the ref doesn't exist, return empty meta
		// We check for "exists" specifically to distinguish from other errors
		var existsErr error
		if dir == "" {
			_, existsErr = RunGitCommand("rev-parse", "--verify", refName)
		} else {
			_, existsErr = RunGitCommandInDir(dir, "rev-parse", "--verify", refName)
		}
		if existsErr != nil {
			return &Meta{}, nil //nolint:nilerr
		}
		return nil, fmt.Errorf("failed to read metadata ref %s: %w", refName, err)
	}

	if content == "" {
		return &Meta{}, nil
	}

	var meta Meta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata for %s: %w", branchName, err)
	}

	return &meta, nil
}

// GetMetadataRefList returns all metadata refs
func GetMetadataRefList() (map[string]string, error) {
	result := make(map[string]string)

	// Get current metadata refs
	output, err := RunGitCommand("for-each-ref", "--format=%(refname) %(objectname)", MetadataRefPrefix)
	if err == nil && output != "" {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			if len(parts) != 2 {
				continue
			}
			refName := parts[0]
			sha := parts[1]

			if strings.HasPrefix(refName, MetadataRefPrefix) {
				branchName := refName[len(MetadataRefPrefix):]
				result[branchName] = sha
			}
		}
	}

	return result, nil
}

// DeleteMetadataRef deletes a metadata ref for a branch
func DeleteMetadataRef(branchName string) error {
	return DeleteMetadataRefInDir("", branchName)
}

// DeleteMetadataRefInDir deletes a metadata ref for a branch in a specific directory
func DeleteMetadataRefInDir(dir, branchName string) error {
	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
	var err error
	if dir == "" {
		_, err = RunGitCommand("update-ref", "-d", refName)
	} else {
		_, err = RunGitCommandInDir(dir, "update-ref", "-d", refName)
	}

	return err
}

// WriteMetadataRef writes metadata for a branch to Git refs
func WriteMetadataRef(branchName string, meta *Meta) error {
	return WriteMetadataRefInDir("", branchName, meta)
}

// WriteMetadataRefInDir writes metadata for a branch to Git refs in a specific directory
func WriteMetadataRefInDir(dir, branchName string, meta *Meta) error {
	// Marshal metadata to JSON
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Create blob using git hash-object
	var sha string
	if dir == "" {
		sha, err = RunGitCommandWithInput(string(jsonData), "hash-object", "-w", "--stdin")
	} else {
		// For hash-object with input, we need to use the runner approach
		runner := NewRunner(dir)
		sha, err = runner.runInternal(context.Background(), string(jsonData), true, "hash-object", "-w", "--stdin")
	}
	if err != nil {
		return fmt.Errorf("failed to create metadata blob: %w", err)
	}

	// Create or update the ref using git update-ref
	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
	if dir == "" {
		_, err = RunGitCommand("update-ref", refName, sha)
	} else {
		_, err = RunGitCommandInDir(dir, "update-ref", refName, sha)
	}
	if err != nil {
		return fmt.Errorf("failed to write metadata ref: %w", err)
	}

	return nil
}

// RenameMetadataRef renames a metadata ref from one branch name to another
func RenameMetadataRef(oldBranchName, newBranchName string) error {
	oldRefName := fmt.Sprintf("%s%s", MetadataRefPrefix, oldBranchName)
	newRefName := fmt.Sprintf("%s%s", MetadataRefPrefix, newBranchName)

	// Get the SHA of the old ref
	sha, err := RunGitCommand("rev-parse", "--verify", oldRefName)
	if err != nil {
		return nil //nolint:nilerr // Nothing to rename
	}

	// Create the new ref
	_, err = RunGitCommand("update-ref", newRefName, sha)
	if err != nil {
		return fmt.Errorf("failed to create new metadata ref: %w", err)
	}

	// Delete the old ref
	_, err = RunGitCommand("update-ref", "-d", oldRefName)
	if err != nil {
		return fmt.Errorf("failed to delete old metadata ref: %w", err)
	}

	return nil
}

// BatchReadMetadataRefs reads metadata for multiple branches in parallel
// Returns a map of branchName -> *Meta and a map of branchName -> error for failed reads
func BatchReadMetadataRefs(branchNames []string) (map[string]*Meta, map[string]error) {
	results := make(map[string]*Meta)
	errs := make(map[string]error)
	resultsMu := sync.Mutex{}
	errsMu := sync.Mutex{}
	var wg sync.WaitGroup

	for _, branchName := range branchNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			meta, err := ReadMetadataRef(name)
			if err != nil {
				errsMu.Lock()
				errs[name] = err
				errsMu.Unlock()
				return
			}
			resultsMu.Lock()
			results[name] = meta
			resultsMu.Unlock()
		}(branchName)
	}

	wg.Wait()
	return results, errs
}
