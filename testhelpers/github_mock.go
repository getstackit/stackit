package testhelpers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-github/v62/github"
)

// MockGitHubServerConfig configures the behavior of a mock GitHub server
type MockGitHubServerConfig struct {
	// PRs maps branch names to PR data for GetPullRequestByBranch
	PRs map[string]*github.PullRequest
	// CreatedPRs stores PRs that were created (for testing)
	CreatedPRs []*github.PullRequest
	// UpdatedPRs stores PRs that were updated (for testing)
	UpdatedPRs map[int]*github.PullRequest
	// ErrorResponses maps endpoint+method to error responses
	ErrorResponses map[string]error
	// Owner and Repo for the mock server
	Owner string
	Repo  string

	mu sync.Mutex
}

// NewMockGitHubServerConfig creates a new mock server config with defaults
func NewMockGitHubServerConfig() *MockGitHubServerConfig {
	return &MockGitHubServerConfig{
		PRs:            make(map[string]*github.PullRequest),
		CreatedPRs:     make([]*github.PullRequest, 0),
		UpdatedPRs:     make(map[int]*github.PullRequest),
		ErrorResponses: make(map[string]error),
		Owner:          "owner",
		Repo:           "repo",
	}
}

// NewMockGitHubServer creates an httptest server that mocks GitHub API endpoints
func NewMockGitHubServer(t *testing.T, config *MockGitHubServerConfig) *httptest.Server {
	if config == nil {
		config = NewMockGitHubServerConfig()
	}

	mux := http.NewServeMux()

	// Combined handler for /repos/{owner}/{repo}/pulls and /repos/{owner}/{repo}/pulls/{number}
	basePath := "/repos/" + config.Owner + "/" + config.Repo + "/pulls"
	basePathWithSlash := basePath + "/"

	handler := func(w http.ResponseWriter, r *http.Request) {
		config.mu.Lock()
		defer config.mu.Unlock()

		// Use original path for all operations
		originalPath := r.URL.Path
		path := originalPath

		// Handle PR number paths (paths starting with basePathWithSlash like /pulls/1)
		// This must be checked before exact basePath match
		if strings.HasPrefix(path, basePathWithSlash) {
			// Check if this is a reviewers endpoint
			if len(path) >= len("/requested_reviewers") && path[len(path)-len("/requested_reviewers"):] == "/requested_reviewers" {
				if r.Method == "POST" {
					// Request reviewers - just return success
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{"message": "Reviewers requested"})
					return
				}
				if r.Method == "DELETE" {
					// Remove reviewers - just return success
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{"message": "Reviewers removed"})
					return
				}
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}

			// Handle PATCH /repos/{owner}/{repo}/pulls/{number}
			if r.Method == "PATCH" {
				// Extract PR number from original path
				prNumber := extractPRNumber(originalPath)
				if prNumber == 0 {
					// Try extracting from the path variable as fallback
					prNumber = extractPRNumber(path)
					if prNumber == 0 {
						http.Error(w, fmt.Sprintf("Invalid PR number from path: %s (original: %s)", path, originalPath), http.StatusBadRequest)
						return
					}
				}

				// Parse request body - first read the raw bytes
				bodyBytes, _ := io.ReadAll(r.Body)

				// Parse request body using a flexible struct that matches GitHub API format
				// The API sends simple fields like {"base": "branch-name"} not {"base": {"ref": "branch-name"}}
				var update struct {
					Title *string `json:"title,omitempty"`
					Body  *string `json:"body,omitempty"`
					Base  *string `json:"base,omitempty"`
					State *string `json:"state,omitempty"`
					Draft *bool   `json:"draft,omitempty"`
				}
				if err := json.Unmarshal(bodyBytes, &update); err != nil {
					http.Error(w, fmt.Sprintf("Failed to decode request body: %v (path: %s, body: %s)", err, originalPath, string(bodyBytes)), http.StatusBadRequest)
					return
				}

				// Get or create PR
				pr, exists := config.UpdatedPRs[prNumber]
				if !exists {
					// Try to find in created PRs
					for _, createdPR := range config.CreatedPRs {
						if createdPR.Number != nil && *createdPR.Number == prNumber {
							// Make a deep copy to avoid modifying the original
							prCopy := *createdPR
							pr = &prCopy
							// Deep copy Base if it exists
							if createdPR.Base != nil {
								baseCopy := *createdPR.Base
								pr.Base = &baseCopy
							}
							break
						}
					}
					if pr == nil {
						// Create a new PR if it doesn't exist
						pr = &github.PullRequest{
							Number: new(prNumber),
						}
					}
				} else {
					// Make a deep copy to avoid modifying the stored version
					prCopy := *pr
					pr = &prCopy
					// Deep copy Base if it exists
					if pr.Base != nil {
						baseCopy := *pr.Base
						pr.Base = &baseCopy
					}
				}

				// Ensure Base is initialized if it was nil
				if pr.Base == nil {
					pr.Base = &github.PullRequestBranch{}
				}

				// Update fields - preserve existing values if not provided
				if update.Title != nil {
					pr.Title = update.Title
				}
				if update.Body != nil {
					pr.Body = update.Body
				}
				if update.Base != nil {
					// Update base branch ref - the API sends base as a string, not an object
					if pr.Base == nil {
						pr.Base = &github.PullRequestBranch{}
					}
					pr.Base.Ref = update.Base
				}
				if update.Draft != nil {
					pr.Draft = update.Draft
				}

				config.UpdatedPRs[prNumber] = pr

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(pr)
				return
			}

			// Handle GET /repos/{owner}/{repo}/pulls/{number}
			if r.Method == "GET" {
				prNumber := extractPRNumber(originalPath)
				if prNumber == 0 {
					http.Error(w, "Invalid PR number", http.StatusBadRequest)
					return
				}

				// Try to find PR in UpdatedPRs first (most recent), then CreatedPRs
				var pr *github.PullRequest
				pr, exists := config.UpdatedPRs[prNumber]
				if !exists {
					// Try to find in created PRs
					for _, createdPR := range config.CreatedPRs {
						if createdPR.Number != nil && *createdPR.Number == prNumber {
							// Make a deep copy to avoid modifying the original
							prCopy := *createdPR
							pr = &prCopy
							// Deep copy Base and Head if they exist
							if createdPR.Base != nil {
								baseCopy := *createdPR.Base
								pr.Base = &baseCopy
							}
							if createdPR.Head != nil {
								headCopy := *createdPR.Head
								pr.Head = &headCopy
							}
							break
						}
					}
				} else {
					// Make a deep copy to avoid modifying the stored version
					prCopy := *pr
					pr = &prCopy
					// Deep copy Base and Head if they exist
					if pr.Base != nil {
						baseCopy := *pr.Base
						pr.Base = &baseCopy
					}
					if pr.Head != nil {
						headCopy := *pr.Head
						pr.Head = &headCopy
					}
				}

				if pr == nil {
					http.Error(w, "PR not found", http.StatusNotFound)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(pr)
				return
			}
		}

		// Handle exact basePath matches (list/create)
		if path == basePath {
			// Handle POST /repos/{owner}/{repo}/pulls (create PR)
			if r.Method == "POST" {
				// Parse request body
				var newPR github.NewPullRequest
				if err := json.NewDecoder(r.Body).Decode(&newPR); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}

				// Create a mock PR response
				prNumber := len(config.CreatedPRs) + 1
				pr := &github.PullRequest{
					Number:  new(prNumber),
					Title:   newPR.Title,
					Body:    newPR.Body,
					Head:    &github.PullRequestBranch{Ref: newPR.Head},
					Base:    &github.PullRequestBranch{Ref: newPR.Base},
					Draft:   newPR.Draft,
					HTMLURL: new("https://github.com/" + config.Owner + "/" + config.Repo + "/pull/" + fmt.Sprintf("%d", prNumber)),
				}

				config.CreatedPRs = append(config.CreatedPRs, pr)
				config.PRs[*newPR.Head] = pr

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(pr)
				return
			}

			// Handle GET /repos/{owner}/{repo}/pulls (list PRs)
			if r.Method == "GET" {
				// Parse query parameters for head filter
				head := r.URL.Query().Get("head")
				if head != "" {
					// Extract branch name from "owner:branch" format
					branchName := head
					if idx := len(config.Owner) + 1; len(head) > idx && head[:idx] == config.Owner+":" {
						branchName = head[idx:]
					}

					pr, exists := config.PRs[branchName]
					if !exists {
						// Return empty list
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode([]*github.PullRequest{})
						return
					}

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]*github.PullRequest{pr})
					return
				}
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
		}

		// If we get here, the path didn't match any handler
		// Check if it's a PR number path that we should handle
		if strings.HasPrefix(path, basePath) {
			http.Error(w, fmt.Sprintf("Unhandled PR path: %s (method: %s, basePath: %s, basePathWithSlash: %s)", path, r.Method, basePath, basePathWithSlash), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Unhandled path: %s (method: %s)", path, r.Method), http.StatusNotFound)
	}

	// Register a single catch-all handler for all paths starting with basePath
	// We'll handle exact vs prefix matching inside the handler
	// Register basePathWithSlash first (prefix match) so it takes precedence
	mux.HandleFunc(basePathWithSlash, handler)
	// Then register basePath for exact matches
	mux.HandleFunc(basePath, handler)

	server := httptest.NewServer(mux)
	t.Cleanup(func() { server.Close() })
	return server
}

// NewMockGitHubClient creates a GitHub client configured to use a mock server
func NewMockGitHubClient(t *testing.T, config *MockGitHubServerConfig) (*github.Client, string, string) {
	server := NewMockGitHubServer(t, config)
	client := github.NewClient(nil)
	baseURL, _ := url.Parse(server.URL + "/")
	client.BaseURL = baseURL
	client.UploadURL = baseURL

	owner := config.Owner
	repo := config.Repo
	if owner == "" {
		owner = "owner"
	}
	if repo == "" {
		repo = "repo"
	}

	return client, owner, repo
}

// extractPRNumber extracts the PR number from a URL path like "/repos/owner/repo/pulls/123"
func extractPRNumber(path string) int {
	// Find the last segment after "/pulls/"
	pullsIdx := -1
	for i := 0; i < len(path)-6; i++ {
		if path[i:i+6] == "/pulls" {
			pullsIdx = i + 6
			break
		}
	}
	if pullsIdx == -1 || pullsIdx >= len(path) {
		return 0
	}

	// Skip the "/" after "/pulls"
	if pullsIdx < len(path) && path[pullsIdx] == '/' {
		pullsIdx++
	}

	// Extract the number
	var number int
	for i := pullsIdx; i < len(path); i++ {
		char := path[i]
		switch {
		case char >= '0' && char <= '9':
			number = number*10 + int(char-'0')
		case char == '/' || char == '?' || char == '#':
			return number
		default:
			return 0 // Invalid character
		}
	}
	return number
}
