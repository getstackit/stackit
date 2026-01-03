// Package submit provides functionality for submitting stacked branches as pull requests.
package submit

import (
	"fmt"
	"regexp"
	"strings"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
)

var scopeRegex = regexp.MustCompile(`^\[[^\]]+\]\s*`)

// GetPRTitle gets the PR title, prompting if needed
func GetPRTitle(branch engine.Branch, editInline bool, existingTitle string, scope string) (string, error) {
	branchName := branch.GetName()
	title := existingTitle
	if title == "" {
		commits, err := branch.GetAllCommits(engine.CommitFormatSubject)
		if err != nil || len(commits) == 0 {
			title = branchName
		} else {
			// GetAllCommits returns newest to oldest, so oldest is last
			title = commits[len(commits)-1]
		}
	}

	if scope != "" {
		if scopeRegex.MatchString(title) {
			if !strings.HasPrefix(strings.ToUpper(title), "["+strings.ToUpper(scope)+"]") {
				title = scopeRegex.ReplaceAllString(title, "["+scope+"] ")
			}
		} else {
			title = fmt.Sprintf("[%s] %s", scope, title)
		}
	}

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
	body := existingBody
	if body == "" {
		messages, err := branch.GetAllCommits(engine.CommitFormatMessage)
		if err == nil && len(messages) > 0 {
			if len(messages) == 1 {
				// Use body (skip first line which is subject)
				lines := strings.Split(messages[0], "\n")
				if len(lines) > 1 {
					body = strings.Join(lines[1:], "\n")
				}
			} else {
				// Format as a bulleted list of subjects in chronological order
				var sb strings.Builder
				// GetAllCommits returns newest to oldest
				for i := len(messages) - 1; i >= 0; i-- {
					msg := messages[i]
					subject := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0])
					if subject != "" {
						sb.WriteString("- " + subject + "\n")
					}
				}
				body = strings.TrimSpace(sb.String())
			}
		}
	}

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
	pr := ctx.PR()

	metadata := &PRMetadata{
		Title:   getStringValue(prInfo, "Title"),
		Body:    getStringValue(prInfo, "Body"),
		IsDraft: false,
	}

	shouldEditTitle := opts.EditTitle || (opts.Edit && !opts.NoEditTitle)
	shouldEditBody := opts.EditDescription || (opts.Edit && !opts.NoEditDescription)

	// If PR exists and local metadata is missing title or body, fetch from GitHub
	if prInfo != nil && prInfo.Number() != nil && (metadata.Title == "" || metadata.Body == "") && ctx.GitHubClient != nil {
		repoOwner, repoName := ctx.GitHubClient.GetOwnerRepo()
		if repoOwner != "" && repoName != "" {
			currentPR, err := ctx.GitHubClient.GetPullRequest(ctx.Context, repoOwner, repoName, *prInfo.Number())
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
		title, err := GetPRTitle(branch, shouldEditTitle, metadata.Title, scope.String())
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
		metadata.IsDraft = false
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

	// Save metadata to engine in case command fails
	if err := pr.UpsertPrInfo(branch, engine.NewPrInfo(
		nil,
		metadata.Title,
		metadata.Body,
		"",
		"",
		"",
		metadata.IsDraft,
	).WithLockReason(branch.GetLockReason())); err != nil {
		ctx.Splog.Debug("Failed to save PR metadata: %v", err)
	}

	return metadata, nil
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
}

// PRMetadata contains PR metadata
type PRMetadata struct {
	Title         string
	Body          string
	IsDraft       bool
	Reviewers     []string
	TeamReviewers []string
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
