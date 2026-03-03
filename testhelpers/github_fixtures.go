package testhelpers

import (
	"github.com/google/go-github/v62/github"
)

// prStateClosed is the GitHub API state for closed PRs
const prStateClosed = "closed"

// SamplePRData provides common PR data for testing
type SamplePRData struct {
	Number        int
	Title         string
	Body          string
	Head          string
	Base          string
	HTMLURL       string
	Draft         bool
	State         string
	Reviewers     []string
	TeamReviewers []string
}

// NewSamplePullRequest creates a github.PullRequest from sample data
func NewSamplePullRequest(data SamplePRData) *github.PullRequest {
	pr := &github.PullRequest{
		Number:  new(data.Number),
		Title:   new(data.Title),
		Body:    new(data.Body),
		Head:    &github.PullRequestBranch{Ref: new(data.Head)},
		Base:    &github.PullRequestBranch{Ref: new(data.Base)},
		HTMLURL: new(data.HTMLURL),
		Draft:   new(data.Draft),
		State:   new(data.State),
	}

	if len(data.Reviewers) > 0 {
		pr.RequestedReviewers = make([]*github.User, len(data.Reviewers))
		for i, reviewer := range data.Reviewers {
			pr.RequestedReviewers[i] = &github.User{
				Login: new(reviewer),
			}
		}
	}

	if len(data.TeamReviewers) > 0 {
		pr.RequestedTeams = make([]*github.Team, len(data.TeamReviewers))
		for i, team := range data.TeamReviewers {
			pr.RequestedTeams[i] = &github.Team{
				Slug: new(team),
			}
		}
	}

	return pr
}

// DefaultPRData returns a default PR data structure for testing
func DefaultPRData() SamplePRData {
	return SamplePRData{
		Number:  123,
		Title:   "Test Pull Request",
		Body:    "This is a test pull request",
		Head:    "feature-branch",
		Base:    "main",
		HTMLURL: "https://github.com/owner/repo/pull/123",
		Draft:   false,
		State:   "open",
	}
}

// DraftPRData returns PR data for a draft PR
func DraftPRData() SamplePRData {
	data := DefaultPRData()
	data.Draft = true
	data.Title = "Draft: Test Pull Request"
	return data
}

// PRWithReviewersData returns PR data with reviewers
func PRWithReviewersData(reviewers []string, teamReviewers []string) SamplePRData {
	data := DefaultPRData()
	data.Reviewers = reviewers
	data.TeamReviewers = teamReviewers
	return data
}

// MergedPRData returns PR data for a merged PR
func MergedPRData() SamplePRData {
	data := DefaultPRData()
	data.State = prStateClosed
	return data
}

// ClosedPRData returns PR data for a closed PR
func ClosedPRData() SamplePRData {
	data := DefaultPRData()
	data.State = prStateClosed
	return data
}
