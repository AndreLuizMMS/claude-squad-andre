package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHome builds a home wired well enough to drive the creation form.
func newTestHome(t *testing.T) *home {
	t.Helper()
	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
		menu:      ui.NewMenu(),
		errBox:    ui.NewErrBox(),
		program:   "bash",
	}
	sp := spinner.New()
	h.spinner = sp
	h.list = ui.NewList(&h.spinner, false)
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewTerminalPane(), ui.NewAgentPane(), ui.NewDockerPane())
	return h
}

// press delivers a key straight to the handler. keySent skips the menu-highlight
// round trip, which in the real program re-sends the same key via a tea.Cmd.
func press(h *home, msg tea.KeyMsg) {
	h.keySent = true
	h.handleKeyPress(msg)
}

func typeRunes(t *testing.T, h *home, s string) {
	t.Helper()
	for _, r := range s {
		press(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func pressEnter(h *home) {
	press(h, tea.KeyMsg{Type: tea.KeyEnter})
}

// startForm puts home into the creation form, bypassing the menu-highlight
// round trip that swallows the first key press. The form only asks for the
// working directory — the title fills itself in from the directory name, and
// ctrl+r later copies over whatever the agent names its own conversation.
func startForm(t *testing.T, h *home) *session.Instance {
	t.Helper()
	inst, err := h.newBlankInstance()
	require.NoError(t, err)
	h.beginNewInstance(inst)
	return inst
}

func TestCreationFormAsksOnlyForDirectory(t *testing.T) {
	dir := t.TempDir()
	h := newTestHome(t)

	inst := startForm(t, h)
	assert.Equal(t, homePrefix, h.pathInput, "the directory field always starts at home")

	// Confirming the directory is the only step: the session starts right away,
	// titled after the directory.
	h.setPathInput(dir)
	pressEnter(h)
	assert.Equal(t, stateDefault, h.state)
	assert.Equal(t, session.Loading, inst.Status)
	assert.Equal(t, dir, inst.Path)
	assert.Equal(t, inst.DirName(), inst.Title)
}

// Letters that double as global shortcuts ("c", "o", "r") must be typed, not
// re-sent through a tea.Cmd: the round trip reorders them and "cortz" lands as
// "roctz".
func TestTypingShortcutLettersIntoTheDirectoryKeepsTheirOrder(t *testing.T) {
	h := newTestHome(t)
	startForm(t, h)

	for _, r := range "cortz" {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		_, returnEarly := h.handleMenuHighlighting(msg)
		require.False(t, returnEarly, "typing %q must not take the menu-highlight round trip", r)
		h.handleKeyPress(msg)
	}
	assert.Equal(t, homePrefix+"cortz", h.pathInput)
}

func TestCreationFormRejectsMissingDirectoryAndKeepsTheTypedValue(t *testing.T) {
	h := newTestHome(t)
	startForm(t, h)

	missing := filepath.Join(t.TempDir(), "nope")
	h.setPathInput(missing)
	pressEnter(h)

	assert.Equal(t, stateNew, h.state, "the developer stays on the creation form")
	assert.Equal(t, missing, h.pathInput, "the typed value is preserved")
	assert.Contains(t, h.errBox.String(), "does not exist")
}

func TestCreationFormAcceptsANonVersionedDirectory(t *testing.T) {
	dir := t.TempDir()
	h := newTestHome(t)
	inst := startForm(t, h)
	h.setPathInput(dir)
	pressEnter(h) // confirm

	assert.Equal(t, stateDefault, h.state, "no repository is required")
	assert.Equal(t, session.Loading, inst.Status)
	assert.Equal(t, dir, inst.Path)
}

func TestCreationFormExpandsHomeShortcut(t *testing.T) {
	h := newTestHome(t)
	inst := startForm(t, h)
	h.setPathInput("~")
	pressEnter(h)

	assert.True(t, filepath.IsAbs(inst.Path))
	assert.NotContains(t, inst.Path, "~")
}

func TestCancelingCreationLeavesNothingBehind(t *testing.T) {
	h := newTestHome(t)
	startForm(t, h)

	press(h, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, stateDefault, h.state)
	assert.Equal(t, 0, h.list.NumInstances())
}

func TestDirectoriesFromDifferentReposCoexist(t *testing.T) {
	repoA, repoB := t.TempDir(), t.TempDir()
	for _, d := range []string{repoA, repoB} {
		require.NoError(t, exec.Command("git", "-C", d, "init", "-q").Run())
	}

	h := newTestHome(t)

	a := startForm(t, h)
	h.setPathInput(repoA)
	pressEnter(h)

	b := startForm(t, h)
	h.setPathInput(repoB)
	pressEnter(h)

	assert.Equal(t, repoA, a.Path)
	assert.Equal(t, repoB, b.Path, "one coordinator, several products")
	assert.Equal(t, 2, h.list.NumInstances())
}

// TestDismissedHelpStillRedraws covers the layout breaking after entering and
// leaving a session: once a help screen has been seen, showHelpScreen skips the
// overlay and runs its callback inline. Attaching resizes the agent's terminal
// to the whole window, so skipping the overlay must still ask for a redraw —
// otherwise the panes stay drawn at the attached size.
func TestDismissedHelpStillRedraws(t *testing.T) {
	h := newTestHome(t)
	h.appState = &fakeAppState{}

	ran := false
	_, _ = h.showHelpScreen(helpTypeInstanceAttach{}, func() { ran = true })
	require.False(t, ran, "first time the overlay is shown; the callback waits for dismissal")
	require.Equal(t, stateHelp, h.state)

	// Mark every help screen as seen, so the next call takes the skip path.
	require.NoError(t, h.appState.SetHelpScreensSeen(^uint32(0)))

	ran = false
	_, cmd := h.showHelpScreen(helpTypeInstanceAttach{}, func() { ran = true })
	assert.True(t, ran, "the callback still runs when the overlay is skipped")
	require.NotNil(t, cmd, "skipping the overlay must still request a redraw")
	assert.NotNil(t, cmd(), "the command produces a resize message")
}

// fakeAppState is an in-memory config.AppState for help-screen bookkeeping.
type fakeAppState struct {
	seen      uint32
	instances []byte
	mosaic    bool
}

func (f *fakeAppState) GetHelpScreensSeen() uint32 { return f.seen }
func (f *fakeAppState) SetHelpScreensSeen(s uint32) error {
	f.seen = s
	return nil
}
func (f *fakeAppState) GetInstances() json.RawMessage { return f.instances }
func (f *fakeAppState) SaveInstances(d json.RawMessage) error {
	f.instances = d
	return nil
}
func (f *fakeAppState) DeleteAllInstances() error { f.instances = nil; return nil }
func (f *fakeAppState) GetViewMosaic() bool       { return f.mosaic }
func (f *fakeAppState) SetViewMosaic(v bool) error {
	f.mosaic = v
	return nil
}
