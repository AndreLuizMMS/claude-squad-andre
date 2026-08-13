package app

import (
	"claude-squad/keys"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Typing into a mosaic cell only works if a key press comes out as the bytes a
// terminal would have sent. Getting this wrong is invisible in review and
// obvious in use — enter that does not submit, backspace that does nothing.
func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"letras", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("oi")}, "oi"},
		{"acentos", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ção")}, "ção"},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"espaço", tea.KeyMsg{Type: tea.KeySpace}, " "},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{"ctrl-c", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{"seta cima", tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{"seta baixo", tea.KeyMsg{Type: tea.KeyDown}, "\x1b[B"},
		{"seta direita", tea.KeyMsg{Type: tea.KeyRight}, "\x1b[C"},
		{"seta esquerda", tea.KeyMsg{Type: tea.KeyLeft}, "\x1b[D"},
		{"shift-tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "\x1b[Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyToBytes(tt.msg); got != tt.want {
				t.Fatalf("keyToBytes(%s) = %q, esperado %q", tt.name, got, tt.want)
			}
		})
	}
}

// The tabs, the per-pane scrolling and the reordering do not exist in the
// mosaic, so their keys must not half-work there.
func TestIsMosaicKey(t *testing.T) {
	dropped := []keys.KeyName{
		keys.KeyTab, keys.KeyShiftUp, keys.KeyShiftDown, keys.KeyMoveUp, keys.KeyMoveDown,
	}
	for _, k := range dropped {
		if isMosaicKey(k) {
			t.Fatalf("tecla %v não deveria valer no mosaico", k)
		}
	}

	kept := []keys.KeyName{
		keys.KeyNew, keys.KeyKill, keys.KeyRename, keys.KeyUp, keys.KeyDown,
		keys.KeyEnter, keys.KeyPause, keys.KeyResume, keys.KeyViewMode, keys.KeyQuit,
	}
	for _, k := range kept {
		if !isMosaicKey(k) {
			t.Fatalf("tecla %v deveria valer no mosaico", k)
		}
	}
}
