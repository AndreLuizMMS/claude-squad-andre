package ui

import (
	"claude-squad/keys"
	"strings"

	"github.com/charmbracelet/bubbles/key"

	"claude-squad/session"

	"github.com/charmbracelet/lipgloss"
)

var keyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{
	Light: "#3A3535",
	Dark:  "#E8E4E4",
})

var descStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#5A5555",
	Dark:  "#B8B2B2",
})

var sepStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#DDDADA",
	Dark:  "#3C3C3C",
})

var actionGroupStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))

var separator = " • "
var verticalSeparator = " │ "

var menuStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("205"))

// MenuState represents different states the menu can be in
type MenuState int

const (
	StateDefault MenuState = iota
	StateEmpty
	StateNewInstance
	StatePrompt
)

type Menu struct {
	options       []keys.KeyName
	height, width int
	state         MenuState
	instance      *session.Instance
	activeTab     int

	// inMosaic changes the wording of the keys that mean something else there:
	// v goes back to the list instead of opening the mosaic, and tab switches
	// the panel of one cell instead of the tab of the pane.
	inMosaic bool

	// mouseSelect is on while the mouse belongs to the terminal, which is the
	// one state the menu has to announce: nothing else on screen says why the
	// wheel stopped scrolling.
	mouseSelect bool

	// keyDown is the key which is pressed. The default is -1.
	keyDown keys.KeyName
}

var defaultMenuOptions = []keys.KeyName{keys.KeyNew, keys.KeyPrompt, keys.KeyHelp, keys.KeyQuit}
var newInstanceMenuOptions = []keys.KeyName{keys.KeySubmitName}
var promptMenuOptions = []keys.KeyName{keys.KeySubmitName}

func NewMenu() *Menu {
	return &Menu{
		options:   defaultMenuOptions,
		state:     StateEmpty,
		activeTab: 0,
		keyDown:   -1,
	}
}

func (m *Menu) Keydown(name keys.KeyName) {
	m.keyDown = name
}

func (m *Menu) ClearKeydown() {
	m.keyDown = -1
}

// SetState updates the menu state and options accordingly
func (m *Menu) SetState(state MenuState) {
	m.state = state
	m.updateOptions()
}

// SetInstance updates the current instance and refreshes menu options
func (m *Menu) SetInstance(instance *session.Instance) {
	m.instance = instance
	// Only change the state if we're not in a special state (NewInstance or Prompt)
	if m.state != StateNewInstance && m.state != StatePrompt {
		if m.instance != nil {
			m.state = StateDefault
		} else {
			m.state = StateEmpty
		}
	}
	m.updateOptions()
}

// SetActiveTab updates the currently active tab
func (m *Menu) SetActiveTab(tab int) {
	m.activeTab = tab
	m.updateOptions()
}

// SetInMosaic tells the menu which of the two views it is under.
func (m *Menu) SetInMosaic(inMosaic bool) {
	m.inMosaic = inMosaic
}

// SetMouseSelect tells the menu whether the mouse is currently the terminal's.
func (m *Menu) SetMouseSelect(on bool) {
	m.mouseSelect = on
	m.updateOptions()
}

// mosaicDescs are the keys the mosaic reads differently from the list.
var mosaicDescs = map[keys.KeyName]string{
	keys.KeyViewMode: "voltar à lista",
	keys.KeyTab:      "trocar painel",
	keys.KeyEnter:    "digitar na célula",
	keys.KeyShiftUp:  "rolar a célula",
}

// describe is the help text of a key in the view the menu is under.
func (m *Menu) describe(name keys.KeyName, binding key.Binding) string {
	// The selection key reads as the way out while it is on: it is a mode, and a
	// mode that does not say how to leave is a trap.
	if name == keys.KeyMouseSelect && m.mouseSelect {
		return "voltar o mouse ao app"
	}
	if m.inMosaic {
		if desc, ok := mosaicDescs[name]; ok {
			return desc
		}
	}
	return binding.Help().Desc
}

// updateOptions updates the menu options based on current state and instance
func (m *Menu) updateOptions() {
	switch m.state {
	case StateEmpty:
		m.options = defaultMenuOptions
	case StateDefault:
		if m.instance != nil {
			// When there is an instance, show that instance's options
			m.addInstanceOptions()
		} else {
			// When there is no instance, show the empty state
			m.options = defaultMenuOptions
		}
	case StateNewInstance:
		m.options = newInstanceMenuOptions
	case StatePrompt:
		m.options = promptMenuOptions
	}
}

func (m *Menu) addInstanceOptions() {
	// Loading instances only get minimal options
	if m.instance != nil && m.instance.Status == session.Loading {
		m.options = []keys.KeyName{keys.KeyNew, keys.KeyHelp, keys.KeyQuit}
		return
	}

	// Instance management group
	options := []keys.KeyName{keys.KeyNew, keys.KeyKill, keys.KeyRename}

	// A session that lost its directory can only be killed.
	if m.instance.Orphaned() {
		m.options = append(options, keys.KeyViewMode, keys.KeyTab, keys.KeyHelp, keys.KeyQuit)
		return
	}

	// Action group. Versioning is entirely the developer's business, so nothing
	// here touches git.
	// A session whose agent exited has no terminal to talk to: all it can do is
	// come back up, or be killed.
	if m.instance.HasExited() {
		m.options = append(options, keys.KeyResume, keys.KeyOpenEditor, keys.KeyViewMode,
			keys.KeyTab, keys.KeyHelp, keys.KeyQuit)
		return
	}

	actionGroup := []keys.KeyName{keys.KeyEnter, keys.KeySendPrompt, keys.KeyOpenEditor}
	if m.instance.Status == session.Paused {
		actionGroup = append(actionGroup, keys.KeyResume)
	} else {
		actionGroup = append(actionGroup, keys.KeyPause)
	}

	// Navigation group, offered wherever there is a history to read: the terminal
	// pane of the list view, and every cell of the mosaic.
	if m.inMosaic || m.activeTab != PreviewTab {
		actionGroup = append(actionGroup, keys.KeyShiftUp)
	}

	// System group
	systemGroup := []keys.KeyName{keys.KeyViewMode, keys.KeyMouseSelect, keys.KeyTab, keys.KeyHelp, keys.KeyQuit}

	// Combine all groups
	options = append(options, actionGroup...)
	options = append(options, systemGroup...)

	m.options = options
}

// SetSize sets the width of the window. The menu will be centered horizontally within this width.
func (m *Menu) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Menu) String() string {
	var s strings.Builder

	// Define group boundaries
	groups := []struct {
		start int
		end   int
	}{
		{0, 2}, // Instance management group (n, d)
		{2, 5}, // Action group (enter, submit, pause/resume)
		{6, 8}, // System group (tab, help, q)
	}

	for i, k := range m.options {
		binding := keys.GlobalkeyBindings[k]

		var (
			localActionStyle = actionGroupStyle
			localKeyStyle    = keyStyle
			localDescStyle   = descStyle
		)
		if m.keyDown == k {
			// Pressed feedback: invert the key itself so the press reads as an
			// instant flash, not just an underline that's easy to miss.
			localActionStyle = localActionStyle.Reverse(true).Bold(true)
			localKeyStyle = localKeyStyle.Reverse(true).Bold(true)
			localDescStyle = localDescStyle.Underline(true)
		}

		var inActionGroup bool
		switch m.state {
		case StateEmpty:
			// For empty state, the action group is the first group
			inActionGroup = i <= 1
		default:
			// For other states, the action group is the second group
			inActionGroup = i >= groups[1].start && i < groups[1].end
		}

		desc := m.describe(k, binding)
		if inActionGroup {
			s.WriteString(localActionStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localActionStyle.Render(desc))
		} else {
			s.WriteString(localKeyStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localDescStyle.Render(desc))
		}

		// Add appropriate separator
		if i != len(m.options)-1 {
			isGroupEnd := false
			for _, group := range groups {
				if i == group.end-1 {
					s.WriteString(sepStyle.Render(verticalSeparator))
					isGroupEnd = true
					break
				}
			}
			if !isGroupEnd {
				s.WriteString(sepStyle.Render(separator))
			}
		}
	}

	centeredMenuText := menuStyle.Render(s.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, centeredMenuText)
}
