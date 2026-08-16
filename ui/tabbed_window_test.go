package ui

import (
	"claude-squad/log"
	"claude-squad/session/tmux"
	"os"
	"path/filepath"
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

// TestSendDockerCommandInterruptsThenTypes proves the two-step send: the raw
// Ctrl-C byte first, then the command plus enter, all into the Docker pane's
// session PTY.
func TestSendDockerCommandInterruptsThenTypes(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	instance := makeStartedInstance(t, "docker-send")
	defer func() { _ = instance.Kill() }()
	composeFile := filepath.Join(instance.Path, "docker-compose.yml")
	require.NoError(t, os.WriteFile(composeFile, []byte(""), 0644))

	w := newTestTabbedWindow()
	ptyFactory := &MockPtyFactory{t: t, cmdExec: mockCmdExec("", true)}
	ts := tmux.NewTmuxSessionWithDeps("docker-send-pane", "bash", ptyFactory, mockCmdExec("", true))
	require.NoError(t, ts.Restore()) // opens the mock PTY the writes land in
	injectSession(w.docker, instance.ID(), ts, instance.Path)

	require.NoError(t, w.SendDockerCommand(instance, "restart"))

	written, err := os.ReadFile(ptyFactory.files[0].Name())
	require.NoError(t, err)
	require.Equal(t, "\x03docker compose -f '"+composeFile+"' restart\r", string(written))
}
