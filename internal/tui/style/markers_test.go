package style

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestStepPips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		current  int
		total    int
		expected string
	}{
		{"first step", 1, 5, "●○○○○"},
		{"middle step", 3, 5, "●●●○○"},
		{"last step", 5, 5, "●●●●●"},
		{"single step", 1, 1, "●"},
		{"zero total renders nothing", 0, 0, ""},
		{"zero current renders nothing", 0, 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Strip color: the pip pattern is the contract, the palette is not.
			assert.Equal(t, tt.expected, ansi.Strip(StepPips(tt.current, tt.total)))
		})
	}
}
