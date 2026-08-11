package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// interactExitKey leaves interactive mode and returns to navigating the list
// and the tabs. It is the one key that is not forwarded to the agent —
// everything else, including ctrl+c, belongs to it.
const interactExitKey = "ctrl+l"

// keyToBytes translates a key press into the bytes a terminal would send, so it
// can be forwarded to the agent's terminal while the coordinator keeps drawing
// its own layout around it. Returns an empty string for keys with nothing
// meaningful to send.
func keyToBytes(msg tea.KeyMsg) string {
	switch msg.Type {
	case tea.KeyRunes:
		if msg.Alt {
			return "\x1b" + string(msg.Runes)
		}
		return string(msg.Runes)
	case tea.KeySpace:
		return " "
	case tea.KeyEnter:
		return "\r"
	case tea.KeyBackspace:
		return "\x7f"
	case tea.KeyDelete:
		return "\x1b[3~"
	case tea.KeyTab:
		return "\t"
	case tea.KeyShiftTab:
		return "\x1b[Z"
	case tea.KeyEsc:
		return "\x1b"
	case tea.KeyUp:
		return "\x1b[A"
	case tea.KeyDown:
		return "\x1b[B"
	case tea.KeyRight:
		return "\x1b[C"
	case tea.KeyLeft:
		return "\x1b[D"
	case tea.KeyShiftUp:
		return "\x1b[1;2A"
	case tea.KeyShiftDown:
		return "\x1b[1;2B"
	case tea.KeyHome:
		return "\x1b[H"
	case tea.KeyEnd:
		return "\x1b[F"
	case tea.KeyPgUp:
		return "\x1b[5~"
	case tea.KeyPgDown:
		return "\x1b[6~"
	}

	// Control combinations are contiguous in the key table: ctrl+a is 0x01 and
	// so on up to ctrl+z. Translating them by hand would be a 26-case switch.
	if b, ok := ctrlByte(msg.Type); ok {
		return string([]byte{b})
	}
	return ""
}

// ctrlByte maps a ctrl+<letter> key type to the byte a terminal sends for it.
func ctrlByte(t tea.KeyType) (byte, bool) {
	if t >= tea.KeyCtrlA && t <= tea.KeyCtrlZ {
		// KeyCtrlA is the first of the run and corresponds to 0x01.
		return byte(t-tea.KeyCtrlA) + 0x01, true
	}
	switch t {
	case tea.KeyCtrlOpenBracket:
		return 0x1b, true
	case tea.KeyCtrlBackslash:
		return 0x1c, true
	case tea.KeyCtrlCloseBracket:
		return 0x1d, true
	case tea.KeyCtrlCaret:
		return 0x1e, true
	case tea.KeyCtrlUnderscore:
		return 0x1f, true
	}
	return 0, false
}
