package foreach

// Handler receives events from the foreach action and handles user interaction.
// Implementations should handle events appropriately for their UI context
// (interactive terminal, non-interactive, dashboard, etc.)
type Handler interface {
	// OnEvent is called for each event during execution.
	// Handlers should use type switches to handle specific event types.
	OnEvent(event Event)
}
