package ui

import (
	"claude-squad/session"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/require"
)

func newTestList(titles ...string) *List {
	s := spinner.New()
	l := NewList(&s, false)
	for _, t := range titles {
		inst, _ := session.NewInstance(session.InstanceOptions{
			Title:   t,
			Path:    ".",
			Program: "echo",
		})
		l.AddInstance(inst)
	}
	return l
}

func TestMoveUp(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveUp()
	require.True(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "b", l.items[0].Title)
	require.Equal(t, "a", l.items[1].Title)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveUp_AtTop(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(0)

	moved := l.MoveUp()
	require.False(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
}

func TestMoveDown(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveDown()
	require.True(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, "b", l.items[2].Title)
}

func TestMoveDown_AtBottom(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(2)

	moved := l.MoveDown()
	require.False(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveWithSingleItem(t *testing.T) {
	l := newTestList("only")
	l.SetSelectedInstance(0)

	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
}

// answering marks an instance as having handed the turn back.
func answering(inst *session.Instance) *session.Instance {
	inst.NeedsAttention = true
	inst.SetStatus(session.Ready)
	return inst
}

func TestSelectNextAttention(t *testing.T) {
	l := newTestList("a", "b", "c")
	answering(l.items[2])
	l.SetSelectedInstance(0)

	require.True(t, l.SelectNextAttention())
	require.Equal(t, 2, l.selectedIdx)
}

func TestSelectNextAttentionWrapsAround(t *testing.T) {
	l := newTestList("a", "b", "c")
	answering(l.items[0])
	l.SetSelectedInstance(2)

	require.True(t, l.SelectNextAttention())
	require.Equal(t, 0, l.selectedIdx)
}

func TestSelectNextAttentionSkipsTheOneAlreadySelected(t *testing.T) {
	l := newTestList("a", "b", "c")
	answering(l.items[1])
	answering(l.items[2])
	l.SetSelectedInstance(1)

	// Standing on an answer and pressing again moves on instead of staying put.
	require.True(t, l.SelectNextAttention())
	require.Equal(t, 2, l.selectedIdx)
}

func TestSelectNextAttentionWithNothingToSee(t *testing.T) {
	l := newTestList("a", "b")
	l.SetSelectedInstance(1)

	require.False(t, l.SelectNextAttention())
	require.Equal(t, 1, l.selectedIdx)

	empty := newTestList()
	require.False(t, empty.SelectNextAttention())
}

func TestGroupHeaderOnlyWhenProjectsDiffer(t *testing.T) {
	l := newTestList("a", "b")
	inst := l.items[0]

	// One project in play: nothing to separate.
	l.repos = map[string]int{"only": 1}
	_, ok := l.groupHeader(inst, "")
	require.False(t, ok)

	l.repos = map[string]int{"one": 1, "two": 1}
	// The session has not started, so it has no directory to group by yet.
	_, ok = l.groupHeader(inst, "")
	require.False(t, ok)
}

func TestGroupHeaderBreaksTheListByProject(t *testing.T) {
	l := newTestList()
	dir := t.TempDir()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title: "worker", Path: dir, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	t.Cleanup(func() { _ = inst.Kill() })

	l.AddInstance(inst)
	l.repos = map[string]int{"one": 1, "two": 1}

	name := inst.DirName()
	header, ok := l.groupHeader(inst, "")
	require.True(t, ok, "the first session of a project gets a header")
	require.Equal(t, name, header)

	// The session right below it belongs to the same project: no second header.
	_, ok = l.groupHeader(inst, name)
	require.False(t, ok)
}

func TestBlockedSessionsAreVisitedFirst(t *testing.T) {
	l := newTestList("a", "b", "c")
	answering(l.items[1])
	l.items[2].NeedsApproval = true
	l.SetSelectedInstance(0)

	// "b" is closer, but "c" is the one holding up its agent.
	require.True(t, l.SelectNextAttention())
	require.Equal(t, 2, l.selectedIdx)

	// With nothing blocked left, the plain answer is next.
	l.items[2].NeedsApproval = false
	require.True(t, l.SelectNextAttention())
	require.Equal(t, 1, l.selectedIdx)
}
