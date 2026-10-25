package tui

// Nerd Font glyphs (Material Design Icons). These are Private Use Area
// code points, so they render from the same face as a Nerd Font prompt.
// Color emoji such as U+1F4CD (📍) are not in that face and disappear
// unless the terminal has a separate emoji fallback.
const (
	iconPin     = "\U000f034e" // nf-md-map-marker
	iconHelp    = "\U000f0625" // nf-md-help-circle-outline
	iconCheck   = "\U000f012c" // nf-md-check-circle
	iconAlert   = "\U000f0026" // nf-md-alert-circle
	iconRefresh = "\U000f0450" // nf-md-refresh
)
