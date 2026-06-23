// Package demo provides a simulated engine for testing TUI interactions
// without requiring a real git repository.
package demo

import "github.com/getstackit/stackit/internal/engine"

// Demo data constants for branch names and statuses used in fixture data.
const (
	demoTrunkBranch   = "main"
	demoAuthBase      = "feature/auth-base"
	demoScopeAuth     = "AUTH"
	demoPRStateOpen   = "OPEN"
	demoChecksPassing = "PASSING"
)

// Branch represents a simulated branch with PR info
type Branch struct {
	Name     string
	Parent   string
	SHA      string
	PRNumber int
	PRState  string // OPEN, MERGED, CLOSED
	PRTitle  string
	IsDraft  bool
	Checks   string // PASSING, FAILING, PENDING, NONE
	Commits  int
	Added    int
	Deleted  int
	Scope    string
}

// Demo stack structure - branching stack with no upstack from current branch
//
// main
// ├── feature/auth-base (#101) [AUTH]
// │   ├── feature/auth-validation (#102) [AUTH]
// │   │   └── feature/auth-login (#103) [AUTH] ← current
// │   └── feature/auth-oauth (#105) [AUTH]
// │       └── feature/auth-oauth-google (#106) [AUTH]
// └── feature/api-refactor (#107) [API]
//
//	└── feature/api-v2 (#108) [API]

var demoBranches = []Branch{
	{
		Name:     demoAuthBase,
		Parent:   demoTrunkBranch,
		SHA:      "sha-auth-base",
		PRNumber: 101,
		PRState:  demoPRStateOpen,
		PRTitle:  "Add authentication base module",
		IsDraft:  false,
		Checks:   demoChecksPassing,
		Commits:  3,
		Added:    150,
		Deleted:  10,
		Scope:    demoScopeAuth,
	},
	{
		Name:     "feature/auth-validation",
		Parent:   demoAuthBase,
		SHA:      "sha-auth-validation",
		PRNumber: 102,
		PRState:  demoPRStateOpen,
		PRTitle:  "Add input validation for auth",
		IsDraft:  false,
		Checks:   demoChecksPassing,
		Commits:  2,
		Added:    50,
		Deleted:  5,
		Scope:    demoScopeAuth,
	},
	{
		Name:     "feature/auth-login",
		Parent:   "feature/auth-validation",
		SHA:      "sha-auth-login",
		PRNumber: 103,
		PRState:  demoPRStateOpen,
		PRTitle:  "Implement login flow",
		IsDraft:  false,
		Checks:   demoChecksPassing,
		Commits:  5,
		Added:    200,
		Deleted:  20,
		Scope:    demoScopeAuth,
	},
	{
		Name:     "feature/auth-oauth",
		Parent:   demoAuthBase,
		SHA:      "sha-auth-oauth",
		PRNumber: 105,
		PRState:  "MERGED",
		PRTitle:  "Add OAuth support",
		IsDraft:  false,
		Checks:   demoChecksPassing,
		Commits:  1,
		Added:    30,
		Deleted:  2,
		Scope:    demoScopeAuth,
	},
	{
		Name:     "feature/auth-oauth-google",
		Parent:   "feature/auth-oauth",
		SHA:      "sha-auth-oauth-google",
		PRNumber: 106,
		PRState:  demoPRStateOpen,
		PRTitle:  "Add Google OAuth provider",
		IsDraft:  true,
		Checks:   "PENDING",
		Commits:  2,
		Added:    80,
		Deleted:  5,
		Scope:    demoScopeAuth,
	},
	{
		Name:     "feature/api-refactor",
		Parent:   demoTrunkBranch,
		SHA:      "sha-api-refactor",
		PRNumber: 107,
		PRState:  demoPRStateOpen,
		PRTitle:  "Refactor API layer",
		IsDraft:  false,
		Checks:   "FAILING",
		Commits:  8,
		Added:    500,
		Deleted:  150,
		Scope:    "API",
	},
	{
		Name:     "feature/api-v2",
		Parent:   "feature/api-refactor",
		SHA:      "sha-api-v2",
		PRNumber: 108,
		PRState:  "CLOSED",
		PRTitle:  "Implement API v2 endpoints",
		IsDraft:  false,
		Checks:   "NONE",
		Commits:  0,
		Added:    0,
		Deleted:  0,
		Scope:    "API",
	},
}

// Current branch is a leaf node (no children) to avoid warnings
var demoCurrentBranch = "feature/auth-login"
var demoTrunk = demoTrunkBranch

// GetDemoBranches returns the demo branch data
func GetDemoBranches() []Branch {
	return demoBranches
}

// GetDemoCurrentBranch returns the simulated current branch
func GetDemoCurrentBranch() string {
	return demoCurrentBranch
}

// GetDemoTrunk returns the simulated trunk branch
func GetDemoTrunk() string {
	return demoTrunk
}

// GetDemoPrInfo returns simulated PR info for a branch
func GetDemoPrInfo(branchName string) *engine.PrInfo {
	for _, b := range demoBranches {
		if b.Name == branchName {
			num := b.PRNumber
			return engine.NewPrInfo(
				&num,
				b.PRTitle,
				"Demo PR body for "+branchName,
				b.PRState,
				b.Parent,
				"https://github.com/example/repo/pull/"+string(rune('0'+num%10)),
				b.IsDraft,
			)
		}
	}
	return nil
}

// GetDemoChecksStatus returns the simulated checks status for a branch
func GetDemoChecksStatus(branchName string) string {
	for _, b := range demoBranches {
		if b.Name == branchName {
			return b.Checks
		}
	}
	return "NONE"
}
