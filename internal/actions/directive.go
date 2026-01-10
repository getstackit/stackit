// Package actions provides shared types for action results.
package actions

// ShellDirective represents a shell integration directive to be emitted by the CLI.
// Actions populate this struct to indicate what shell operations should happen,
// and the CLI layer is responsible for actually emitting the directives.
type ShellDirective struct {
	// CDPath is the path to change directory to (empty = no cd needed)
	CDPath string
	// RerunArgs are arguments to re-run stackit with after cd (nil = no rerun)
	RerunArgs []string
	// FallbackTip is shown when shell integration is not available.
	// If empty, no tip is shown.
	FallbackTip string
}

// HasDirective returns true if this directive contains any actions to perform.
func (d *ShellDirective) HasDirective() bool {
	return d != nil && (d.CDPath != "" || len(d.RerunArgs) > 0)
}
