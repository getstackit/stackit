package testhelpers

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// NewTestPrInfo creates a PrInfo for testing with common defaults
// Most common case: open PR with just a number
func NewTestPrInfo(number git.PRNumber) *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{
		Number: &number,
		State:  git.PRStateOpen,
	})
}

// NewTestPrInfoWithState creates a PrInfo with a specific state
func NewTestPrInfoWithState(number git.PRNumber, state git.PRState) *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{
		Number: &number,
		State:  state,
	})
}

// NewTestPrInfoMerged creates a PrInfo for a merged PR
func NewTestPrInfoMerged(number git.PRNumber, base string) *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{
		Number: &number,
		State:  "MERGED",
		Base:   base,
	})
}

// NewTestPrInfoClosed creates a PrInfo for a closed PR
func NewTestPrInfoClosed(number git.PRNumber) *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{
		Number: &number,
		State:  "CLOSED",
	})
}

// NewTestPrInfoDraft creates a PrInfo for a draft PR
func NewTestPrInfoDraft(number git.PRNumber) *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{
		Number:  &number,
		State:   git.PRStateOpen,
		IsDraft: true,
	})
}

// NewTestPrInfoWithTitle creates a PrInfo with a title
func NewTestPrInfoWithTitle(number git.PRNumber, title string) *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{
		Number: &number,
		Title:  title,
		State:  git.PRStateOpen,
	})
}

// NewTestPrInfoFull creates a PrInfo with all fields specified
func NewTestPrInfoFull(number git.PRNumber, title, body string, state git.PRState, base, url string, isDraft bool) *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{
		Number:  &number,
		Title:   title,
		Body:    body,
		State:   state,
		Base:    base,
		URL:     url,
		IsDraft: isDraft,
	})
}

// NewTestPrInfoEmpty creates an empty PrInfo (useful for clearing PR info)
func NewTestPrInfoEmpty() *engine.PrInfo {
	return engine.NewPrInfo(engine.PrInfoFields{})
}
