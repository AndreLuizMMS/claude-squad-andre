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

// idleRound feeds a single round where nothing changed on the agent's screen.
func idleRound(h *home, inst *session.Instance) bool {
	return h.applyMetadataResults([]instanceMetaResult{{instance: inst}})
}

// idle feeds enough quiet rounds for the turn to count as over.
func idle(h *home, inst *session.Instance) (finished bool) {
	for i := 0; i < idleTicksToFinish; i++ {
		if idleRound(h, inst) {
			finished = true
		}
	}
	return finished
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
		assert.False(t, idleRound(h, inst), "back to idle right away, round %d", i)
	}
	assert.False(t, inst.NeedsAttention, "flicker never counts as an answer")
}

// TestAPauseMidAnswerDoesNotRing is the reported bug: the agent goes quiet for
// a moment while it thinks or waits on a tool, and the old rule called that the
// end of the turn and rang immediately.
func TestAPauseMidAnswerDoesNotRing(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "thinking")

	work(h, inst, busyTicksToArm)
	for i := 0; i < idleTicksToFinish-1; i++ {
		assert.False(t, idleRound(h, inst), "a short pause is not the end, round %d", i)
	}
	assert.False(t, inst.NeedsAttention, "nothing to read while the agent is mid-answer")

	// It picks the answer back up, then really finishes.
	work(h, inst, 1)
	assert.True(t, idle(h, inst), "the real end of the turn rings")
}

// TestTheAgentSayingItIsBusyKeepsTheTurnOpen: the agent's own working marker
// wins over a screen that happens not to change.
func TestTheAgentSayingItIsBusyKeepsTheTurnOpen(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "busy")

	work(h, inst, busyTicksToArm)
	for i := 0; i < 20; i++ {
		finished := h.applyMetadataResults([]instanceMetaResult{{instance: inst, busy: true}})
		assert.False(t, finished, "still working, round %d", i)
	}
	assert.Equal(t, session.Running, inst.Status)
	assert.True(t, idle(h, inst), "and it rings once the marker is gone")
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

// TestDiffIsReadOnScheduleAndOnSelectionChange guards the throttle: reading the
// diff twice a second per session costs real CPU on a large repository and says
// nothing new while the agent sits still.
func TestDiffIsReadOnScheduleAndOnSelectionChange(t *testing.T) {
	assert.True(t, heavyReadDue(0, 0), "the first round reads the first session")
	for tick := 1; tick < diffEveryNTicks; tick++ {
		assert.False(t, heavyReadDue(tick, 0), "idle round %d skips the diff", tick)
	}
	assert.True(t, heavyReadDue(diffEveryNTicks, 0), "the periodic refresh still happens")
}

// TestHeavyReadsAreSpreadAcrossRounds: a screenful of agents must not start all
// of its git processes in the same round. Each session gets the same 2s beat,
// offset from its neighbours.
func TestHeavyReadsAreSpreadAcrossRounds(t *testing.T) {
	const sessions = 12
	perRound := make([]int, diffEveryNTicks)
	for tick := 0; tick < diffEveryNTicks; tick++ {
		for idx := 0; idx < sessions; idx++ {
			if heavyReadDue(tick, idx) {
				perRound[tick]++
			}
		}
	}
	for tick, n := range perRound {
		assert.Equal(t, sessions/diffEveryNTicks, n,
			"round %d should carry its share of the sessions, not all of them", tick)
	}
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

// TestScreenChurnIsIgnoredWhenTheAgentReportsItself is the detection fix: an
// agent that says when it is working (claude, gemini) must not be judged by its
// screen changing, which counts spinners, clocks and blinking cursors as work.
func TestScreenChurnIsIgnoredWhenTheAgentReportsItself(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "chatty")

	for i := 0; i < 30; i++ {
		finished := h.applyMetadataResults([]instanceMetaResult{
			{instance: inst, updated: true, hasBusyMarker: true},
		})
		assert.False(t, finished, "the screen moved but the agent is not working, round %d", i)
	}
	assert.Equal(t, session.Ready, inst.Status, "a redrawing screen is not a busy agent")
	assert.False(t, inst.NeedsAttention, "and never becomes an answer")

	// The agent's own marker still drives a real turn from start to finish.
	for i := 0; i < busyTicksToArm; i++ {
		h.applyMetadataResults([]instanceMetaResult{
			{instance: inst, busy: true, hasBusyMarker: true},
		})
	}
	assert.Equal(t, session.Running, inst.Status)

	var finished bool
	for i := 0; i < idleTicksToFinish; i++ {
		if h.applyMetadataResults([]instanceMetaResult{{instance: inst, hasBusyMarker: true}}) {
			finished = true
		}
	}
	assert.True(t, finished, "the marker going away ends the turn")
	assert.Equal(t, []string{"chatty respondeu"}, h.notices, "and the notification names the session")
}

func TestNotifyCommand(t *testing.T) {
	// A configured notifier receives the title and the body as its last args.
	cmd := notifyCommand("my-notifier --urgent", "Claude Squad", "worker respondeu")
	require.NotNil(t, cmd)
	assert.Equal(t, []string{"my-notifier", "--urgent", "Claude Squad", "worker respondeu"}, cmd.Args)

	// A machine with no notifier is not an error: the bell still rings.
	cmd = notifyCommand("", "t", "b")
	if cmd != nil {
		assert.NotEmpty(t, cmd.Path)
	}
}

func TestNotifyBodyNamesTheSessions(t *testing.T) {
	assert.Equal(t, "", notifyBody(nil))
	assert.Equal(t, "worker respondeu", notifyBody([]string{"worker respondeu"}))
	assert.Equal(t, "a respondeu • b caiu", notifyBody([]string{"a respondeu", "b caiu"}))
}

// TestAQuestionOnScreenIsNotAnAnswer separates the two states that used to look
// alike: an agent that finished its turn versus an agent stopped on a question
// it cannot answer by itself. The second one blocks until someone replies.
func TestAQuestionOnScreenBlocksTheSession(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "asking")

	work(h, inst, busyTicksToArm)
	finished := h.applyMetadataResults([]instanceMetaResult{{instance: inst, hasPrompt: true}})

	assert.False(t, finished, "a question is not the turn being handed back")
	assert.True(t, inst.NeedsApproval, "the session is blocked on the developer")
	assert.Equal(t, session.Ready, inst.Status, "waiting on an answer is not working")
	assert.Equal(t, []string{"asking pediu aprovação"}, h.notices)

	// The question stays on screen until answered, and must announce only once.
	for i := 0; i < 10; i++ {
		h.applyMetadataResults([]instanceMetaResult{{instance: inst, hasPrompt: true}})
		assert.Empty(t, h.notices, "the same question announces once, round %d", i)
	}
	assert.True(t, inst.NeedsApproval)

	// Answered: the agent goes back to work and nothing is blocked anymore.
	work(h, inst, 1)
	assert.False(t, inst.NeedsApproval)
}

func TestAutoYesAnswersInsteadOfBlocking(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "auto")
	inst.AutoYes = true

	h.applyMetadataResults([]instanceMetaResult{{instance: inst, hasPrompt: true}})
	assert.False(t, inst.NeedsApproval, "auto-yes answers it, so nobody is blocked")
	assert.Empty(t, h.notices)
}

// TestAnAgentThatDiedStopsLookingReady is the second lie the list used to tell:
// the agent's process ends, its terminal goes with it, and the session keeps
// the green dot it had at the moment it died.
func TestAnAgentThatDiedStopsLookingReady(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "gone")

	work(h, inst, busyTicksToArm)
	require.True(t, idle(h, inst))
	require.True(t, inst.NeedsAttention)

	h.applyMetadataResults([]instanceMetaResult{{instance: inst, dead: true}})
	assert.Equal(t, session.Exited, inst.Status)
	assert.False(t, inst.NeedsAttention, "there is nothing left to read")
	assert.Equal(t, []string{"gone caiu"}, h.notices)

	// It stays dead quietly instead of announcing itself every round.
	for i := 0; i < 10; i++ {
		h.applyMetadataResults([]instanceMetaResult{{instance: inst, dead: true}})
		assert.Empty(t, h.notices, "round %d", i)
	}
}

func TestADeadSessionIsNoLongerWatched(t *testing.T) {
	h := newTestHome(t)
	inst := startedInstance(t, "watched")
	h.list.AddInstance(inst)

	require.Len(t, h.snapshotActiveInstances(), 1)

	h.applyMetadataResults([]instanceMetaResult{{instance: inst, dead: true}})
	assert.Empty(t, h.snapshotActiveInstances(),
		"reading a terminal that is gone only fills the log with the same failure")
}
