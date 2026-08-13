package tmux

import (
	cmd2 "claude-squad/cmd"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-squad/cmd/cmd_test"

	"github.com/stretchr/testify/require"
)

type MockPtyFactory struct {
	t *testing.T

	// Array of commands and the corresponding file handles representing PTYs.
	cmds  []*exec.Cmd
	files []*os.File
}

func (pt *MockPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	filePath := filepath.Join(pt.t.TempDir(), fmt.Sprintf("pty-%s-%d", pt.t.Name(), rand.Int31()))
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err == nil {
		pt.cmds = append(pt.cmds, cmd)
		pt.files = append(pt.files, f)
	}
	return f, err
}

func (pt *MockPtyFactory) Close() {}

func NewMockPtyFactory(t *testing.T) *MockPtyFactory {
	return &MockPtyFactory{
		t: t,
	}
}

func TestSanitizeName(t *testing.T) {
	session := NewTmuxSession("asdf", "program")
	require.Equal(t, TmuxPrefix+"asdf", session.sanitizedName)

	session = NewTmuxSession("a sd f . . asdf", "program")
	require.Equal(t, TmuxPrefix+"asdf__asdf", session.sanitizedName)
}

func TestStartTmuxSession(t *testing.T) {
	ptyFactory := NewMockPtyFactory(t)

	created := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") && !created {
				created = true
				return fmt.Errorf("session already exists")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	workdir := t.TempDir()
	session := newTmuxSession("test-session", "claude", ptyFactory, cmdExec)

	err := session.Start(workdir)
	require.NoError(t, err)
	require.Equal(t, 2, len(ptyFactory.cmds))
	require.Equal(t, fmt.Sprintf("tmux new-session -d -s claudesquad_test-session -c %s claude", workdir),
		cmd2.ToString(ptyFactory.cmds[0]))
	require.Equal(t, "tmux attach-session -t =claudesquad_test-session",
		cmd2.ToString(ptyFactory.cmds[1]),
		"the target is exact: a prefix match would attach to another agent's session")

	require.Equal(t, 2, len(ptyFactory.files))

	// File should be closed.
	_, err = ptyFactory.files[0].Stat()
	require.Error(t, err)
	// File should be open
	_, err = ptyFactory.files[1].Stat()
	require.NoError(t, err)
}

func TestMarkersMatchTheAgentBehindTheProgramPath(t *testing.T) {
	// The configured program is whatever "which claude" resolved to, so a raw
	// equality check against "claude" would find no markers at all and leave
	// the session judged only by its screen flickering.
	for _, program := range []string{
		"claude",
		"/home/me/.local/bin/claude",
		"/usr/bin/claude --dangerously-skip-permissions",
	} {
		s := newTmuxSession("t", program, nil, nil)
		if !s.HasBusyMarker() {
			t.Errorf("program %q should be recognized as claude", program)
		}
		if s.markers().prompt == "" {
			t.Errorf("program %q should have a prompt marker", program)
		}
	}

	// An unknown agent has no markers, and falls back to watching the pane.
	s := newTmuxSession("t", "/usr/bin/bash", nil, nil)
	if s.HasBusyMarker() {
		t.Error("bash announces nothing and should have no busy marker")
	}
}

// TestSplitCaptures: a batched read is one blob of text, and cutting it back
// into one screen per session is the whole reason the batch is safe to use.
func TestSplitCaptures(t *testing.T) {
	out := captureMarker + "alpha\n" +
		"line one\nline two\n" +
		captureMarker + "beta\n" +
		"only line\n"

	got := splitCaptures(out, []string{"alpha", "beta"})

	require.Equal(t, "line one\nline two\n", got["alpha"])
	require.Equal(t, "only line\n", got["beta"])
}

// TestSplitCapturesIgnoresAMarkerPrintedByAnAgent: the delimiter is a plain
// line of text, and an agent that happens to print one must not be able to
// hand its screen to another session.
func TestSplitCapturesIgnoresAMarkerPrintedByAnAgent(t *testing.T) {
	out := captureMarker + "alpha\n" +
		"real content\n" +
		captureMarker + "not-a-session\n" +
		"still alpha\n"

	got := splitCaptures(out, []string{"alpha"})

	require.Equal(t, 1, len(got))
	require.Equal(t, "real content\n"+captureMarker+"not-a-session\nstill alpha\n", got["alpha"])
}

// TestSplitCapturesTruncatedBatch: tmux stops a sequence at the first command
// that fails, so a session that died takes the rest of the batch with it. The
// sessions that did come back must still be usable, and the ones that did not
// must be absent rather than empty.
func TestSplitCapturesTruncatedBatch(t *testing.T) {
	out := captureMarker + "alpha\nalpha screen\n"

	got := splitCaptures(out, []string{"alpha", "beta", "gamma"})

	require.Equal(t, "alpha screen\n", got["alpha"])
	_, ok := got["beta"]
	require.False(t, ok, "a session the batch never reached must not look empty")
}

// The caret is read from a line of tmux output, so what that line can look like
// decides whether a cell draws a caret in the right place, no caret, or a caret
// somewhere wrong.
func TestParseCursor(t *testing.T) {
	tests := []struct {
		name              string
		out               string
		wantX, wantY      int
		wantVisible, fail bool
	}{
		{name: "cursor showing", out: "10 4 1\n", wantX: 10, wantY: 4, wantVisible: true},
		{name: "cursor hidden by the program", out: "0 0 0\n", wantVisible: false},
		{name: "tmux too old to expand the fields", out: "#{cursor_x}\n", fail: true},
		{name: "nothing came back", out: "", fail: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, visible, err := parseCursor(tt.out)
			if tt.fail {
				if err == nil {
					t.Fatalf("parseCursor(%q) devia falhar", tt.out)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCursor(%q): %v", tt.out, err)
			}
			if x != tt.wantX || y != tt.wantY || visible != tt.wantVisible {
				t.Errorf("parseCursor(%q) = %d,%d,%v; esperado %d,%d,%v",
					tt.out, x, y, visible, tt.wantX, tt.wantY, tt.wantVisible)
			}
		})
	}
}
