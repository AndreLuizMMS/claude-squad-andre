package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestDockerActionOverlayPicksCommand(t *testing.T) {
	tests := []struct {
		key     string
		command string
	}{
		{"l", "logs -f --tail=200"},
		{"r", "restart"},
		{"x", "stop"},
		{"u", "up -d"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			d := NewDockerActionOverlay()
			shouldClose := d.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			require.True(t, shouldClose)
			require.Equal(t, tt.command, d.Command)
		})
	}
}

func TestDockerActionOverlayEscCancelsWithoutCommand(t *testing.T) {
	d := NewDockerActionOverlay()
	shouldClose := d.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, shouldClose)
	require.Empty(t, d.Command)
}

func TestDockerActionOverlayIgnoresUnknownKeys(t *testing.T) {
	d := NewDockerActionOverlay()
	shouldClose := d.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	require.False(t, shouldClose)
	require.Empty(t, d.Command)
}
