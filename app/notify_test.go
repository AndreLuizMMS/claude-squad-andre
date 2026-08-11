package app

import (
	"claude-squad/session"
	"claude-squad/session/git"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startedInstance returns a running session backed by a real tmux session.
func startedInstance(t *testing.T, title string) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title: title, Path: t.TempDir(), Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	t.Cleanup(func() { _ = inst.Kill() })
	return inst
}

// work feeds n rounds where the agent's screen changed.
func work(h *home, inst *session.Instance, n int) (finished bool) {
	for i := 0; i < n; i++ {
		if h.applyMetadataResults([]instanceMetaResult{{instance: inst, updated: true}}) {
			finished = true
		}
	}
	return finished
}

// idle feeds one round where nothing changed on the agent's screen.
func idle(h *home, inst *session.Instance) bool {
	return h.applyMetadataResults([]instanceMetaResult{{instance: inst}})
}

func TestBellRingsOnceWhenTheAgentAnswers(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "worker")

	assert.False(t, work(h, inst, busyTicksToArm), "starting to work is not an answer")
	assert.Equal(t, session.Running, inst.Status)

	assert.True(t, idle(h, inst), "finishing a stretch of work rings once")
	assert.Equal(t, session.Ready, inst.Status)
	assert.True(t, inst.NeedsAttention, "and the session is flagged for the developer")

	// The reported bug: it kept ringing while the session just sat there.
	for i := 0; i < 20; i++ {
		assert.False(t, idle(h, inst), "an idle session must stay quiet, round %d", i)
	}
}

// TestFlickerDoesNotRing is the root cause of the repeated sound: the status
// detector reports any change on screen, so a blinking cursor or a spinner
// looks like the agent starting and stopping over and over.
func TestFlickerDoesNotRing(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "flicker")

	for i := 0; i < 30; i++ {
		assert.False(t, work(h, inst, 1), "single blip of activity, round %d", i)
		assert.False(t, idle(h, inst), "back to idle right away, round %d", i)
	}
	assert.False(t, inst.NeedsAttention, "flicker never counts as an answer")
}

func TestEachStretchOfWorkRingsAgain(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "twice")

	work(h, inst, busyTicksToArm)
	require.True(t, idle(h, inst))

	// A second real turn rings again — this is a notification, not a one-off.
	work(h, inst, busyTicksToArm)
	assert.True(t, idle(h, inst), "the next answer rings too")
}

func TestGoingBackToWorkClearsTheAlert(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "resumed")

	work(h, inst, busyTicksToArm)
	require.True(t, idle(h, inst))
	require.True(t, inst.NeedsAttention)

	work(h, inst, 1)
	assert.False(t, inst.NeedsAttention, "an agent back at work has nothing pending to read")
}

func TestAnOrphanedSessionNeverRings(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "orphan")

	work(h, inst, busyTicksToArm)
	finished := h.applyMetadataResults([]instanceMetaResult{{instance: inst, dirMissing: true}})
	assert.False(t, finished, "losing the directory is not an answer")
	assert.Equal(t, session.Orphaned, inst.Status)
}

// TestIdleSessionsAreNotDiffedEveryRound guards the throttle: reading the diff
// twice a second per session costs real CPU on a large repository and says
// nothing new while the agent sits still.
func TestDiffIsReadOnScheduleAndOnSelectionChange(t *testing.T) {
	h := newTestHome(t)

	assert.True(t, h.forceDiffRead(), "the first round reads everything")
	for i := 1; i < diffEveryNTicks; i++ {
		h.metaTicks = i
		assert.False(t, h.forceDiffRead(), "idle round %d skips the diff", i)
	}
	h.metaTicks = diffEveryNTicks
	assert.True(t, h.forceDiffRead(), "the periodic refresh still happens")

	// Moving the selection needs the full diff right away — the pane renders it.
	h.metaTicks = 1
	h.diffDirty = true
	assert.True(t, h.forceDiffRead(), "a new selection is read immediately")
	assert.False(t, h.forceDiffRead(), "and only once")
}

// TestSkippedDiffKeepsThePreviousNumbers: a round that did not read the diff
// must not wipe the counters shown in the list.
func TestSkippedDiffKeepsThePreviousNumbers(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "counted")

	stats := &git.DiffStats{Added: 7, Removed: 2}
	h.applyMetadataResults([]instanceMetaResult{{instance: inst, diffRead: true, diffStats: stats}})
	require.Equal(t, stats, inst.GetDiffStats())

	h.applyMetadataResults([]instanceMetaResult{{instance: inst}})
	assert.Equal(t, stats, inst.GetDiffStats(), "a skipped read leaves the numbers alone")
}
