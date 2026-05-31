package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// =============================================================================
// Attach Operations
// =============================================================================

func TestWorktreeAttach(t *testing.T) {
	t.Parallel()

	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("attach existing stack to worktree", func(t *testing.T, sh *TestShell) {
		// Create a regular stack (not in a worktree)
		sh.WriteFile("feature.txt", "feature content").
			Run("create feature -m 'feature branch'")

		// Should be on the feature branch
		sh.OnBranch("feature")

		// Attach the stack to a worktree
		sh.Run("worktree attach feature")

		// Should return to main after attaching
		sh.OnBranch("main")

		// Verify worktree was created
		sh.Run("worktree list").
			OutputContains("feature")

		// Get worktree path and verify it exists
		worktreePath := sh.GetWorktreePath("feature")
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Errorf("Worktree directory should exist at %s", worktreePath)
		}

		// Verify the branch is checked out in the worktree
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("feature")
	})

	run("attach with custom name", func(t *testing.T, sh *TestShell) {
		// Create a stack
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		// Attach with custom name
		sh.Run("worktree attach feature --name my-wt")

		// Verify worktree was created with custom name
		sh.Run("worktree list").
			OutputContains("my-wt")

		// Verify the path uses the custom name (use worktree open to get path)
		sh.Run("worktree open my-wt")
		worktreePath := strings.TrimSpace(sh.Output())
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Errorf("Worktree directory should exist at %s", worktreePath)
		}
		// The path should contain "my-wt" as the directory name
		if !strings.Contains(worktreePath, "my-wt") {
			t.Errorf("Worktree path should contain custom name 'my-wt', got: %s", worktreePath)
		}
	})

	run("attach child branch attaches entire stack", func(_ *testing.T, sh *TestShell) {
		// Create a stack with multiple branches
		sh.WriteFile("root.txt", "root").
			Run("create stack-root -m 'stack root'")
		sh.WriteFile("child.txt", "child").
			Run("create child -m 'child branch'")

		// Attach by specifying the child branch
		sh.Run("worktree attach child")

		// Verify worktree was created with stack root name
		sh.Run("worktree list").
			OutputContains("stack-root")

		// Verify the worktree exists and has the stack root checked out
		worktreePath := sh.GetWorktreePath("stack-root")
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("stack-root")

		// Both branches should be accessible in the worktree
		sh.HasBranches("main", "stack-root", "child")
	})

	run("attach preserves unrelated current branch", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("a.txt", "a").
			Run("create stack-a -m 'stack a'")
		sh.Checkout("main")
		sh.WriteFile("b.txt", "b").
			Run("create stack-b -m 'stack b'")

		sh.Run("worktree attach stack-a")

		sh.OnBranch("stack-b")
		sh.Run("worktree list").
			OutputContains("stack-a")
	})

	run("attach fails for untracked branch", func(_ *testing.T, sh *TestShell) {
		// Create a regular git branch (not tracked by stackit)
		sh.Git("checkout -b untracked-branch")
		sh.WriteFile("file.txt", "content")
		sh.Git("add file.txt")
		sh.Git("commit -m 'commit'")

		// Attach should fail
		sh.RunExpectError("worktree attach untracked-branch")
	})

	run("attach fails for non-existent branch", func(_ *testing.T, sh *TestShell) {
		sh.RunExpectError("worktree attach nonexistent")
	})

	run("attach fails for branch already in worktree", func(_ *testing.T, sh *TestShell) {
		// Create a stack with worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		// Try to attach again - should fail
		sh.RunExpectError("worktree attach feature")
	})

	run("attach fails from inside worktree", func(_ *testing.T, sh *TestShell) {
		// Create a worktree
		sh.WriteFile("wt1.txt", "wt1").
			Run("create wt1 -w -m 'worktree 1'")

		worktreePath := sh.GetWorktreePath("wt1")
		shW := sh.InWorktree(worktreePath)

		// Create another stack in main repo
		sh.WriteFile("stack2.txt", "stack2").
			Run("create stack2 -m 'stack 2'")

		// Try to attach from inside worktree - should fail
		shW.RunExpectError("worktree attach stack2")
	})
}

// =============================================================================
// Detach Operations
// =============================================================================

func TestWorktreeDetach(t *testing.T) {
	t.Parallel()

	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("detach preserves branches from attached worktree", func(t *testing.T, sh *TestShell) {
		// Create a regular stack
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")
		sh.WriteFile("child.txt", "child").
			Run("create child -m 'child branch'")

		// Attach to worktree
		sh.Run("worktree attach feature")

		worktreePath := sh.GetWorktreePath("feature")

		// Detach the worktree
		sh.Run("worktree detach feature")

		// Worktree should be removed
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Errorf("Worktree directory should be removed at %s", worktreePath)
		}

		// Worktree should not be listed
		sh.Run("worktree list")
		output := sh.Output()
		if strings.Contains(output, "feature") && !strings.Contains(output, "No managed worktrees") {
			t.Errorf("Worktree should be removed from list")
		}

		// Branches should still exist
		sh.HasBranches("main", "feature", "child")

		// Stack structure should be preserved
		sh.ExpectStackStructure(map[string]string{
			"feature": "main",
			"child":   "feature",
		})
	})

	run("detach created worktree reparents children and deletes anchor", func(t *testing.T, sh *TestShell) {
		// Create worktree with wt create (creates anchor branch)
		sh.Run("worktree create my-wt")

		// Get worktree path using worktree open (works with name, not anchor branch)
		sh.Run("worktree open my-wt")
		worktreePath := strings.TrimSpace(sh.Output())
		shW := sh.InWorktree(worktreePath)

		// Create a child branch in the worktree
		shW.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		// Detach the worktree
		sh.Run("worktree detach my-wt")

		// Worktree should be removed
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Errorf("Worktree directory should be removed at %s", worktreePath)
		}

		// The anchor branch should be deleted (it was a worktree-anchor type)
		branches, _ := sh.Scene().Repo.GetLocalBranches()
		for _, b := range branches {
			if strings.Contains(b, "my-wt") && strings.Contains(b, "-wt") {
				t.Errorf("Anchor branch should be deleted, but found: %s", b)
			}
		}

		// Feature branch should still exist and be reparented to main
		sh.HasBranches("main", "feature")
		sh.ExpectBranchParent("feature", "main")
	})

	run("detach with dirty worktree requires --force", func(t *testing.T, sh *TestShell) {
		for _, tt := range []struct {
			name     string
			cmd      string
			wantGone bool
		}{
			{"fails without force", "worktree detach feature", false},
			{"removes with force", "worktree detach feature --force", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				sh := sh.WithT(t)
				sh.ResetRepo()
				sh.WriteFile("feature.txt", "feature").Run("create feature -m 'feature branch'")
				sh.Run("worktree attach feature")
				worktreePath := sh.GetWorktreePath("feature")
				MakeWorktreeDirty(t, worktreePath)

				if tt.wantGone {
					sh.Run(tt.cmd)
					if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
						t.Errorf("worktree directory should be removed at %s", worktreePath)
					}
					sh.HasBranches("main", "feature")
				} else {
					sh.RunExpectError(tt.cmd)
					if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
						t.Errorf("worktree should still exist at %s", worktreePath)
					}
				}
			})
		}
	})

	run("detach fails from inside worktree", func(_ *testing.T, sh *TestShell) {
		// Create a stack and attach
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")
		sh.Run("worktree attach feature")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Try to detach from inside the worktree - should fail
		shW.RunExpectError("worktree detach feature")
	})

	run("detach non-existent worktree fails", func(_ *testing.T, sh *TestShell) {
		sh.RunExpectError("worktree detach nonexistent")
	})
}

// =============================================================================
// Attach/Detach Round-Trip
// =============================================================================

func TestWorktreeAttachDetachRoundTrip(t *testing.T) {
	t.Parallel()

	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("attach detach attach cycle works", func(_ *testing.T, sh *TestShell) {
		// Create a stack
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		// Attach
		sh.Run("worktree attach feature")
		sh.Run("worktree list").OutputContains("feature")

		// Detach
		sh.Run("worktree detach feature")
		sh.Run("worktree list").OutputContains("No managed worktrees")

		// Attach again (should work)
		sh.Run("worktree attach feature")
		sh.Run("worktree list").OutputContains("feature")

		// Verify worktree works
		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("feature")
	})

	run("work in attached worktree then detach preserves work", func(_ *testing.T, sh *TestShell) {
		// Create a stack
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		// Attach to worktree
		sh.Run("worktree attach feature")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Do work in the worktree - create child branches
		shW.WriteFile("child1.txt", "child1").
			Run("create child1 -m 'child 1'")
		shW.WriteFile("child2.txt", "child2").
			Run("create child2 -m 'child 2'")

		// Detach
		sh.Run("worktree detach feature")

		// All branches should be preserved
		sh.HasBranches("main", "feature", "child1", "child2")

		// Stack structure should be preserved
		sh.ExpectStackStructure(map[string]string{
			"feature": "main",
			"child1":  "feature",
			"child2":  "child1",
		})
	})
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestWorktreeAttachDetachEdgeCases(t *testing.T) {
	t.Parallel()

	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("attach stack with deep hierarchy", func(_ *testing.T, sh *TestShell) {
		// Create deep stack
		sh.WriteFile("a.txt", "a").Run("create a -m 'branch a'")
		sh.WriteFile("b.txt", "b").Run("create b -m 'branch b'")
		sh.WriteFile("c.txt", "c").Run("create c -m 'branch c'")
		sh.WriteFile("d.txt", "d").Run("create d -m 'branch d'")

		// Attach from the leaf
		sh.Run("worktree attach d")

		// Should attach at stack root
		worktreePath := sh.GetWorktreePath("a")
		shW := sh.InWorktree(worktreePath)

		// Verify we can navigate the full stack in worktree
		shW.OnBranch("a")
		shW.Checkout("d").OnBranch("d")
		shW.Run("down").OnBranch("c")
		shW.Run("down").OnBranch("b")
		shW.Run("down").OnBranch("a")
	})

	run("detach worktree with branch checked out elsewhere fails gracefully", func(t *testing.T, sh *TestShell) {
		// Create a stack
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		// Attach to worktree
		sh.Run("worktree attach feature")

		// Checkout the same branch in main repo (this creates a conflict)
		sh.Checkout("feature")

		_ = sh.GetWorktreePath("feature") // Verify worktree exists

		// Detach should still work (or fail gracefully)
		// The git worktree remove might fail because the branch is checked out elsewhere
		// but the unregistration should still happen
		sh.Run("worktree detach feature --force")

		// Regardless of git errors, the worktree should be unregistered
		sh.Run("worktree list")
		output := sh.Output()
		if strings.Contains(output, "feature") && !strings.Contains(output, "No managed worktrees") {
			t.Errorf("Worktree should be unregistered from list")
		}
	})
}

func TestWorktreeRepair(t *testing.T) {
	t.Parallel()

	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("repair converts legacy registration to hidden anchor", func(t *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		legacyPath := t.TempDir()
		writeWorktreeRegistration(t, sh, "feature", map[string]any{
			"name":        "legacy-wt",
			"path":        legacyPath,
			"stackRoot":   "feature",
			"mainRepoDir": sh.Scene().Dir,
		})

		sh.Run("worktree list").OutputContains("repair")
		sh.Run("worktree repair legacy-wt").
			OutputContains("converted legacy registration to hidden anchor")

		sh.Run("worktree list --json")
		var result struct {
			Worktrees []struct {
				Name              string   `json:"name"`
				AnchorBranch      string   `json:"anchor_branch"`
				RegistrationState string   `json:"registration_state"`
				RootBranches      []string `json:"root_branches"`
				NeedsRepair       bool     `json:"needs_repair"`
			} `json:"worktrees"`
		}
		if err := json.Unmarshal([]byte(sh.Output()), &result); err != nil {
			t.Fatalf("failed to parse JSON output: %v\n%s", err, sh.Output())
		}
		if len(result.Worktrees) != 1 {
			t.Fatalf("expected one worktree, got %d", len(result.Worktrees))
		}
		const featureRoot = "feature"
		if result.Worktrees[0].AnchorBranch == featureRoot {
			t.Fatalf("expected repair to move registration to a hidden anchor")
		}
		if result.Worktrees[0].RegistrationState != "ok" {
			t.Fatalf("expected repaired registration to be ok, got %s", result.Worktrees[0].RegistrationState)
		}
		if result.Worktrees[0].NeedsRepair {
			t.Fatalf("expected repaired registration not to need repair")
		}
		if len(result.Worktrees[0].RootBranches) != 1 || result.Worktrees[0].RootBranches[0] != featureRoot {
			t.Fatalf("expected feature root branch after repair, got %#v", result.Worktrees[0].RootBranches)
		}
	})

	run("repair removes stale registration for missing directory", func(t *testing.T, sh *TestShell) {
		writeWorktreeRegistration(t, sh, "missing-anchor", map[string]any{
			"name":        "stale-wt",
			"path":        t.TempDir() + "/gone",
			"stackRoot":   "missing-anchor",
			"mainRepoDir": sh.Scene().Dir,
		})

		sh.Run("worktree repair stale-wt").
			OutputContains("removed stale registration")

		sh.Run("worktree list")
		if !strings.Contains(sh.Output(), "No managed worktrees") {
			t.Fatalf("expected stale registration to be removed, got:\n%s", sh.Output())
		}
	})
}

func writeWorktreeRegistration(t *testing.T, sh *TestShell, refName string, meta map[string]any) {
	t.Helper()

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("failed to marshal worktree metadata: %v", err)
	}

	hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	hashCmd.Dir = sh.Scene().Dir
	hashCmd.Stdin = strings.NewReader(string(data))
	shaBytes, err := hashCmd.Output()
	if err != nil {
		t.Fatalf("failed to write worktree metadata blob: %v", err)
	}
	sha := strings.TrimSpace(string(shaBytes))

	if err := sh.Scene().Repo.RunGitCommand("update-ref", "refs/stackit/worktrees/"+refName, sha); err != nil {
		t.Fatalf("failed to update worktree ref: %v", err)
	}
}
