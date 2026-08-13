package app

import (
	"claude-squad/keys"
	"claude-squad/log"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// viewMode is which of the two screens the coordinator is showing: the list
// with one session in the pane beside it, or the mosaic with all of them at
// once.
type viewMode int

const (
	viewList viewMode = iota
	viewMosaic
)

// keyToBytes translates a key press into what a terminal would have sent, so it
// can be handed to the focused agent as if it had been typed into its own
// window.
//
// Bubble Tea numbers control keys with their own ASCII code (enter is 13, tab
// is 9, escape 27, backspace 127, ctrl-a through ctrl-z 1 to 26) and gives
// every other special key a negative number. That split is the whole rule: the
// positive ones are already the byte, the negative ones need a sequence.
func keyToBytes(msg tea.KeyMsg) string {
	// Alt-anything is the plain key with an escape in front of it.
	prefix := ""
	if msg.Alt {
		prefix = "\x1b"
	}

	// Space arrives as its own key type but still carries its rune, so it is
	// read the same way as any other typed character.
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		if len(msg.Runes) == 0 {
			return prefix + " "
		}
		return prefix + string(msg.Runes)
	}
	if msg.Type >= 0 && msg.Type <= 127 {
		return prefix + string([]byte{byte(msg.Type)})
	}

	var seq string
	switch msg.Type {
	case tea.KeyUp:
		seq = "\x1b[A"
	case tea.KeyDown:
		seq = "\x1b[B"
	case tea.KeyRight:
		seq = "\x1b[C"
	case tea.KeyLeft:
		seq = "\x1b[D"
	case tea.KeyShiftTab:
		seq = "\x1b[Z"
	case tea.KeyHome:
		seq = "\x1b[H"
	case tea.KeyEnd:
		seq = "\x1b[F"
	case tea.KeyPgUp:
		seq = "\x1b[5~"
	case tea.KeyPgDown:
		seq = "\x1b[6~"
	case tea.KeyDelete:
		seq = "\x1b[3~"
	default:
		// A key with no terminal representation is better dropped than sent as
		// something the agent would misread.
		return ""
	}
	return prefix + seq
}

// handleMosaicKey routes a key press while the mosaic is on screen. It reports
// whether it consumed the press; when it did not, the press falls through to
// the coordinator's normal keys, which keeps n/D/R/e/o meaning the same thing
// in both view modes.
func (m *home) handleMosaicKey(msg tea.KeyMsg) (cmd tea.Cmd, handled bool) {
	// Reading back through a cell's history works whether or not the cell holds
	// the keyboard: shift is what keeps these two out of the agent's way, since
	// no agent reads shift+arrow as anything.
	if lines, ok := mosaicScroll(msg.String()); ok {
		if m.mosaic.Scroll(m.list.GetSelectedInstance(), lines) {
			return m.instanceChanged(), true
		}
		return nil, true
	}

	// Focused: the agent owns the keyboard. Only the key that gives the screen
	// back to the coordinator is held back — everything else is the developer
	// talking to the agent, including q and D.
	if m.mosaic.Focused() {
		if msg.Type == tea.KeyCtrlL {
			m.mosaic.Blur()
			return nil, true
		}
		out := keyToBytes(msg)
		if out == "" {
			return nil, true
		}
		instance := m.mosaic.FocusedInstance()
		if err := m.mosaic.SendKeys(instance, out); err != nil {
			return m.handleError(err), true
		}
		// Typing into a session is the developer having seen it answer.
		instance.NeedsAttention = false
		return nil, true
	}

	// Esc brings the highlighted cell back to the live screen. It is only spelled
	// out for the cell that is actually frozen: esc with nothing to leave falls
	// through to whatever else it means.
	if msg.Type == tea.KeyEsc && m.mosaic.ScrollToLive(m.list.GetSelectedInstance()) {
		return m.instanceChanged(), true
	}

	// Navigating: enter takes the keyboard into the highlighted cell. It is
	// spelled out rather than read from the key map because the map sends both
	// enter and o to the same action, and here they part ways — o still opens
	// the session full screen.
	if msg.String() == "enter" {
		if err := m.mosaic.Focus(m.list.GetSelectedInstance()); err != nil {
			return m.handleError(err), true
		}
		return nil, true
	}

	// Tab switches the highlighted cell between the three panels — the agent, the
	// Cursor CLI and a shell — the same rotation the tabs follow in the list view,
	// but per cell: each session can be showing a different one.
	if msg.String() == "tab" {
		m.mosaic.CyclePanel(m.list.GetSelectedInstance())
		m.syncSessionSizes()
		return m.instanceChanged(), true
	}

	// Arrows are literal here: the sessions are on a grid, so left goes to the
	// cell on the left. Reading them off the global key map instead would move
	// one position in list order and look like the selection jumped screens.
	dCol, dRow, ok := mosaicDirection(msg.String())
	if !ok {
		return nil, false
	}
	m.list.SetSelectedInstance(m.mosaic.Move(m.list.GetInstances(), m.list.SelectedIdx(), dCol, dRow))
	return m.instanceChanged(), true
}

// mosaicScrollStep is how many lines one notch of the wheel, or one press of
// shift+arrow, walks back through a cell's history. Three is what a wheel notch
// scrolls in a terminal, and a cell is short enough that one line at a time
// would take a hundred presses to reach the top of an answer.
const mosaicScrollStep = 3

// mosaicScroll translates a key into a walk through the highlighted cell's
// history, counted in lines away from the live screen: up goes into the past,
// the way a terminal reads it.
func mosaicScroll(key string) (lines int, ok bool) {
	switch key {
	case "shift+up":
		return mosaicScrollStep, true
	case "shift+down":
		return -mosaicScrollStep, true
	}
	return 0, false
}

// mosaicDirection translates a navigation key into a step on the grid.
func mosaicDirection(key string) (dCol, dRow int, ok bool) {
	switch key {
	case "left", "h":
		return -1, 0, true
	case "right", "l":
		return 1, 0, true
	case "up", "k":
		return 0, -1, true
	case "down", "j":
		return 0, 1, true
	}
	return 0, 0, false
}

// toggleViewMode switches between the list and the mosaic, resizing every
// session's terminal to the shape it is about to be shown in.
func (m *home) toggleViewMode() tea.Cmd {
	if m.viewMode == viewMosaic {
		m.mosaic.Blur()
		m.viewMode = viewList
	} else {
		m.viewMode = viewMosaic
	}
	m.menu.SetInMosaic(m.viewMode == viewMosaic)
	if err := m.appState.SetViewMosaic(m.viewMode == viewMosaic); err != nil {
		log.ErrorLog.Printf("failed to persist view mode: %v", err)
	}
	// The two views split the screen differently — the mosaic leaves the menu one
	// line, the list gives it a tenth — so the whole layout is measured again.
	if m.winWidth > 0 && m.winHeight > 0 {
		m.updateHandleWindowSizeEvent(tea.WindowSizeMsg{Width: m.winWidth, Height: m.winHeight})
	} else {
		m.syncSessionSizes()
	}
	return m.instanceChanged()
}

// agentRenameCommand is what the agent is asked to do when the rename key is
// pressed. It is the agent's own slash command, sent as a line of input, not
// anything the coordinator does to its own record of the session — that is R.
const agentRenameCommand = "/rename"

// toggleMouseSelect hands the mouse to the terminal and back.
//
// While the app asks for mouse reporting, every click and drag is delivered to
// it as an event and the terminal never sees them — which is why dragging over
// a command an agent printed selects nothing. Giving the reporting up puts the
// terminal back in charge of the mouse, so selecting and copying work exactly as
// they do in any other program. The wheel is what it costs: those events stop
// arriving too, so scrolling a cell goes back to shift+arrows while this is on.
func (m *home) toggleMouseSelect() tea.Cmd {
	m.mouseSelect = !m.mouseSelect
	m.menu.SetMouseSelect(m.mouseSelect)
	if m.mouseSelect {
		return tea.DisableMouse
	}
	return tea.EnableMouseCellMotion
}

// syncSessionSizes gives every session the terminal shape of the view it is
// being shown in. A session sized for the preview pane and drawn in a mosaic
// cell renders at the wrong width, which reads as a broken agent rather than a
// mis-sized one.
func (m *home) syncSessionSizes() {
	if m.viewMode == viewMosaic {
		m.mosaicCount = m.list.NumInstances()
		// Cells no longer share one size: a row with a single session is wider
		// than a row with three, so each session is sized to its own cell.
		m.mosaic.SyncSizes(m.list.GetInstances())
		return
	}
	m.restorePreviewSize()
}

// mosaicView draws the mosaic with the menu and error box under it, matching
// the layout of the list view — down to the title bar on top, which is where
// the account-wide usage is read in either view.
func (m *home) mosaicView() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.list.Header(m.winWidth),
		m.mosaic.String(m.list.GetInstances(), m.list.SelectedIdx()),
	)
}

var (
	mosaicFormBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#ffd23f")).
				Padding(1, 3)

	mosaicFormTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffd23f"))

	mosaicFormLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"})

	mosaicFormHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#808080", Dark: "#808080"})
)

// newInstanceForm is the creation form drawn in the middle of the mosaic. The
// list view runs the same form inline under the session being created, but the
// mosaic has no list to write on, so it gets a box of its own.
func (m *home) newInstanceForm() string {
	if m.list.NumInstances() == 0 {
		return ""
	}
	instance := m.list.GetInstances()[m.list.NumInstances()-1]

	// The field being typed into carries the cursor; the other is just shown.
	name, dir := instance.Title, m.pathInput
	if m.newField == fieldPath {
		dir += "_"
	} else {
		name += "_"
	}

	hint := "enter confirma · esc cancela"
	if m.newField == fieldPath {
		switch n := len(m.pathCandidates); {
		case n == 0:
			hint = "sem diretório com esse nome · esc cancela"
		case n == 1:
			hint = "tab completa · enter cria · esc cancela"
		default:
			hint = fmt.Sprintf("tab percorre %d diretórios · enter cria · esc cancela", n)
		}
	}

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		mosaicFormTitleStyle.Render("Nova sessão"),
		"",
		mosaicFormLabelStyle.Render("nome       ")+name,
		mosaicFormLabelStyle.Render("diretório  ")+dir,
		"",
		mosaicFormHintStyle.Render(hint),
	)
	return mosaicFormBorderStyle.Width(52).Render(body)
}

// isMosaicKey reports whether a key belongs to the coordinator while the mosaic
// is up. The mosaic drops the tabs and the scroll mode, so the keys that drive
// them would do nothing but confuse.
func isMosaicKey(name keys.KeyName) bool {
	switch name {
	case keys.KeyTab, keys.KeyShiftUp, keys.KeyShiftDown, keys.KeyMoveUp, keys.KeyMoveDown:
		return false
	}
	return true
}
