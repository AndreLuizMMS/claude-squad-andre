package app

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// notifyTitle is what the desktop notification is signed with.
const notifyTitle = "Claude Squad"

// notifyCommand builds the command that raises a desktop notification. It
// returns nil when the machine has no notifier, in which case the bell is the
// only signal — a missing notifier is not an error worth showing.
//
// custom, when set, is a command line whose program receives the title and the
// body as its last two arguments.
func notifyCommand(custom, title, body string) *exec.Cmd {
	if fields := strings.Fields(custom); len(fields) > 0 {
		return exec.Command(fields[0], append(fields[1:], title, body)...)
	}
	if runtime.GOOS == "darwin" {
		script := fmt.Sprintf("display notification %s with title %s",
			strconv.Quote(body), strconv.Quote(title))
		return exec.Command("osascript", "-e", script)
	}
	// notify-send is the freedesktop standard; wsl-notify-send bridges the same
	// call to a Windows toast from inside WSL, where no notification daemon runs.
	for _, name := range []string{"notify-send", "wsl-notify-send.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return exec.Command(path, title, body)
		}
	}
	return nil
}

// notifyBody joins what happened in one round into a single line. One round can
// carry several sessions, and one notification naming all of them beats a
// burst of notifications naming one each.
func notifyBody(notices []string) string {
	return strings.Join(notices, " • ")
}

// notifyPending raises the desktop notification for what happened in the last
// round. It returns nil when there is nothing to announce, the user turned
// notifications off, or the machine has no notifier.
func (m *home) notifyPending() tea.Cmd {
	if m.appConfig.DisableNotify || len(m.notices) == 0 {
		return nil
	}
	cmd := notifyCommand(m.appConfig.NotifyCommand, notifyTitle, notifyBody(m.notices))
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		// The notifier is a separate program that may be slow to start, so it
		// is never waited on inside the draw loop.
		if err := cmd.Start(); err != nil {
			return nil
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
}
