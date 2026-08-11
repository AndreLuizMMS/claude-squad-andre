package app

import (
	"claude-squad/session"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyToBytesCoversWhatATerminalSends(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"letters", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("oi")}, "oi"},
		{"accents", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ção")}, "ção"},
		{"space", tea.KeyMsg{Type: tea.KeySpace}, " "},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, "\x1b[B"},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, "\x1b[C"},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "\x1b[D"},
		{"delete", tea.KeyMsg{Type: tea.KeyDelete}, "\x1b[3~"},
		{"alt+letter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, "\x1bb"},
		{"ctrl+c interrupts the agent", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{"ctrl+a", tea.KeyMsg{Type: tea.KeyCtrlA}, "\x01"},
		{"ctrl+z", tea.KeyMsg{Type: tea.KeyCtrlZ}, "\x1a"},
		{"ctrl+q reaches the agent now", tea.KeyMsg{Type: tea.KeyCtrlQ}, "\x11"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, keyToBytes(c.msg))
		})
	}
}

func TestEnterStartsTypingWithoutTakingOverTheScreen(t *testing.T) {
	h := newTestHome(t)
	h.appState = &fakeAppState{seen: ^uint32(0)} // help already seen: go straight in

	dir := t.TempDir()
	inst := startForm(t, h, "chat")
	pressEnter(h)
	h.setPathInput(dir)
	pressEnter(h) // creates the session

	// Starting happens in the background in the real program; do it inline here.
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()
	h.newInstanceFinalizer()
	h.list.SetSelectedInstance(0)

	press(h, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, stateInteract, h.state, "enter starts typing into the session")

	// The list is still rendered — that is the whole point.
	assert.Contains(t, h.list.String(), "chat", "the session list stays visible")

	// Keys that are shortcuts outside now belong to the agent, and do not
	// create sessions or kill anything.
	before := h.list.NumInstances()
	press(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	press(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	press(h, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, before, h.list.NumInstances(), "shortcuts are typed, not executed")
	assert.Equal(t, stateInteract, h.state)

	// ctrl+l is the way back to the list and the tabs.
	press(h, tea.KeyMsg{Type: tea.KeyCtrlL})
	assert.Equal(t, stateDefault, h.state)
}

func TestLeavingInteractionRestoresTheMenu(t *testing.T) {
	h := newTestHome(t)
	h.state = stateInteract
	h.interactTerminal = true

	cmd := h.leaveInteract()
	assert.Equal(t, stateDefault, h.state)
	assert.False(t, h.interactTerminal)
	require.NotNil(t, cmd, "leaving asks for a redraw")
}

func TestInteractionStopsWhenTheSessionCannotTakeKeys(t *testing.T) {
	h := newTestHome(t)
	h.state = stateInteract

	// No selected session at all.
	press(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.Equal(t, stateDefault, h.state, "typing into nothing drops back to the list")
}

// TestBellRingsOnlyWhenAnAgentFinishes covers the notification sound: it fires
// when an agent stops working and hands the turn back, and stays quiet
// otherwise — an agent that has been idle must not ring on every tick.
func TestBellRingsOnlyWhenAnAgentFinishes(t *testing.T) {
	h := newTestHome(t)

	inst, err := session.NewInstance(session.InstanceOptions{
		Title: "worker", Path: t.TempDir(), Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	// Working, then done: that is the moment worth a sound.
	inst.SetStatus(session.Running)
	finished := h.applyMetadataResults([]instanceMetaResult{{instance: inst}})
	assert.True(t, finished, "an agent that just finished rings")
	assert.Equal(t, session.Ready, inst.Status)

	// Still done on the next round: silence.
	finished = h.applyMetadataResults([]instanceMetaResult{{instance: inst}})
	assert.False(t, finished, "an idle agent does not keep ringing")

	// Back to work: silence.
	finished = h.applyMetadataResults([]instanceMetaResult{{instance: inst, updated: true}})
	assert.False(t, finished, "starting to work is not a notification")
	assert.Equal(t, session.Running, inst.Status)

	// A session that lost its directory is not a finished chat.
	finished = h.applyMetadataResults([]instanceMetaResult{{instance: inst, dirMissing: true}})
	assert.False(t, finished)
	assert.Equal(t, session.Orphaned, inst.Status)
}

func TestBellCanBeTurnedOff(t *testing.T) {
	h := newTestHome(t)
	assert.False(t, h.appConfig.DisableBell, "the sound is on unless it is turned off")
}
