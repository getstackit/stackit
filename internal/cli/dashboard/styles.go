package dashboard

import (
	"sync"

	"github.com/getstackit/stackit/internal/tui/style"
)

// All dashboard styles are defined in style.DefaultDashboardStyles() so that
// the st-tui storyboard stays in sync with the real dashboard.
var ds = style.DefaultDashboardStyles()

// Re-export for convenient access within this package. DefaultDashboardStyles
// uses only literal color codes, so this initializer is safe to run at
// package-init time.
var (
	titleStyle        = ds.Title
	headerStatusStyle = ds.HeaderStatus
	headerBorderStyle = ds.HeaderBorder
	paneHeaderStyle   = ds.PaneHeader
	selectedRowStyle  = ds.SelectedRow
	footerStyle       = ds.Footer
	errorTextStyle    = ds.ErrorText

	leftPaneStyle  = ds.LeftPane
	rightPaneStyle = ds.RightPane
	actionBarStyle = ds.ActionBar

	badgeReady      = ds.BadgeReady
	badgePending    = ds.BadgePending
	badgeBlocked    = ds.BadgeBlocked
	badgeIncomplete = ds.BadgeIncomplete

	buttonPrimary = ds.ButtonPrimary

	helpTitleStyle   = ds.HelpTitle
	helpSectionStyle = ds.HelpSection
	helpKeyStyle     = ds.HelpKey
	helpDescStyle    = ds.HelpDesc

	dialogStyle = ds.Dialog
)

// commonStyles is loaded lazily because style.DefaultCommonStyles() calls
// lipgloss.HasDarkBackground (via DimStyle/SubtleStyle), which writes an
// OSC 11 + DA1 query pair to stdout to ask the terminal for its background
// color. Loading at package-init time means those queries leak as visible
// escape codes on every CLI invocation — even ones like `stackit log` or
// `stackit version` that don't render adaptive styles at all — because the
// dashboard package is transitively imported by the root command.
var (
	commonStylesOnce sync.Once
	commonStylesV    style.CommonStyles
)

func commonStyles() style.CommonStyles {
	commonStylesOnce.Do(func() {
		commonStylesV = style.DefaultCommonStyles()
	})
	return commonStylesV
}
