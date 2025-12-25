package integration

import (
	"fmt"
	"sync"
	"testing"

	"stackit.dev/stackit/internal/git"
)

func TestParallelMetadataRead(t *testing.T) {
	// This test attempts to trigger race conditions in go-git when reading metadata in parallel.
	binaryPath := getStackitBinary(t)
	sh := NewTestShell(t, binaryPath)

	// 1. Create many branches with metadata
	numBranches := 20
	for i := 0; i < numBranches; i++ {
		branchName := fmt.Sprintf("branch-%d", i)
		sh.Run(fmt.Sprintf("create %s -m 'feat: %d'", branchName, i))
		sh.Checkout(mainBranchName)
	}

	// 2. Read metadata in parallel many times
	var wg sync.WaitGroup
	numIterations := 50
	errors := make(chan error, numBranches*numIterations)

	for i := 0; i < numIterations; i++ {
		for j := 0; j < numBranches; j++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				branchName := fmt.Sprintf("branch-%d", idx)
				meta, err := git.ReadMetadataRefInDir(sh.Dir(), branchName)
				if err != nil {
					errors <- fmt.Errorf("branch %s: unexpected error: %w", branchName, err)
					return
				}
				if meta.ParentBranchName == nil {
					errors <- fmt.Errorf("branch %s: metadata lost! ParentBranchName is nil", branchName)
					return
				}
				if *meta.ParentBranchName != mainBranchName {
					errors <- fmt.Errorf("branch %s: incorrect parent: %s", branchName, *meta.ParentBranchName)
				}
			}(j)
		}
	}

	wg.Wait()
	close(errors)

	errCount := 0
	for err := range errors {
		t.Log(err)
		errCount++
	}

	if errCount > 0 {
		t.Errorf("Detected %d metadata read failures in parallel", errCount)
	} else {
		sh.Log("No parallel read failures detected.")
	}
}
