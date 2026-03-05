package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// cmdAdd is shared by the git passthrough lists and the `notes add` command.
const cmdAdd = "add"

var gitCommandAllowlist = []string{
	cmdAdd,
	"am",
	"apply",
	"archive",
	"bisect",
	"blame",
	"bundle",
	"cherry-pick",
	"clean",
	"clone",
	"commit",
	"diff",
	"difftool",
	"fetch",
	"format-patch",
	"fsck",
	"grep",
	// "merge" removed - stackit has its own merge command
	"mv",
	// "notes" removed - stackit has its own notes command
	"pull",
	"push",
	"range-diff",
	"rebase",
	"reflog",
	"remote",
	"request-pull",
	cmdReset,
	"restore",
	"revert",
	"rm",
	"show",
	"send-email",
	"sparse-checkout",
	"stash",
	"status",
	"submodule",
	"switch",
	"tag",
}

var modifyingGitCommands = []string{
	cmdAdd,
	"am",
	"apply",
	"cherry-pick",
	"clean",
	"commit",
	"mv",
	"pull",
	"rebase",
	cmdReset,
	"restore",
	"revert",
	"rm",
	"stash",
}

// HandlePassthrough checks if the command should be passed through to git
// and executes it if so. Returns true if the command was handled (and the program should exit).
func HandlePassthrough(args []string) bool {
	handled, _ := HandlePassthroughWithResult(args, true, os.Stdout, os.Stderr)
	return handled
}

// HandlePassthroughWithResult is like HandlePassthrough but returns an error instead of exiting if exit is false.
func HandlePassthroughWithResult(args []string, exit bool, out, errWriter io.Writer) (bool, error) {
	if len(args) < 2 {
		return false, nil
	}

	// Skip global flags to find the git command
	i := 1
	var cwd string
	for i < len(args) {
		arg := args[i]

		// Handle flags with values
		if arg == "--cwd" {
			if i+1 < len(args) {
				cwd = args[i+1]
				i += 2
				continue
			}
		}

		// Handle boolean flags
		if slices.Contains([]string{"--debug", "--interactive", "--no-interactive", "--verify", "--no-verify", "--quiet", "-q"}, arg) {
			i++
			continue
		}

		// If it's a known git command, we've found our passthrough
		if slices.Contains(gitCommandAllowlist, arg) {
			command := arg
			gitArgs := args[i:]

			// Check if the command is modifying and the branch is locked or frozen
			if slices.Contains(modifyingGitCommands, command) {
				runner := git.NewRunner(nil)
				if cwd != "" {
					runner = git.NewRunnerWithPath(cwd, nil)
				}
				if locked, frozen, branch := isCurrentBranchLockedOrFrozen(runner); locked || frozen {
					var state, cmd string
					switch {
					case locked && frozen:
						state = "locked and frozen"
						cmd = "st unlock' and 'st unfreeze"
					case locked:
						state = "locked"
						cmd = "st unlock"
					case frozen:
						state = "frozen"
						cmd = "st unfreeze"
					}
					err := fmt.Errorf("branch %s is %s. Use '%s' to enable modifications", branch, state, cmd)
					if exit {
						_, _ = fmt.Fprintf(errWriter, "Error: %v\n", err)
						os.Exit(1)
					}
					return true, err
				}
			}

			// Execute git command
			var gitCmd *exec.Cmd
			if cwd != "" {
				gitCmd = exec.Command("git", append([]string{"-C", cwd}, gitArgs...)...)
			} else {
				gitCmd = exec.Command("git", gitArgs...)
			}
			gitCmd.Stdin = os.Stdin
			gitCmd.Stdout = out
			gitCmd.Stderr = errWriter

			// Print passthrough message
			_, _ = fmt.Fprintf(errWriter, "\033[90mPassing command through to git...\033[0m\n")
			_, _ = fmt.Fprintf(errWriter, "\033[90mRunning: \"git %s\"\033[0m\n\n", strings.Join(gitArgs, " "))

			err := gitCmd.Run()
			if err != nil {
				if exit {
					var exitError *exec.ExitError
					if errors.As(err, &exitError) {
						os.Exit(exitError.ExitCode())
					}
					os.Exit(1)
				}
				return true, err
			}
			if exit {
				os.Exit(0)
			}
			return true, nil
		}

		// Not a known git command and not a skipped flag, so stop
		return false, nil
	}

	return false, nil
}

// readRefMetadata reads the blob at refName and unmarshals it into dest,
// returning false if the ref, blob, or JSON is missing or invalid.
func readRefMetadata(runner git.Runner, refName string, dest any) bool {
	sha, err := runner.GetRef(refName)
	if err != nil {
		return false
	}
	content, err := runner.CatFile(sha)
	if err != nil {
		return false
	}
	return json.Unmarshal([]byte(content), dest) == nil
}

func isCurrentBranchLockedOrFrozen(runner git.Runner) (bool, bool, string) {
	branch, err := runner.GetCurrentBranch()
	if err != nil {
		return false, false, ""
	}
	branch = strings.TrimSpace(branch)

	var lockMeta struct {
		LockReason engine.LockReason `json:"lockReason"`
	}
	locked := readRefMetadata(runner, "refs/stackit/metadata/"+branch, &lockMeta) && lockMeta.LockReason.IsLocked()

	var frozenMeta struct {
		Frozen bool `json:"frozen"`
	}
	frozen := readRefMetadata(runner, "refs/stackit/local-metadata/"+branch, &frozenMeta) && frozenMeta.Frozen

	return locked, frozen, branch
}

func newAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "add [args...]",
		Short:              "git add passthrough",
		Long:               "arguments [args] (optional) git add arguments",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}

func newCherryPickCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "cherry-pick [args...]",
		Short:              "git cherry-pick passthrough",
		Long:               "arguments [args] (optional) git cherry-pick arguments",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}

func newRebaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "rebase [args...]",
		Short:              "git rebase passthrough",
		Long:               "arguments [args] (optional) git rebase arguments",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}

func newResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "reset [args...]",
		Short:              "git reset passthrough",
		Long:               "arguments [args] (optional) git reset arguments",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}
