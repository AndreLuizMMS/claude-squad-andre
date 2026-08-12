package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┘", " ", "└")
	highlightColor    = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	inactiveTabStyle  = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(highlightColor).
				AlignHorizontal(lipgloss.Center)
	activeTabStyle = inactiveTabStyle.
			Border(activeTabBorder, true).
			AlignHorizontal(lipgloss.Center)
	windowStyle = lipgloss.NewStyle().
			BorderForeground(highlightColor).
			Border(lipgloss.NormalBorder(), false, true, true, true)
)

const (
	PreviewTab int = iota
	AgentTab
	TerminalTab
)

type Tab struct {
	Name   string
	Render func(width int, height int) string
}

// TabbedWindow has tabs at the top of a pane which can be selected. The tabs
// take up one rune of height.
type TabbedWindow struct {
	tabs []string

	activeTab int
	height    int
	width     int

	preview  *PreviewPane
	terminal *TerminalPane
	agent    *TerminalPane
	instance *session.Instance
}

func NewTabbedWindow(preview *PreviewPane, terminal, agent *TerminalPane) *TabbedWindow {
	return &TabbedWindow{
		tabs: []string{
			"Claude Code",
			"Cursor CLI",
			"Bash $",
		},
		preview:  preview,
		terminal: terminal,
		agent:    agent,
	}
}

// activeTerminal returns the shell-backed pane for the active tab, or nil when
// the preview tab is active.
func (w *TabbedWindow) activeTerminal() *TerminalPane {
	switch w.activeTab {
	case TerminalTab:
		return w.terminal
	case AgentTab:
		return w.agent
	}
	return nil
}

func (w *TabbedWindow) SetInstance(instance *session.Instance) {
	w.instance = instance
}

// AdjustPreviewWidth trims a couple columns off the provided width so borders
// never wrap. A fixed margin instead of a percentage: on a wide terminal a
// proportional cut throws away a lot of real screen space for no reason.
func AdjustPreviewWidth(width int) int {
	adjusted := width - 2
	if adjusted < 1 {
		adjusted = width
	}
	return adjusted
}

func (w *TabbedWindow) SetSize(width, height int) {
	w.width = AdjustPreviewWidth(width)
	w.height = height

	// Calculate the content height by subtracting:
	// 1. Tab height (including border and padding)
	// 2. Window style vertical frame size
	// 3. Additional padding/spacing (2 for the newline and spacing)
	tabHeight := activeTabStyle.GetVerticalFrameSize() + 1
	contentHeight := height - tabHeight - windowStyle.GetVerticalFrameSize() - 2
	contentWidth := w.width - windowStyle.GetHorizontalFrameSize()

	w.preview.SetSize(contentWidth, contentHeight)
	w.terminal.SetSize(contentWidth, contentHeight)
	w.agent.SetSize(contentWidth, contentHeight)
}

func (w *TabbedWindow) GetPreviewSize() (width, height int) {
	return w.preview.width, w.preview.height
}

func (w *TabbedWindow) Toggle() {
	w.activeTab = (w.activeTab + 1) % len(w.tabs)
}

// UpdatePreview updates the content of the preview pane. instance may be nil.
func (w *TabbedWindow) UpdatePreview(instance *session.Instance) error {
	if w.activeTab != PreviewTab {
		return nil
	}
	return w.preview.UpdateContent(instance)
}

// UpdateTerminal updates the active shell pane content (terminal or Cursor).
func (w *TabbedWindow) UpdateTerminal(instance *session.Instance) error {
	pane := w.activeTerminal()
	if pane == nil {
		return nil
	}
	return pane.UpdateContent(instance)
}

// ResetPreviewToNormalMode resets the preview pane to normal mode
func (w *TabbedWindow) ResetPreviewToNormalMode(instance *session.Instance) error {
	return w.preview.ResetToNormalMode(instance)
}

// Add these new methods for handling scroll events
func (w *TabbedWindow) ScrollUp() {
	if pane := w.activeTerminal(); pane != nil {
		if err := pane.ScrollUp(); err != nil {
			log.InfoLog.Printf("tabbed window failed to scroll terminal up: %v", err)
		}
		return
	}
	if err := w.preview.ScrollUp(w.instance); err != nil {
		log.InfoLog.Printf("tabbed window failed to scroll up: %v", err)
	}
}

func (w *TabbedWindow) ScrollDown() {
	if pane := w.activeTerminal(); pane != nil {
		if err := pane.ScrollDown(); err != nil {
			log.InfoLog.Printf("tabbed window failed to scroll terminal down: %v", err)
		}
		return
	}
	if err := w.preview.ScrollDown(w.instance); err != nil {
		log.InfoLog.Printf("tabbed window failed to scroll down: %v", err)
	}
}

// IsInPreviewTab returns true if the preview tab is currently active
func (w *TabbedWindow) IsInPreviewTab() bool {
	return w.activeTab == PreviewTab
}

// IsInTerminalTab returns true when the active tab is backed by a shell pane
// (Terminal or Cursor) — both attach and scroll the same way.
func (w *TabbedWindow) IsInTerminalTab() bool {
	return w.activeTerminal() != nil
}

// GetActiveTab returns the currently active tab index
func (w *TabbedWindow) GetActiveTab() int {
	return w.activeTab
}

// AttachTerminal attaches to the tmux session behind the active shell pane.
func (w *TabbedWindow) AttachTerminal() (chan struct{}, error) {
	pane := w.activeTerminal()
	if pane == nil {
		return nil, fmt.Errorf("no terminal session to attach to")
	}
	return pane.Attach()
}

// CleanupTerminal closes the terminal and Cursor sessions
func (w *TabbedWindow) CleanupTerminal() {
	w.terminal.Close()
	w.agent.Close()
}

// CleanupTerminalForInstance closes the cached shell sessions for the given instance title.
func (w *TabbedWindow) CleanupTerminalForInstance(title string) {
	w.terminal.CloseForInstance(title)
	w.agent.CloseForInstance(title)
}

// IsPreviewInScrollMode returns true if the preview pane is in scroll mode
func (w *TabbedWindow) IsPreviewInScrollMode() bool {
	return w.preview.isScrolling
}

// IsTerminalInScrollMode returns true if the active shell pane is in scroll mode
func (w *TabbedWindow) IsTerminalInScrollMode() bool {
	pane := w.activeTerminal()
	return pane != nil && pane.IsScrolling()
}

// ResetTerminalToNormalMode exits scroll mode on the active shell pane
func (w *TabbedWindow) ResetTerminalToNormalMode() {
	if pane := w.activeTerminal(); pane != nil {
		pane.ResetToNormalMode()
	}
}

func (w *TabbedWindow) String() string {
	if w.width == 0 || w.height == 0 {
		return ""
	}

	var renderedTabs []string

	totalTabWidth := w.width + windowStyle.GetHorizontalFrameSize()
	tabWidth := totalTabWidth / len(w.tabs)
	lastTabWidth := totalTabWidth - tabWidth*(len(w.tabs)-1)
	tabHeight := activeTabStyle.GetVerticalFrameSize() + 1 // get padding border margin size + 1 for character height

	for i, t := range w.tabs {
		width := tabWidth
		if i == len(w.tabs)-1 {
			width = lastTabWidth
		}

		var style lipgloss.Style
		isFirst, isLast, isActive := i == 0, i == len(w.tabs)-1, i == w.activeTab
		if isActive {
			style = activeTabStyle
		} else {
			style = inactiveTabStyle
		}
		border, _, _, _, _ := style.GetBorder()
		if isFirst && isActive {
			border.BottomLeft = "│"
		} else if isFirst {
			border.BottomLeft = "├"
		} else if isLast && isActive {
			border.BottomRight = "│"
		} else if isLast {
			border.BottomRight = "┤"
		}
		style = style.Border(border)
		style = style.Width(width - style.GetHorizontalFrameSize())
		renderedTabs = append(renderedTabs, style.Render(t))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	content := w.preview.String()
	if pane := w.activeTerminal(); pane != nil {
		content = pane.String()
	}
	window := windowStyle.Render(
		lipgloss.Place(
			w.width, w.height-2-windowStyle.GetVerticalFrameSize()-tabHeight,
			lipgloss.Left, lipgloss.Top, content))

	return lipgloss.JoinVertical(lipgloss.Left, "\n", row, window)
}
