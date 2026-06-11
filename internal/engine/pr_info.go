package engine

import "encoding/json"

// PrInfo represents PR information for a branch
// PrInfo is immutable - use With* methods to create modified copies
type PrInfo struct {
	number      *int
	title       string
	body        string
	isDraft     bool
	state       string     // MERGED, CLOSED, OPEN
	base        string     // Base branch name
	baseSHA     string     // Tip SHA of base branch at last submit (used to detect stale base.sha on GitHub)
	url         string     // PR URL
	lockReason  LockReason // Why the PR is locked (empty if not locked)
	mergeBranch string     // Name of the merge branch this PR is part of
}

// NewPrInfo creates a new PrInfo instance
func NewPrInfo(number *int, title, body, state, base, url string, isDraft bool) *PrInfo {
	return &PrInfo{
		number:  number,
		title:   title,
		body:    body,
		isDraft: isDraft,
		state:   state,
		base:    base,
		url:     url,
	}
}

// Number returns the PR number
func (p *PrInfo) Number() *int {
	return p.number
}

// Title returns the PR title
func (p *PrInfo) Title() string {
	return p.title
}

// Body returns the PR body
func (p *PrInfo) Body() string {
	return p.body
}

// IsDraft returns whether the PR is a draft
func (p *PrInfo) IsDraft() bool {
	return p.isDraft
}

// State returns the PR state (MERGED, CLOSED, OPEN)
func (p *PrInfo) State() string {
	return p.state
}

// Base returns the base branch name
func (p *PrInfo) Base() string {
	return p.base
}

// BaseSHA returns the tip SHA of the base branch at the time of the last submit.
// Used to detect when a parent branch has been force-pushed, making GitHub's
// stored base.sha stale.
func (p *PrInfo) BaseSHA() string {
	return p.baseSHA
}

// URL returns the PR URL
func (p *PrInfo) URL() string {
	return p.url
}

// IsLocked returns whether the PR footer shows it as locked
func (p *PrInfo) IsLocked() bool {
	return p.lockReason.IsLocked()
}

// LockReason returns the reason why the PR is locked
func (p *PrInfo) LockReason() LockReason {
	return p.lockReason
}

// MergeBranch returns the name of the merge branch this PR is part of
func (p *PrInfo) MergeBranch() string {
	return p.mergeBranch
}

// MarshalJSON implements json.Marshaler for PrInfo
func (p *PrInfo) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Number      *int   `json:"number,omitempty"`
		Base        string `json:"base,omitempty"`
		URL         string `json:"url,omitempty"`
		Title       string `json:"title,omitempty"`
		Body        string `json:"body,omitempty"`
		State       string `json:"state,omitempty"`
		IsDraft     bool   `json:"is_draft"`
		LockReason  string `json:"lock_reason,omitempty"`
		MergeBranch string `json:"merge_branch,omitempty"`
	}
	return json.Marshal(&Alias{
		Number:      p.number,
		Base:        p.base,
		URL:         p.url,
		Title:       p.title,
		Body:        p.body,
		State:       p.state,
		IsDraft:     p.isDraft,
		LockReason:  string(p.lockReason),
		MergeBranch: p.mergeBranch,
	})
}

// WithNumber returns a new PrInfo with the number field updated
func (p *PrInfo) WithNumber(number *int) *PrInfo {
	return &PrInfo{
		number:      number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithTitle returns a new PrInfo with the title field updated
func (p *PrInfo) WithTitle(title string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithBody returns a new PrInfo with the body field updated
func (p *PrInfo) WithBody(body string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithTitleAndBody returns a new PrInfo with both title and body fields updated
// This is more efficient than chaining WithTitle().WithBody() as it only creates one copy
func (p *PrInfo) WithTitleAndBody(title, body string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       title,
		body:        body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithIsDraft returns a new PrInfo with the isDraft field updated
func (p *PrInfo) WithIsDraft(isDraft bool) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithState returns a new PrInfo with the state field updated
func (p *PrInfo) WithState(state string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithBase returns a new PrInfo with the base field updated
func (p *PrInfo) WithBase(base string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithBaseSHA returns a new PrInfo with the baseSHA field updated
func (p *PrInfo) WithBaseSHA(sha string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     sha,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithURL returns a new PrInfo with the url field updated
func (p *PrInfo) WithURL(url string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         url,
		lockReason:  p.lockReason,
		mergeBranch: p.mergeBranch,
	}
}

// WithLockReason returns a new PrInfo with the lockReason field updated
func (p *PrInfo) WithLockReason(reason LockReason) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  reason,
		mergeBranch: p.mergeBranch,
	}
}

// WithMergeBranch returns a new PrInfo with the mergeBranch field updated
func (p *PrInfo) WithMergeBranch(branch string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		baseSHA:     p.baseSHA,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: branch,
	}
}
