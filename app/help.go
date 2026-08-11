package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type helpText interface {
	// toContent returns the help UI content.
	toContent() string
	// mask returns the bit mask for this help text. These are used to track which help screens
	// have been seen in the config and app state.
	mask() uint32
}

type helpTypeGeneral struct{}

type helpTypeInstanceStart struct {
	instance *session.Instance
}

type helpTypeInstanceAttach struct{}

type helpTypeInstancePause struct{}

func helpStart(instance *session.Instance) helpText {
	return helpTypeInstanceStart{instance: instance}
}

func (h helpTypeGeneral) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Claude Squad"),
		"",
		"A terminal UI that manages multiple Claude Code (and other local agents), each in its own directory.",
		"",
		headerStyle.Render("Managing:"),
		keyStyle.Render("n")+descStyle.Render("         - Create a new session"),
		keyStyle.Render("N")+descStyle.Render("         - Create a new session with a prompt"),
		keyStyle.Render("D")+descStyle.Render("         - Kill (delete) the selected session"),
		keyStyle.Render("↑/j, ↓/k")+descStyle.Render("  - Navigate between sessions"),
		keyStyle.Render("J/K")+descStyle.Render("       - Reorder sessions"),
		keyStyle.Render("↵/o")+descStyle.Render("       - Attach to the selected session"),
		keyStyle.Render("ctrl-q")+descStyle.Render("    - Detach from session"),
		"",
		headerStyle.Render("Session state:"),
		keyStyle.Render("c")+descStyle.Render("         - Pause: close the terminal, keep the session"),
		keyStyle.Render("r")+descStyle.Render("         - Resume a paused session"),
		"",
		headerStyle.Render("Other:"),
		keyStyle.Render("tab")+descStyle.Render("       - Switch between preview, diff, and terminal tabs"),
		keyStyle.Render("shift-↓/↑")+descStyle.Render(" - Scroll in preview/diff/terminal view"),
		keyStyle.Render("q")+descStyle.Render("         - Quit the application"),
	)
	return content
}

func (h helpTypeInstanceStart) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Instance Created"),
		"",
		descStyle.Render("New session created:"),
		descStyle.Render(fmt.Sprintf("• Working directory: %s",
			lipgloss.NewStyle().Bold(true).Render(h.instance.Path))),
		descStyle.Render(fmt.Sprintf("• %s running in background tmux session",
			lipgloss.NewStyle().Bold(true).Render(h.instance.Program))),
		"",
		descStyle.Render("The agent edits that directory directly. Versioning is yours to drive."),
		"",
		headerStyle.Render("Managing:"),
		keyStyle.Render("↵/o")+descStyle.Render("   - Attach to the session to interact with it directly"),
		keyStyle.Render("tab")+descStyle.Render("   - Switch preview panes to view session diff"),
		keyStyle.Render("D")+descStyle.Render("     - Kill (delete) the selected session"),
		keyStyle.Render("c")+descStyle.Render("     - Pause: close the terminal, keep the session"),
	)
	return content
}

func (h helpTypeInstanceAttach) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Attaching to Instance"),
		"",
		descStyle.Render("To detach from a session, press ")+keyStyle.Render("ctrl-q"),
	)
	return content
}

func (h helpTypeInstancePause) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Pause Session"),
		"",
		"The terminal will be closed. Your directory is left exactly as it is — nothing is committed and nothing is removed.",
		"",
		"Resuming reopens the terminal in the same directory, in whatever state it is in then.",
		"",
		"Note: the agent loses its conversation context when paused.",
		"",
		headerStyle.Render("Commands:"),
		keyStyle.Render("c")+descStyle.Render(" - Pause the session"),
		keyStyle.Render("r")+descStyle.Render(" - Resume a paused session"),
	)
	return content
}

func (h helpTypeGeneral) mask() uint32 {
	return 1
}

func (h helpTypeInstanceStart) mask() uint32 {
	return 1 << 1
}
func (h helpTypeInstanceAttach) mask() uint32 {
	return 1 << 2
}
func (h helpTypeInstancePause) mask() uint32 {
	return 1 << 3
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#7D56F4"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#36CFC9"))
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFCC00"))
	descStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
)

// showHelpScreen displays the help screen overlay if it hasn't been shown before
func (m *home) showHelpScreen(helpType helpText, onDismiss func()) (tea.Model, tea.Cmd) {
	// Get the flag for this help type
	var alwaysShow bool
	switch helpType.(type) {
	case helpTypeGeneral:
		alwaysShow = true
	}

	flag := helpType.mask()

	// Check if this help screen has been seen before
	// Only show if we're showing the general help screen or the corresponding flag is not set
	// in the seen bitmask.
	if alwaysShow || (m.appState.GetHelpScreensSeen()&flag) == 0 {
		// Mark this help screen as seen and save state
		if err := m.appState.SetHelpScreensSeen(m.appState.GetHelpScreensSeen() | flag); err != nil {
			log.WarningLog.Printf("Failed to save help screen state: %v", err)
		}

		content := helpType.toContent()

		m.textOverlay = overlay.NewTextOverlay(content)
		m.textOverlay.OnDismiss = onDismiss
		m.state = stateHelp
		return m, nil
	}

	// Skip displaying the help screen. The dismissal callback still runs, and it
	// still needs the redraw that dismissing the overlay would have produced:
	// attaching resizes the agent's terminal to the full window, so without a
	// resize on the way back the panes stay drawn at the wrong size.
	if onDismiss != nil {
		onDismiss()
		return m, tea.WindowSize()
	}
	return m, nil
}

// handleHelpState handles key events when in help state
func (m *home) handleHelpState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key press will close the help overlay
	shouldClose := m.textOverlay.HandleKeyPress(msg)
	if shouldClose {
		m.state = stateDefault
		return m, tea.Sequence(
			tea.WindowSize(),
			func() tea.Msg {
				m.menu.SetState(ui.StateDefault)
				return nil
			},
		)
	}

	return m, nil
}
