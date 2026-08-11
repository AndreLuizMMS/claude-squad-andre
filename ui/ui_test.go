package ui

import (
	"claude-squad/session"
	"claude-squad/session/git"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMenuFor(t *testing.T, status session.Status) *Menu {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   "s",
		Path:    t.TempDir(),
		Program: "bash",
	})
	require.NoError(t, err)
	inst.SetStatus(status)

	m := NewMenu()
	m.SetState(StateDefault)
	m.SetInstance(inst)
	return m
}

func TestMenuOffersNoGitActions(t *testing.T) {
	m := newMenuFor(t, session.Ready)
	rendered := m.String()

	assert.NotContains(t, rendered, "push", "versioning is the developer's business")
	assert.NotContains(t, rendered, "checkout")
	assert.NotContains(t, rendered, "branch")
	assert.Contains(t, rendered, "pausar", "closing the terminal is still offered")
}

func TestMenuForOrphanedSessionOnlyOffersKill(t *testing.T) {
	m := newMenuFor(t, session.Orphaned)
	rendered := m.String()

	assert.Contains(t, rendered, "encerrar")
	assert.NotContains(t, rendered, "pausar")
	assert.NotContains(t, rendered, "retomar")
}

func TestDiffPaneSaysThereIsNoComparisonBase(t *testing.T) {
	inst, err := session.NewInstance(session.InstanceOptions{
		Title: "plain", Path: t.TempDir(), Program: "bash",
	})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	inst.SetDiffStats(&git.DiffStats{Error: git.ErrNoDiffBase})

	d := NewDiffPane()
	d.SetSize(80, 10)
	d.SetDiff(inst)

	rendered := d.String()
	assert.Contains(t, rendered, "No comparison base")
	assert.NotContains(t, rendered, "Error:", "a plain directory is not an error state")
}

func TestDiffPaneDeclaresChangesAreNotOnlyTheAgents(t *testing.T) {
	inst, err := session.NewInstance(session.InstanceOptions{
		Title: "direct", Path: t.TempDir(), Program: "bash",
	})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	inst.SetDiffStats(&git.DiffStats{Added: 1, Removed: 0, Content: "+hello"})

	d := NewDiffPane()
	d.SetSize(90, 10)
	d.SetDiff(inst)

	assert.Contains(t, d.String(), "from any source")
}

func TestListShowsModeDirectoryAndOrphanState(t *testing.T) {
	sp := spinner.New()
	l := NewList(&sp, false)
	l.SetSize(80, 20)

	dir := t.TempDir()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title: "shown", Path: dir, Program: "bash",
	})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()
	l.AddInstance(inst)()

	assert.Contains(t, l.String(), dir, "sessions show their working directory")

	inst.SetStatus(session.Orphaned)
	rendered := l.String()
	assert.Contains(t, rendered, "ausente:", "the lost path explains the orphan state")
	assert.Contains(t, rendered, orphanedIcon)
}

func TestTruncateLeftKeepsTheEndOfThePath(t *testing.T) {
	assert.Equal(t, "~/a/b", truncateLeft("~/a/b", 10), "fits, untouched")
	assert.Equal(t, "...g/here", truncateLeft("~/very/long/path/going/here", 9),
		"the segment being typed stays visible")
	assert.Equal(t, "", truncateLeft("~/x", 0))
	assert.Equal(t, "..", truncateLeft("~/x/y", 2))
}

func TestListShowsTheEndOfALongPathWhileTyping(t *testing.T) {
	sp := spinner.New()
	l := NewList(&sp, false)
	l.SetSize(40, 20)

	inst, err := session.NewInstance(session.InstanceOptions{
		Title: "typing", Path: t.TempDir(), Program: "bash",
	})
	require.NoError(t, err)
	l.AddInstance(inst)
	l.SetNewInstanceHint("dir: ~/some/very/long/path/being/typed/_ [tab 3]")

	rendered := l.String()
	assert.Contains(t, rendered, "typed/", "the tail being completed stays on screen")
}

func TestListShoutsWhenASessionAnswered(t *testing.T) {
	sp := spinner.New()
	l := NewList(&sp, false)
	l.SetSize(60, 20)

	inst, err := session.NewInstance(session.InstanceOptions{
		Title: "answered", Path: t.TempDir(), Program: "bash",
	})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()
	l.AddInstance(inst)()

	inst.SetStatus(session.Ready)
	assert.NotContains(t, l.String(), "RESPONDEU", "a plain ready session is quiet")

	inst.NeedsAttention = true
	rendered := l.String()
	assert.Contains(t, rendered, "RESPONDEU", "an unread answer is impossible to miss")

	// The badge must not push the line past the width of the pane.
	for _, line := range strings.Split(rendered, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 62, "line overflows the list pane: %q", line)
	}
}
