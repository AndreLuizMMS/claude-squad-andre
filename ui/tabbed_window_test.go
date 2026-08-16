package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestTabbedWindow() *TabbedWindow {
	return NewTabbedWindow(NewPreviewPane(), NewTerminalPane(), NewAgentPane(), NewDockerPane())
}

func TestTabbedWindowHasFourTabsIncludingDocker(t *testing.T) {
	w := newTestTabbedWindow()
	require.Len(t, w.tabs, 4)
	require.Equal(t, "Docker", w.tabs[DockerTab])
}

func TestTabbedWindowToggleReachesDocker(t *testing.T) {
	w := newTestTabbedWindow()
	for i := 0; i < DockerTab; i++ {
		w.Toggle()
	}
	require.Equal(t, DockerTab, w.GetActiveTab())

	// One more toggle wraps back to the first tab.
	w.Toggle()
	require.Equal(t, PreviewTab, w.GetActiveTab())
}

func TestTabbedWindowActiveTerminalOnDockerTab(t *testing.T) {
	w := newTestTabbedWindow()
	w.activeTab = DockerTab
	require.Same(t, w.docker, w.activeTerminal())
	require.True(t, w.IsInTerminalTab())
}
