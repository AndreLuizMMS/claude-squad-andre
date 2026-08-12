package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// Drives the real Update loop, including the menu-highlight key re-send.
func TestKillGoesThroughUpdateLoop(t *testing.T) {
	h := newTestHome(t)
	startForm(t, h, "sess")
	h.state = stateDefault
	h.keySent = false

	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}}
	_, cmd := h.Update(key)
	require.Equal(t, stateDefault, h.state, "first press only highlights the menu")
	require.NotNil(t, cmd)

	// The highlight batch re-sends the same key.
	_, _ = h.Update(key)
	require.Equal(t, stateConfirm, h.state, "re-sent key must open the confirmation")
	require.Equal(t, 1, h.list.NumInstances(), "nothing killed before confirming")
}
