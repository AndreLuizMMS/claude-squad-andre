package tmux

import (
	"bytes"
	"claude-squad/cmd"
	"claude-squad/log"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

const ProgramClaude = "claude"

const ProgramAider = "aider"
const ProgramGemini = "gemini"

// agentMarkers are the strings an agent draws in its own interface. busy is
// what it shows while a turn is in flight; prompt is what it shows while it
// waits on a yes/no answer. The busy marker is matched lowercased, so agents
// that capitalize it differently between versions still match.
var agentMarkers = map[string]struct{ prompt, busy string }{
	ProgramClaude: {"No, and tell Claude what to do differently", "esc to interrupt"},
	ProgramAider:  {"(Y)es/(N)o/(D)on't ask again", ""},
	ProgramGemini: {"Yes, allow once", "esc to cancel"},
}

// markers returns the markers of the agent running in this session. The
// configured program is usually a resolved path ("/home/me/.local/bin/claude")
// and may carry flags, so the lookup is done on the command's base name — a
// path match would silently fall through to no markers at all, which is what
// made a blinking cursor look like an agent starting and stopping work.
func (t *TmuxSession) markers() (m struct{ prompt, busy string }) {
	fields := strings.Fields(t.program)
	if len(fields) == 0 {
		return m
	}
	name := filepath.Base(fields[0])
	for program, markers := range agentMarkers {
		if strings.HasPrefix(name, program) {
			return markers
		}
	}
	return m
}

// HasBusyMarker reports whether this agent announces its own working state. For
// the ones that do, that announcement is the only signal worth trusting; for
// the rest, changes in the pane are all there is to go on.
func (t *TmuxSession) HasBusyMarker() bool {
	return t.markers().busy != ""
}

// TmuxSession represents a managed tmux session
type TmuxSession struct {
	// Initialized by NewTmuxSession
	//
	// The name of the tmux session and the sanitized name used for tmux commands.
	sanitizedName string
	program       string
	// ptyFactory is used to create a PTY for the tmux session.
	ptyFactory PtyFactory
	// cmdExec is used to execute commands in the tmux session.
	cmdExec cmd.Executor

	// Initialized by Start or Restore
	//
	// ptmx is a PTY is running the tmux attach command. This can be resized to change the
	// stdout dimensions of the tmux pane. On detach, we close it and set a new one.
	// This should never be nil.
	ptmx *os.File
	// monitor monitors the tmux pane content and sends signals to the UI when it's status changes
	monitor *statusMonitor

	// Initialized by Attach
	// Deinitilaized by Detach
	//
	// Channel to be closed at the very end of detaching. Used to signal callers.
	attachCh chan struct{}
	// While attached, we use some goroutines to manage the window size and stdin/stdout. This stuff
	// is used to terminate them on Detach. We don't want them to outlive the attached window.
	ctx    context.Context
	cancel func()
	wg     *sync.WaitGroup
}

const TmuxPrefix = "claudesquad_"

var whiteSpaceRegex = regexp.MustCompile(`\s+`)

func toClaudeSquadTmuxName(str string) string {
	str = whiteSpaceRegex.ReplaceAllString(str, "")
	str = strings.ReplaceAll(str, ".", "_") // tmux replaces all . with _
	return fmt.Sprintf("%s%s", TmuxPrefix, str)
}

// NewTmuxSession creates a new TmuxSession with the given name and program.
func NewTmuxSession(name string, program string) *TmuxSession {
	return newTmuxSession(name, program, MakePtyFactory(), cmd.MakeExecutor())
}

// NewTmuxSessionWithDeps creates a new TmuxSession with provided dependencies for testing.
func NewTmuxSessionWithDeps(name string, program string, ptyFactory PtyFactory, cmdExec cmd.Executor) *TmuxSession {
	return newTmuxSession(name, program, ptyFactory, cmdExec)
}

// exactTarget is how a tmux session must be addressed. Plain "-t name" falls
// back to a prefix match when no session carries that name exactly, so killing
// a session called "a-16f7e7" that had already died would instead kill
// "a-6e6164" — a different agent, still working. Sessions are named after the
// title the developer typed, and repeated titles are normal, so the prefixes
// collide constantly. The leading "=" demands the exact name.
func exactTarget(name string) string { return "=" + name }

// exactPaneTarget is the same demand for the commands that address a pane or a
// window rather than a session — capture-pane, set-option. Those parse the
// name as the session half of "session:window.pane", so it needs the colon:
// without it tmux rejects the "=" outright.
func exactPaneTarget(name string) string { return "=" + name + ":" }

// target is this session's exact tmux address.
func (t *TmuxSession) target() string { return exactTarget(t.sanitizedName) }

// paneTarget is this session's exact address for pane and window commands.
func (t *TmuxSession) paneTarget() string { return exactPaneTarget(t.sanitizedName) }

func newTmuxSession(name string, program string, ptyFactory PtyFactory, cmdExec cmd.Executor) *TmuxSession {
	return &TmuxSession{
		sanitizedName: toClaudeSquadTmuxName(name),
		program:       program,
		ptyFactory:    ptyFactory,
		cmdExec:       cmdExec,
	}
}

// Start creates and starts a new tmux session, then attaches to it. Program is the command to run in
// the session (ex. claude). workdir is the git worktree directory.
func (t *TmuxSession) Start(workDir string) error {
	// Check if the session already exists
	if t.DoesSessionExist() {
		return fmt.Errorf("tmux session already exists: %s", t.sanitizedName)
	}

	// Create a new detached tmux session and start claude in it
	cmd := exec.Command("tmux", "new-session", "-d", "-s", t.sanitizedName, "-c", workDir, t.program)

	ptmx, err := t.ptyFactory.Start(cmd)
	if err != nil {
		// Cleanup any partially created session if any exists.
		if t.DoesSessionExist() {
			cleanupCmd := exec.Command("tmux", "kill-session", "-t", t.target())
			if cleanupErr := t.cmdExec.Run(cleanupCmd); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
		}
		return fmt.Errorf("error starting tmux session: %w", err)
	}

	// Poll for session existence with exponential backoff
	timeout := time.After(2 * time.Second)
	sleepDuration := 5 * time.Millisecond
	for !t.DoesSessionExist() {
		select {
		case <-timeout:
			if cleanupErr := t.Close(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
			return fmt.Errorf("timed out waiting for tmux session %s: %v", t.sanitizedName, err)
		default:
			time.Sleep(sleepDuration)
			// Exponential backoff up to 50ms max
			if sleepDuration < 50*time.Millisecond {
				sleepDuration *= 2
			}
		}
	}
	ptmx.Close()

	// Set history limit to enable scrollback (default is 2000, we'll use 10000 for more history)
	historyCmd := exec.Command("tmux", "set-option", "-t", t.paneTarget(), "history-limit", "10000")
	if err := t.cmdExec.Run(historyCmd); err != nil {
		log.InfoLog.Printf("Warning: failed to set history-limit for session %s: %v", t.sanitizedName, err)
	}

	t.enableMouse()

	err = t.Restore()
	if err != nil {
		if cleanupErr := t.Close(); cleanupErr != nil {
			err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
		}
		return fmt.Errorf("error restoring tmux session: %w", err)
	}

	return nil
}

// CheckAndHandleTrustPrompt checks the pane content once for a trust prompt and dismisses it if found.
// Returns true if the prompt was found and handled.
func (t *TmuxSession) CheckAndHandleTrustPrompt() bool {
	content, err := t.CapturePaneContent()
	if err != nil {
		return false
	}

	if strings.HasSuffix(t.program, ProgramClaude) {
		if strings.Contains(content, "Do you trust the files in this folder?") ||
			strings.Contains(content, "new MCP server") {
			if err := t.TapEnter(); err != nil {
				log.ErrorLog.Printf("could not tap enter on trust/MCP screen: %v", err)
			}
			return true
		}
	} else {
		if strings.Contains(content, "Open documentation url for more info") {
			if err := t.TapDAndEnter(); err != nil {
				log.ErrorLog.Printf("could not tap enter on trust screen: %v", err)
			}
			return true
		}
	}
	return false
}

// Restore attaches to an existing session and restores the window size
// enableMouse turns tmux mouse mode on for this session, which lets the
// mouse wheel scroll the pane's history. Selecting text to copy still works:
// hold Shift while dragging, which every mainstream terminal (Windows
// Terminal, iTerm2, GNOME Terminal, etc.) treats as a request to bypass the
// app's mouse capture and do native selection instead. Reapplied on every
// restore in case a session was created before this existed.
func (t *TmuxSession) enableMouse() {
	cmd := exec.Command("tmux", "set-option", "-t", t.paneTarget(), "mouse", "on")
	if err := t.cmdExec.Run(cmd); err != nil {
		log.InfoLog.Printf("Warning: failed to enable tmux mouse mode for session %s: %v", t.sanitizedName, err)
	}
	t.setScrollSpeed()
}

// setScrollSpeed rebinds the wheel-scroll amount in copy-mode to
// wheelScrollLines per tick. tmux defaults to 5, which reads as sluggish
// when paging through a long session history.
func (t *TmuxSession) setScrollSpeed() {
	const wheelScrollLines = "10"
	for _, table := range []string{"copy-mode", "copy-mode-vi"} {
		for _, dir := range []string{"up", "down"} {
			key := "Wheel" + strings.ToUpper(dir[:1]) + dir[1:] + "Pane"
			cmd := exec.Command("tmux", "bind-key", "-T", table, key,
				"send-keys", "-X", "-N", wheelScrollLines, "scroll-"+dir)
			if err := t.cmdExec.Run(cmd); err != nil {
				log.InfoLog.Printf("Warning: failed to set tmux scroll speed for session %s: %v", t.sanitizedName, err)
			}
		}
	}
}

func (t *TmuxSession) Restore() error {
	t.enableMouse()
	ptmx, err := t.ptyFactory.Start(exec.Command("tmux", "attach-session", "-t", t.target()))
	if err != nil {
		return fmt.Errorf("error opening PTY: %w", err)
	}
	t.ptmx = ptmx
	t.monitor = newStatusMonitor()
	return nil
}

type statusMonitor struct {
	// Store hashes to save memory.
	prevOutputHash []byte
}

func newStatusMonitor() *statusMonitor {
	return &statusMonitor{}
}

// hash hashes the string.
func (m *statusMonitor) hash(s string) []byte {
	h := sha256.New()
	// TODO: this allocation sucks since the string is probably large. Ideally, we hash the string directly.
	h.Write([]byte(s))
	return h.Sum(nil)
}

// TapEnter sends an enter keystroke to the tmux pane.
func (t *TmuxSession) TapEnter() error {
	_, err := t.ptmx.Write([]byte{0x0D})
	if err != nil {
		return fmt.Errorf("error sending enter keystroke to PTY: %w", err)
	}
	return nil
}

// TapDAndEnter sends 'D' followed by an enter keystroke to the tmux pane.
func (t *TmuxSession) TapDAndEnter() error {
	_, err := t.ptmx.Write([]byte{0x44, 0x0D})
	if err != nil {
		return fmt.Errorf("error sending enter keystroke to PTY: %w", err)
	}
	return nil
}

func (t *TmuxSession) SendKeys(keys string) error {
	_, err := t.ptmx.Write([]byte(keys))
	return err
}

// HasUpdated checks if the tmux pane content has changed since the last tick. It also returns true if
// the tmux pane has a prompt for aider or claude code, and whether the agent
// still says it is working.
//
// A capture failure is reported rather than swallowed: it is the same signal a
// dead terminal gives, and the caller is the one that can tell the two apart
// without paying for an extra tmux call on every healthy round.
func (t *TmuxSession) HasUpdated() (updated bool, hasPrompt bool, busy bool, err error) {
	content, err := t.CapturePaneContent()
	if err != nil {
		log.ErrorLog.Printf("error capturing pane content in status monitor: %v", err)
		return false, false, false, err
	}
	updated, hasPrompt, busy = t.Observe(content)
	return updated, hasPrompt, busy, nil
}

// Observe draws the same conclusions HasUpdated does, from a screen that was
// already read. Batched reads capture many sessions with one tmux process, so
// the capture and the reading of it are separate steps.
func (t *TmuxSession) Observe(content string) (updated bool, hasPrompt bool, busy bool) {
	m := t.markers()
	if m.prompt != "" {
		hasPrompt = strings.Contains(content, m.prompt)
	}
	if m.busy != "" {
		busy = strings.Contains(strings.ToLower(content), m.busy)
	}

	// Hashed once: the pane is a screenful of text and this runs for every
	// session twice a second.
	sum := t.monitor.hash(content)
	if !bytes.Equal(sum, t.monitor.prevOutputHash) {
		t.monitor.prevOutputHash = sum
		return true, hasPrompt, busy
	}
	return false, hasPrompt, busy
}

// Name is the tmux session name behind this session, which is what a batched
// read has to address.
func (t *TmuxSession) Name() string { return t.sanitizedName }

// captureMarker prefixes the line a batched read emits before each session's
// screen. It is deliberately improbable: a pane that printed this line by
// coincidence would be mistaken for a delimiter.
const captureMarker = "@@claude-squad-capture@@"

// CaptureMany reads the screens of several tmux sessions using a single tmux
// process, and returns them keyed by session name.
//
// One process per session is what made a screenful of agents slow: spawning
// tmux costs about as much as everything else in a round put together, and a
// round reads every session twice a second. tmux runs a sequence of commands
// in one invocation, so twenty reads cost one spawn instead of twenty.
//
// A sequence stops at the first command that fails, so a session that died
// between being listed and being read truncates the rest of the batch. The
// names that came back short are simply absent from the map, and the caller
// falls back to reading those one by one.
func CaptureMany(cmdExec cmd.Executor, names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}

	args := make([]string, 0, len(names)*9)
	for i, name := range names {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args,
			"display-message", "-p", captureMarker+name, ";",
			"capture-pane", "-p", "-e", "-J", "-t", exactPaneTarget(name))
	}

	// Output, not CombinedOutput: a truncated batch writes its error to stderr
	// and the screens read so far are still on stdout, and still good.
	out, _ := cmdExec.Output(exec.Command("tmux", args...))
	if len(out) == 0 {
		return nil
	}
	return splitCaptures(string(out), names)
}

// splitCaptures cuts a batched read back into one screen per session, using
// the marker lines the batch interleaved between them.
func splitCaptures(out string, names []string) map[string]string {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}

	result := make(map[string]string, len(names))
	current := ""
	var body []string
	flush := func() {
		if current != "" {
			// Whether a screen is followed by the next marker or by the end of
			// the batch decides whether its last newline survives the split.
			// Normalizing to the single trailing newline a lone capture-pane
			// produces keeps a session's screen — and therefore the hash that
			// says whether it changed — identical either way.
			joined := strings.TrimSuffix(strings.Join(body, "\n"), "\n")
			if joined != "" {
				joined += "\n"
			}
			result[current] = joined
		}
		body = body[:0]
	}
	for _, line := range strings.Split(out, "\n") {
		if name, ok := strings.CutPrefix(line, captureMarker); ok {
			if _, isWanted := wanted[name]; isWanted {
				flush()
				current = name
				continue
			}
		}
		if current != "" {
			body = append(body, line)
		}
	}
	flush()
	return result
}

// detachKeyByte is ctrl+l: the key that leaves an attached session and returns
// to the list of sessions.
const detachKeyByte = 12

func (t *TmuxSession) Attach() (chan struct{}, error) {
	t.attachCh = make(chan struct{})

	t.wg = &sync.WaitGroup{}
	t.wg.Add(1)
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// The first goroutine should terminate when the ptmx is closed. We use the
	// waitgroup to wait for it to finish.
	// The 2nd one returns when you press escape to Detach. It doesn't need to be
	// in the waitgroup because is the goroutine doing the Detaching; it waits for
	// all the other ones.
	go func() {
		defer t.wg.Done()
		_, _ = io.Copy(os.Stdout, t.ptmx)
		// When io.Copy returns, it means the connection was closed
		// This could be due to normal detach or Ctrl-D
		// Check if the context is done to determine if it was a normal detach
		select {
		case <-t.ctx.Done():
			// Normal detach, do nothing
		default:
			// If context is not done, it was likely an abnormal termination (Ctrl-D)
			// Print warning message
			fmt.Fprintf(os.Stderr, "\n\033[31mError: Session terminated without detaching. Use Ctrl-L to properly detach from tmux sessions.\033[0m\n")
		}
	}()

	go func() {
		// Close the channel after 50ms
		timeoutCh := make(chan struct{})
		go func() {
			time.Sleep(50 * time.Millisecond)
			close(timeoutCh)
		}()

		// Read input from stdin and check for Ctrl+q
		buf := make([]byte, 32)
		for {
			nr, err := os.Stdin.Read(buf)
			if err != nil {
				if err == io.EOF {
					break
				}
				continue
			}

			// Nuke the first bytes of stdin, up to 64, to prevent tmux from reading it.
			// When we attach, there tends to be terminal control sequences like ?[?62c0;95;0c or
			// ]10;rgb:f8f8f8. The control sequences depend on the terminal (warp vs iterm). We should use regex ideally
			// but this works well for now. Log this for debugging.
			//
			// There seems to always be control characters, but I think it's possible for there not to be. The heuristic
			// here can be: if there's characters within 50ms, then assume they are control characters and nuke them.
			select {
			case <-timeoutCh:
			default:
				log.InfoLog.Printf("nuked first stdin: %s", buf[:nr])
				continue
			}

			// Check for Ctrl+l (ASCII 12), the key that gives the screen back
			// to the coordinator.
			if nr == 1 && buf[0] == detachKeyByte {
				// Detach from the session
				t.Detach()
				return
			}

			// Forward other input to tmux
			_, _ = t.ptmx.Write(buf[:nr])
		}
	}()

	t.monitorWindowSize()
	return t.attachCh, nil
}

// DetachSafely disconnects from the current tmux session without panicking
func (t *TmuxSession) DetachSafely() error {
	// Only detach if we're actually attached
	if t.attachCh == nil {
		return nil // Already detached
	}

	var errs []error

	// Close the attached pty session.
	if t.ptmx != nil {
		if err := t.ptmx.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error closing attach pty session: %w", err))
		}
		t.ptmx = nil
	}

	// Clean up attach state
	if t.attachCh != nil {
		close(t.attachCh)
		t.attachCh = nil
	}

	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}

	if t.wg != nil {
		t.wg.Wait()
		t.wg = nil
	}

	t.ctx = nil

	if len(errs) > 0 {
		return fmt.Errorf("errors during detach: %v", errs)
	}
	return nil
}

// Detach disconnects from the current tmux session. It panics if detaching fails. At the moment, there's no
// way to recover from a failed detach.
func (t *TmuxSession) Detach() {
	// TODO: control flow is a bit messy here. If there's an error,
	// I'm not sure if we get into a bad state. Needs testing.
	defer func() {
		close(t.attachCh)
		t.attachCh = nil
		t.cancel = nil
		t.ctx = nil
		t.wg = nil
	}()

	// Close the attached pty session.
	err := t.ptmx.Close()
	if err != nil {
		// This is a fatal error. We can't detach if we can't close the PTY. It's better to just panic and have the
		// user re-invoke the program than to ruin their terminal pane.
		msg := fmt.Sprintf("error closing attach pty session: %v", err)
		log.ErrorLog.Println(msg)
		panic(msg)
	}
	// Attach goroutines should die on EOF due to the ptmx closing. Call
	// t.Restore to set a new t.ptmx.
	if err = t.Restore(); err != nil {
		// This is a fatal error. Our invariant that a started TmuxSession always has a valid ptmx is violated.
		msg := fmt.Sprintf("error closing attach pty session: %v", err)
		log.ErrorLog.Println(msg)
		panic(msg)
	}

	// Cancel goroutines created by Attach.
	t.cancel()
	t.wg.Wait()
}

// Close terminates the tmux session and cleans up resources
func (t *TmuxSession) Close() error {
	var errs []error

	if t.ptmx != nil {
		if err := t.ptmx.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error closing PTY: %w", err))
		}
		t.ptmx = nil
	}

	cmd := exec.Command("tmux", "kill-session", "-t", t.target())
	if err := t.cmdExec.Run(cmd); err != nil {
		errs = append(errs, fmt.Errorf("error killing tmux session: %w", err))
	}

	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple errors occurred during cleanup:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return errors.New(errMsg)
}

// SetDetachedSize set the width and height of the session while detached. This makes the
// tmux output conform to the specified shape.
func (t *TmuxSession) SetDetachedSize(width, height int) error {
	return t.updateWindowSize(width, height)
}

// updateWindowSize updates the window size of the PTY.
func (t *TmuxSession) updateWindowSize(cols, rows int) error {
	return pty.Setsize(t.ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
		X:    0,
		Y:    0,
	})
}

func (t *TmuxSession) DoesSessionExist() bool {
	// Using "-t name" does a prefix match, which is wrong. `-t=` does an exact match.
	existsCmd := exec.Command("tmux", "has-session", "-t", t.target())
	return t.cmdExec.Run(existsCmd) == nil
}

// CapturePaneContent captures the content of the tmux pane
func (t *TmuxSession) CapturePaneContent() (string, error) {
	// Add -e flag to preserve escape sequences (ANSI color codes)
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-t", t.paneTarget())
	output, err := t.cmdExec.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("error capturing pane content: %v", err)
	}
	return string(output), nil
}

// CapturePaneContentWithOptions captures the pane content with additional options
// start and end specify the starting and ending line numbers (use "-" for the start/end of history)
func (t *TmuxSession) CapturePaneContentWithOptions(start, end string) (string, error) {
	// Add -e flag to preserve escape sequences (ANSI color codes)
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", start, "-E", end, "-t", t.paneTarget())
	output, err := t.cmdExec.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to capture tmux pane content with options: %v", err)
	}
	return string(output), nil
}

// CursorPosition is where the caret sits inside the pane, in columns and rows
// counted from the top left of what is on screen, and whether the program
// running there is showing it at all.
//
// It has to be asked for separately because capture-pane returns the characters
// of the screen and nothing about the caret: a captured screen looks exactly the
// same whether the agent is waiting for input or has hidden the cursor mid-run.
func (t *TmuxSession) CursorPosition() (x, y int, visible bool, err error) {
	cmd := exec.Command("tmux", "display-message", "-p", "-t", t.paneTarget(),
		"#{cursor_x} #{cursor_y} #{cursor_flag}")
	out, err := t.cmdExec.Output(cmd)
	if err != nil {
		return 0, 0, false, fmt.Errorf("error reading cursor position: %v", err)
	}
	return parseCursor(string(out))
}

// HistorySize is how many lines tmux is still holding above the visible screen.
// It is the ceiling on scrolling back: asking for a window that starts before
// the oldest line tmux kept does not fail, it comes back short, which would draw
// a cell half full of nothing.
func (t *TmuxSession) HistorySize() (int, error) {
	cmd := exec.Command("tmux", "display-message", "-p", "-t", t.paneTarget(), "#{history_size}")
	out, err := t.cmdExec.Output(cmd)
	if err != nil {
		return 0, fmt.Errorf("error reading history size: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("unexpected history size: %q", strings.TrimSpace(string(out)))
	}
	return n, nil
}

// parseCursor reads the three fields tmux prints back. A tmux too old to know
// cursor_flag prints the name back unexpanded, which is read as "no cursor"
// rather than as an error: the screen is still worth drawing without a caret.
func parseCursor(out string) (x, y int, visible bool, err error) {
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return 0, 0, false, fmt.Errorf("unexpected cursor format: %q", strings.TrimSpace(out))
	}
	if x, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, false, fmt.Errorf("unexpected cursor column: %q", fields[0])
	}
	if y, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, false, fmt.Errorf("unexpected cursor row: %q", fields[1])
	}
	return x, y, fields[2] == "1", nil
}

// CleanupSessions kills all tmux sessions that start with "session-"
func CleanupSessions(cmdExec cmd.Executor) error {
	// First try to list sessions
	cmd := exec.Command("tmux", "ls")
	output, err := cmdExec.Output(cmd)

	// If there's an error and it's because no server is running, that's fine
	// Exit code 1 typically means no sessions exist
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil // No sessions to clean up
		}
		return fmt.Errorf("failed to list tmux sessions: %v", err)
	}

	re := regexp.MustCompile(fmt.Sprintf(`%s.*:`, TmuxPrefix))
	matches := re.FindAllString(string(output), -1)
	for i, match := range matches {
		matches[i] = match[:strings.Index(match, ":")]
	}

	for _, match := range matches {
		log.InfoLog.Printf("cleaning up session: %s", match)
		if err := cmdExec.Run(exec.Command("tmux", "kill-session", "-t", exactTarget(match))); err != nil {
			return fmt.Errorf("failed to kill tmux session %s: %v", match, err)
		}
	}
	return nil
}
