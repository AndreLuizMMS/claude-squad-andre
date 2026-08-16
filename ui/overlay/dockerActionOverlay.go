package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dockerActions is the docker compose verb run for each key, in the order
// they are listed to the developer — the single source of truth both
// HandleKeyPress and Render read from. Just the verb, not the full command:
// the caller (TabbedWindow.SendDockerCommand) is the one that knows which
// compose file this session actually has, and prefixes `docker compose -f
// <that file>` onto it.
var dockerActions = []struct {
	key, label, verb string
}{
	{"l", "Logs", "logs -f --tail=200"},
	{"r", "Restart", "restart"},
	{"x", "Stop", "stop"},
	{"u", "Up", "up -d"},
}

// DockerActionOverlay is the menu the Docker tab opens on 'a': one key per
// docker compose action, no navigation — the same directness
// ConfirmationOverlay uses for y/n.
type DockerActionOverlay struct {
	width int
	// Command is the docker compose verb for the action the developer
	// picked (e.g. "restart"), set by HandleKeyPress. Empty when the
	// overlay was cancelled instead.
	Command string
}

func NewDockerActionOverlay() *DockerActionOverlay {
	return &DockerActionOverlay{width: 40}
}

// HandleKeyPress processes a key press and reports whether the overlay
// should close — true on esc and on every action key, false on anything
// else (the overlay stays open on a key it doesn't recognize).
func (d *DockerActionOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	if msg.String() == "esc" {
		return true
	}
	for _, a := range dockerActions {
		if msg.String() == a.key {
			d.Command = a.verb
			return true
		}
	}
	return false
}

// Render draws the action menu: one line per key, styled the same purple
// border TextInputOverlay and the profile picker already use for a modal
// that isn't a yes/no confirmation.
func (d *DockerActionOverlay) Render(opts ...WhitespaceOption) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(d.width)

	keyStyle := lipgloss.NewStyle().Bold(true)
	lines := []string{"Docker", ""}
	for _, a := range dockerActions {
		lines = append(lines, keyStyle.Render(a.key)+"  "+a.label)
	}
	lines = append(lines, "", keyStyle.Render("esc")+"  cancelar")

	return style.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
