package testhelpers

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// NewTestPrInfo creates a PrInfo for testing with common defaults
// Most common case: open PR with just a number
func NewTestPrInfo(number int) *engine.PrInfo {
	return engine.NewPrInfo(
		&number,
		"",
		"",
		"OPEN",
		"",
		"",
		false,
	)
}

// NewTestPrInfoWithState creates a PrInfo with a specific state
func NewTestPrInfoWithState(number int, state git.PRState) *engine.PrInfo {
	return engine.NewPrInfo(
		&number,
		"",
		"",
		state,
		"",
		"",
		false,
	)
}

// NewTestPrInfoMerged creates a PrInfo for a merged PR
func NewTestPrInfoMerged(number int, base string) *engine.PrInfo {
	return engine.NewPrInfo(
		&number,
		"",
		"",
		"MERGED",
		base,
		"",
		false,
	)
}

// NewTestPrInfoClosed creates a PrInfo for a closed PR
func NewTestPrInfoClosed(number int) *engine.PrInfo {
	return engine.NewPrInfo(
		&number,
		"",
		"",
		"CLOSED",
		"",
		"",
		false,
	)
}

// NewTestPrInfoDraft creates a PrInfo for a draft PR
func NewTestPrInfoDraft(number int) *engine.PrInfo {
	return engine.NewPrInfo(
		&number,
		"",
		"",
		"OPEN",
		"",
		"",
		true,
	)
}

// NewTestPrInfoWithTitle creates a PrInfo with a title
func NewTestPrInfoWithTitle(number int, title string) *engine.PrInfo {
	return engine.NewPrInfo(
		&number,
		title,
		"",
		"OPEN",
		"",
		"",
		false,
	)
}

// NewTestPrInfoFull creates a PrInfo with all fields specified
func NewTestPrInfoFull(number int, title, body string, state git.PRState, base, url string, isDraft bool) *engine.PrInfo {
	return engine.NewPrInfo(
		&number,
		title,
		body,
		state,
		base,
		url,
		isDraft,
	)
}

// NewTestPrInfoEmpty creates an empty PrInfo (useful for clearing PR info)
func NewTestPrInfoEmpty() *engine.PrInfo {
	return engine.NewPrInfo(
		nil,
		"",
		"",
		"",
		"",
		"",
		false,
	)
}
