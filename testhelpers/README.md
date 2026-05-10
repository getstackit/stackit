# Test Helpers

This package provides testing utilities for the Stackit CLI Go port, ported from the TypeScript test infrastructure.

## Overview

The testhelpers package provides:
- **Scene System**: Reusable test scenarios with temporary Git repositories
- **Git Repository Helpers**: Utilities for manipulating Git repos in tests
- **Assertions**: Custom assertion helpers for branches and commits

## Scene System

The scene system creates isolated test environments with temporary Git repositories.

### Basic Usage

```go
import (
    "testing"
    "github.com/getstackit/stackit/testhelpers"
)

func TestMyFeature(t *testing.T) {
    // Create a basic scene with a single commit
    scene := testhelpers.NewScene(t, testhelpers.BasicSceneSetup)
    
    // Use scene.Repo to interact with the Git repository
    err := scene.Repo.RunCliCommand([]string{"branch", "create", "feature"})
    require.NoError(t, err)
    
    // Assertions
    testhelpers.ExpectBranchesString(t, scene.Repo, "feature, main")
}
```

### Custom Setup

```go
func TestCustomScenario(t *testing.T) {
    customSetup := func(scene *testhelpers.Scene) error {
        // Create multiple commits
        scene.Repo.CreateChangeAndCommit("commit 1", "1")
        scene.Repo.CreateChangeAndCommit("commit 2", "2")
        
        // Create branches
        scene.Repo.CreateAndCheckoutBranch("parent")
        scene.Repo.CheckoutBranch("main")
        
        return nil
    }
    
    scene := testhelpers.NewScene(t, customSetup)
    // Test implementation...
}
```

## Git Repository Helpers

The `GitRepo` type provides methods for Git operations:

### Basic Operations

```go
repo := scene.Repo

// Create commits
repo.CreateChangeAndCommit("content", "prefix")

// Branch operations
repo.CreateAndCheckoutBranch("feature")
repo.CheckoutBranch("main")
repo.DeleteBranch("old-branch")

// Get information
branch, _ := repo.CurrentBranchName()
ref, _ := repo.GetRef("HEAD")
messages, _ := repo.ListCurrentBranchCommitMessages()
```

### Running CLI Commands

```go
// Run a CLI command
err := repo.RunCliCommand([]string{"branch", "create", "feature"})

// Run a CLI command and get output
output, err := repo.RunCliCommandAndGetOutput([]string{"branch", "list"})
```

## Assertions

### Branch Assertions

```go
// Assert branches as a slice
testhelpers.ExpectBranches(t, repo, []string{"main", "feature"})

// Assert branches as a comma-separated string (matches TypeScript API)
testhelpers.ExpectBranchesString(t, repo, "feature, main")
```

### Commit Assertions

```go
// Assert commits on a specific branch
testhelpers.ExpectCommits(t, repo, "main", []string{"Initial commit", "Second commit"})

// Assert commits as a comma-separated string (matches TypeScript API)
testhelpers.ExpectCommitsString(t, repo, "Initial commit, Second commit")
```

## Scene Types

The following scene setups are available:

- **`BasicSceneSetup`**: Creates a scene with a single commit on main branch

Additional scene types can be added as needed, following the pattern:

```go
func MySceneSetup(scene *testhelpers.Scene) error {
    // Setup logic here
    return nil
}
```

## Cleanup

Scenes automatically clean up temporary directories using `t.Cleanup()`. If the `DEBUG` environment variable is set, temporary directories are preserved for inspection.

## Migration Notes

This package is a direct port of the TypeScript test infrastructure:
- `AbstractScene` → `Scene`
- `GitRepo` class → `GitRepo` struct
- `expectBranches` → `ExpectBranches` / `ExpectBranchesString`
- `expectCommits` → `ExpectCommits` / `ExpectCommitsString`

The API is designed to be as similar as possible to the TypeScript version while following Go idioms.

## GitHub API Mocking

The testhelpers package provides utilities for testing commands that interact with the GitHub API without making real API calls.

### Mock GitHub Server

The `NewMockGitHubServer` and `NewMockGitHubClient` functions create a mock GitHub API server using Go's `httptest` package. This allows you to test GitHub API interactions locally without requiring authentication or hitting rate limits.

### Basic Usage

```go
import (
    "testing"
    "github.com/getstackit/stackit/testhelpers"
    "github.com/getstackit/stackit/internal/actions"
    "github.com/getstackit/stackit/internal/engine"
    "github.com/getstackit/stackit/internal/output"
)

func TestSubmitWithMockedGitHub(t *testing.T) {
    scene := testhelpers.NewScene(t, nil)
    
    // Set up your test scenario...
    scene.Repo.CreateChangeAndCommit("initial", "init")
    scene.Repo.RunGitCommand("config", "--local", "stackit.trunk", "main")
    scene.Repo.CreateAndCheckoutBranch("feature")
    scene.Repo.CreateChangeAndCommit("feature change", "feat")
    
    // Create engine and track branch
    eng, err := engine.NewEngine(engine.Options{
        RepoRoot:          scene.Dir,
        Trunk:             "main",
        MaxUndoStackDepth: engine.DefaultMaxUndoStackDepth,
    })
    require.NoError(t, err)
    err = eng.TrackBranch("feature", "main")
    require.NoError(t, err)
    
    // Create mocked GitHub client
    config := testhelpers.NewMockGitHubServerConfig()
    githubClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
    
    // Use mocked client in submit options
    opts := actions.SubmitOptions{
        Engine:       eng,
        Splog:        output.NewSplog(),
        DryRun:       false,
        NoEdit:       true,
        GitHubClient: githubClient,  // Inject mocked client
        GitHubOwner:  owner,
        GitHubRepo:   repo,
    }
    
    // Submit will use mocked client (push is automatically skipped)
    err = actions.SubmitAction(opts)
    require.NoError(t, err)
    
    // Verify PR was created in mock
    require.Greater(t, len(config.CreatedPRs), 0)
}
```

### Mock Server Configuration

The `MockGitHubServerConfig` allows you to customize the mock server's behavior:

```go
config := testhelpers.NewMockGitHubServerConfig()
config.Owner = "myorg"
config.Repo = "myrepo"

// Pre-populate existing PRs
prData := testhelpers.DefaultPRData()
prData.Head = "feature-branch"
prData.Number = 123
pr := testhelpers.NewSamplePullRequest(prData)
config.PRs["feature-branch"] = pr
config.UpdatedPRs[123] = pr

githubClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
```

### Testing PR Operations

You can test individual PR operations using the mocked client:

```go
func TestCreatePullRequest(t *testing.T) {
    config := testhelpers.NewMockGitHubServerConfig()
    client, owner, repo := testhelpers.NewMockGitHubClient(t, config)
    
    opts := git.CreatePROptions{
        Title: "Test PR",
        Head:  "feature",
        Base:  "main",
    }
    
    pr, err := git.CreatePullRequest(context.Background(), client, owner, repo, opts)
    require.NoError(t, err)
    require.NotNil(t, pr)
    require.Equal(t, 1, *pr.Number)
    
    // Verify in mock
    require.Greater(t, len(config.CreatedPRs), 0)
}
```

### Test Fixtures

The `github_fixtures.go` file provides helper functions for creating test data:

- `DefaultPRData()` - Creates a standard PR data structure
- `DraftPRData()` - Creates a draft PR
- `PRWithReviewersData()` - Creates a PR with reviewers
- `NewSamplePullRequest()` - Converts PR data to a `github.PullRequest`

### How It Works

1. **Mock Server**: `NewMockGitHubServer` creates an `httptest.Server` that handles GitHub API endpoints
2. **Client Configuration**: `NewMockGitHubClient` configures a `github.Client` to use the mock server's URL
3. **Automatic Cleanup**: The mock server is automatically closed when the test completes via `t.Cleanup()`
4. **Push Skipping**: When a mocked GitHub client is provided to `SubmitAction`, git push operations are automatically skipped

### Supported Endpoints

The mock server currently supports:
- `POST /repos/{owner}/{repo}/pulls` - Create pull request
- `PATCH /repos/{owner}/{repo}/pulls/{number}` - Update pull request
- `GET /repos/{owner}/{repo}/pulls/{number}` - Get pull request
- `GET /repos/{owner}/{repo}/pulls` - List pull requests (with head filter)
- `POST /repos/{owner}/{repo}/pulls/{number}/requested_reviewers` - Request reviewers
- `DELETE /repos/{owner}/{repo}/pulls/{number}/requested_reviewers` - Remove reviewers

### Limitations

- The mock server is simplified and may not handle all edge cases
- Some GitHub API features may not be fully implemented
- For complex scenarios, consider using integration tests with a real GitHub repository
