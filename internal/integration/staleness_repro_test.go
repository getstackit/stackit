package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
)

func TestReadMetadataRefStaleness(t *testing.T) {
	// This test tries to reproduce the "read-your-own-writes" issue between git CLI and go-git.
	binaryPath := getStackitBinary(t)
	sh := NewTestShell(t, binaryPath)

	// 1. Create a branch and metadata
	sh.Run("create feature-a -m 'feat: a'")

	// DEBUG: list all refs
	sh.Git("for-each-ref refs/stackit/metadata/").Log(sh.Output())

	// 2. Read metadata via engine/go-git
	meta, err := git.ReadMetadataRefInDir(sh.Dir(), "feature-a")
	require.NoError(t, err)
	require.NotNil(t, meta.ParentBranchName)
	require.Equal(t, "main", *meta.ParentBranchName)

	// 3. Update metadata via CLI (simulating SetParent in Move command)
	newParent := "some-other-branch"
	meta.ParentBranchName = &newParent
	err = git.WriteMetadataRefInDir(sh.Dir(), "feature-a", meta) // This uses CLI internally
	require.NoError(t, err)

	// 4. Read metadata again via engine/go-git (simulating RestackBranch in Move command)
	meta2, err := git.ReadMetadataRefInDir(sh.Dir(), "feature-a")
	require.NoError(t, err)

	if meta2.ParentBranchName == nil || *meta2.ParentBranchName != newParent {
		t.Errorf("STALENESS DETECTED: expected parent %s, got %v", newParent, meta2.ParentBranchName)
	}
}
