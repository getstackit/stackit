// Package submit provides functionality for submitting stacked branches as pull requests.
package submit

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/pr"
	"github.com/getstackit/stackit/internal/tui"
)

// GetPRTitle gets the PR title, prompting if needed
func GetPRTitle(branch engine.Branch, editInline bool, existingTitle string, scope engine.Scope) (string, error) {
	title := pr.GenerateTitle(branch, existingTitle, scope)

	if !editInline {
		return title, nil
	}

	result, err := tui.PromptTextInput("Title:", title)
	if err != nil {
		return "", fmt.Errorf("failed to get PR title: %w", err)
	}

	return result, nil
}

// GetPRBody gets the PR body, prompting if needed
func GetPRBody(branch engine.Branch, editInline bool, existingBody string) (string, error) {
	body := pr.GenerateBody(branch, existingBody)

	if !editInline {
		return body, nil
	}

	return tui.OpenEditor(body, "stackit-pr-description-*.md")
}

// GetReviewers gets reviewers from flag or prompts user
func GetReviewers(reviewersFlag string) ([]string, []string, error) {
	if reviewersFlag == "" {
		return nil, nil, nil
	}

	reviewers, teamReviewers := github.ParseReviewers(reviewersFlag)
	return reviewers, teamReviewers, nil
}

// GetReviewersWithPrompt gets reviewers, prompting if flag is empty
func GetReviewersWithPrompt(reviewersFlag string) ([]string, []string, error) {
	if reviewersFlag == "" {
		result, err := tui.PromptTextInput("Reviewers (comma-separated GitHub usernames):", "")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get reviewers: %w", err)
		}

		reviewersFlag = result
	}

	reviewers, teamReviewers := github.ParseReviewers(reviewersFlag)
	return reviewers, teamReviewers, nil
}

// PreparePRMetadata prepares PR metadata for a branch
func PreparePRMetadata(branch engine.Branch, opts MetadataOptions, ctx *app.Context) (*PRMetadata, error) {
	prInfo, _ := branch.GetPrInfo()
	nav := ctx.Navigator()

	metadata := &PRMetadata{
		Title:   getStringValue(prInfo, "Title"),
		Body:    getStringValue(prInfo, "Body"),
		IsDraft: false,
	}

	shouldEditTitle := opts.EditTitle || (opts.Edit && !opts.NoEditTitle)
	shouldEditBody := opts.EditDescription || (opts.Edit && !opts.NoEditDescription)

	// If PR exists and local metadata is missing title or body, fetch from GitHub
	if prInfo != nil && prInfo.Number() != nil && (metadata.Title == "" || metadata.Body == "") && ctx.GitHub() != nil {
		repoOwner, repoName := ctx.GitHub().GetOwnerRepo()
		if repoOwner != "" && repoName != "" {
			currentPR, err := ctx.GitHub().GetPullRequest(ctx.Context, repoOwner, repoName, *prInfo.Number())
			if err == nil && currentPR != nil {
				if metadata.Title == "" {
					metadata.Title = currentPR.Title
				}
				if metadata.Body == "" {
					metadata.Body = currentPR.Body
				}
			}
		}
	}

	scope := nav.GetScope(branch)

	// Handle Title
	if shouldEditTitle || metadata.Title == "" {
		title, err := GetPRTitle(branch, shouldEditTitle, metadata.Title, scope)
		if err != nil {
			return nil, err
		}
		metadata.Title = title
	}

	// Handle Body
	if shouldEditBody || metadata.Body == "" {
		body, err := GetPRBody(branch, shouldEditBody, metadata.Body)
		if err != nil {
			return nil, err
		}
		metadata.Body = body
	}

	switch {
	case opts.Draft:
		metadata.IsDraft = true
	case opts.Publish:
		metadata.IsDraft = false
	case prInfo == nil:
		// For new PRs, use config draft setting or inherit from parent PR
		metadata.IsDraft = opts.ConfigDraft || isParentPRDraft(branch)
	default:
		metadata.IsDraft = prInfo.IsDraft()
	}

	if opts.ReviewersPrompt {
		reviewers, teamReviewers, err := GetReviewersWithPrompt(opts.Reviewers)
		if err != nil {
			return nil, err
		}
		metadata.Reviewers = reviewers
		metadata.TeamReviewers = teamReviewers
	} else if opts.Reviewers != "" {
		reviewers, teamReviewers, err := GetReviewers(opts.Reviewers)
		if err != nil {
			return nil, err
		}
		metadata.Reviewers = reviewers
		metadata.TeamReviewers = teamReviewers
	}

	// Merge config reviewers if no reviewers from flag
	if len(metadata.Reviewers) == 0 && len(metadata.TeamReviewers) == 0 && len(opts.ConfigReviewers) > 0 {
		reviewers, teamReviewers := github.ParseReviewers(strings.Join(opts.ConfigReviewers, ","))
		metadata.Reviewers = reviewers
		metadata.TeamReviewers = teamReviewers
	}

	// Set labels from config
	metadata.Labels = opts.ConfigLabels

	// Set assignees from config
	metadata.Assignees = opts.ConfigAssignees

	// The prepared title/body/draft are persisted by the caller in one batch
	// after planning (see prepareBranchesForSubmit), so a later submit failure
	// can still recover them. PreparePRMetadata itself stays side-effect free.
	return metadata, nil
}

// pendingPrInfo builds the PR info to persist for a branch from its prepared
// metadata, matching what was previously written inline by PreparePRMetadata.
func pendingPrInfo(branch engine.Branch, metadata *PRMetadata) *engine.PrInfo {
	return engine.NewPrInfo(
		nil,
		metadata.Title,
		metadata.Body,
		"",
		"",
		"",
		metadata.IsDraft,
	).WithLockReason(branch.GetLockReason())
}

// MetadataOptions contains options for PR metadata collection
type MetadataOptions struct {
	Edit              bool
	EditTitle         bool
	EditDescription   bool
	NoEdit            bool
	NoEditTitle       bool
	NoEditDescription bool
	Draft             bool
	Publish           bool
	Reviewers         string
	ReviewersPrompt   bool
	// Config-driven options
	ConfigDraft     bool     // Default draft mode from config (used for new PRs)
	ConfigReviewers []string // Default reviewers from config
	ConfigLabels    []string // Default labels from config
	ConfigAssignees []string // Default assignees from config
}

// PRMetadata contains PR metadata
type PRMetadata struct {
	Title         string
	Body          string
	IsDraft       bool
	Reviewers     []string
	TeamReviewers []string
	Labels        []string // Labels to apply to the PR
	Assignees     []string // Assignees to apply to the PR
}

// isParentPRDraft returns true if the branch's parent has a draft PR.
// Returns false for trunk-parented branches, branches with no parent PR, or on errors.
func isParentPRDraft(branch engine.Branch) bool {
	parent := branch.GetParent()
	if parent == nil {
		return false
	}
	prInfo, err := parent.GetPrInfo()
	if err != nil || prInfo == nil {
		return false
	}
	return prInfo.IsDraft()
}

// Helper to get string value from prInfo
func getStringValue(prInfo *engine.PrInfo, field string) string {
	if prInfo == nil {
		return ""
	}
	switch field {
	case "Title":
		return prInfo.Title()
	case "Body":
		return prInfo.Body()
	case "Base":
		return prInfo.Base()
	case "State":
		return prInfo.State()
	default:
		return ""
	}
}
