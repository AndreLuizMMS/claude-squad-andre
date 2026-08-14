package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

var terminalPaneStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})

var terminalFooterStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#808080", Dark: "#808080"})

// terminalSession holds a cached tmux session for a specific instance.
type terminalSession struct {
	tmuxSession  *tmux.TmuxSession
	worktreePath string
	// width/height are the shape the tmux session was last set to. The list view
	// and the mosaic want different shapes for the same shell, so the size is
	// tracked per session instead of assumed from the pane.
	width, height int
	// verifiedAt is when tmux last confirmed this shell is still there.
	verifiedAt time.Time
}

// verifyEvery is how long a cached shell is trusted without asking tmux again.
// The question costs a process, the pane is read ten times a second, and a
// shell that dies is caught anyway: its next read fails and the entry is
// dropped.
const verifyEvery = 5 * time.Second

// TerminalPane manages tmux sessions in the worktree directory of selected instances.
// Sessions are cached per instance so switching between instances preserves terminal state.
// The pane runs whatever program it was built with: a login shell, or a CLI agent.
type TerminalPane struct {
	mu            sync.Mutex
	width, height int
	sessions      map[string]*terminalSession // instanceTitle → session
	currentTitle  string                      // currently displayed instance
	content       string
	fallback      bool
	fallbackText  string

	prefix  string // tmux session name prefix, keeps panes from colliding
	program string // command run inside the session; empty means $SHELL

	isScrolling bool
	viewport    viewport.Model
}

func newTerminalPane(prefix, program string) *TerminalPane {
	return &TerminalPane{
		sessions: make(map[string]*terminalSession),
		viewport: viewport.New(0, 0),
		prefix:   prefix,
		program:  program,
	}
}

// NewTerminalPane creates a pane running the user's shell.
func NewTerminalPane() *TerminalPane {
	return newTerminalPane("term_", "")
}

// NewAgentPane creates a pane running the Cursor CLI (`agent`), with
// permission prompts skipped so the pane opens ready to work.
func NewAgentPane() *TerminalPane {
	return newTerminalPane("agent_", "agent --force")
}

func (t *TerminalPane) SetSize(width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.width = width
	t.height = height
	t.viewport.Width = width
	t.viewport.Height = height
	if s, ok := t.sessions[t.currentTitle]; ok && s.tmuxSession != nil {
		t.resizeLocked(s, width, height)
	}
}

// resizeLocked reshapes a cached tmux session, skipping the call when it is
// already the right shape. Caller must hold t.mu.
func (t *TerminalPane) resizeLocked(s *terminalSession, width, height int) {
	if s == nil || s.tmuxSession == nil || width <= 0 || height <= 0 {
		return
	}
	if s.width == width && s.height == height {
		return
	}
	if err := s.tmuxSession.SetDetachedSize(width, height); err != nil {
		log.InfoLog.Printf("terminal pane: failed to set detached size: %v", err)
		return
	}
	s.width, s.height = width, height
}

// CaptureForInstance reads one instance's pane at an arbitrary size, without
// changing which instance the pane is showing in the list view. The mosaic needs
// every visible session captured, not only the selected one.
// offset is how many lines above the live screen the read starts, so a mosaic
// cell reading back through the shell's history gets the same treatment as one
// reading back through the agent's.
func (t *TerminalPane) CaptureForInstance(instance *session.Instance, width, height, offset int) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if instance == nil || !instance.Started() || instance.Status == session.Paused {
		return "", nil
	}
	if t.program != "" {
		if _, err := exec.LookPath(t.program); err != nil {
			return fmt.Sprintf("Comando '%s' não encontrado no PATH.", t.program), nil
		}
	}

	// ensureSessionLocked points the pane at whatever it just started; the list
	// view is still showing its own instance and must not be moved.
	prevTitle, prevFallback, prevText := t.currentTitle, t.fallback, t.fallbackText
	defer func() { t.currentTitle, t.fallback, t.fallbackText = prevTitle, prevFallback, prevText }()

	if err := t.ensureSessionLocked(instance); err != nil {
		return "", err
	}
	// Same as UpdateContent: ensureSessionLocked already checked, and the mosaic
	// runs this for every cell on screen.
	s, ok := t.sessions[instance.ID()]
	if !ok || s.tmuxSession == nil {
		return "", nil
	}
	t.resizeLocked(s, width, height)
	content, err := t.captureWindow(s, height, offset)
	if err != nil {
		// A read that fails is how a dead shell announces itself now that the
		// existence check runs on a timer. Drop it so the next call rebuilds.
		delete(t.sessions, instance.ID())
		return "", err
	}
	return content, nil
}

// captureWindow reads the live screen, or a screenful from that far back in the
// history. See Instance.PreviewWindow for how tmux numbers those lines.
func (t *TerminalPane) captureWindow(s *terminalSession, height, offset int) (string, error) {
	if offset <= 0 || height <= 0 {
		return s.tmuxSession.CapturePaneContent()
	}
	return s.tmuxSession.CapturePaneContentWithOptions(
		strconv.Itoa(-offset), strconv.Itoa(height-1-offset))
}

// HistoryForInstance is how far back one instance's shell can be read.
func (t *TerminalPane) HistoryForInstance(instance *session.Instance) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if instance == nil {
		return 0, nil
	}
	s, ok := t.sessions[instance.ID()]
	if !ok || s.tmuxSession == nil {
		return 0, nil
	}
	return s.tmuxSession.HistorySize()
}

// CursorForInstance is where the caret sits in one instance's shell, for the
// mosaic cell showing it. Same reason as Instance.CursorPosition: the captured
// characters say nothing about the caret.
func (t *TerminalPane) CursorForInstance(instance *session.Instance) (x, y int, visible bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if instance == nil {
		return 0, 0, false, nil
	}
	s, ok := t.sessions[instance.ID()]
	if !ok || s.tmuxSession == nil {
		return 0, 0, false, nil
	}
	return s.tmuxSession.CursorPosition()
}

// SendKeysToInstance types into one instance's pane. In the mosaic the cell that
// holds the keyboard is not necessarily the one the list view is showing.
func (t *TerminalPane) SendKeysToInstance(instance *session.Instance, keys string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if instance == nil {
		return fmt.Errorf("nenhuma sessão selecionada")
	}
	s, ok := t.sessions[instance.ID()]
	if !ok || s.tmuxSession == nil {
		return fmt.Errorf("o painel da sessão '%s' ainda não subiu", instance.Title)
	}
	return s.tmuxSession.SendKeys(keys)
}

// SendPromptToInstance types a prompt into one instance's pane and taps enter
// — the same two-step SendKeys-then-enter Instance.SendPrompt does for the
// main agent, but for whichever CLI this pane runs (the Cursor CLI pane).
func (t *TerminalPane) SendPromptToInstance(instance *session.Instance, prompt string) error {
	if err := t.SendKeysToInstance(instance, prompt); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)

	t.mu.Lock()
	s, ok := t.sessions[instance.ID()]
	t.mu.Unlock()
	if !ok || s.tmuxSession == nil {
		return fmt.Errorf("o painel da sessão '%s' ainda não subiu", instance.Title)
	}
	return s.tmuxSession.TapEnter()
}

// setFallbackState sets the terminal pane to display a fallback message.
// Caller must hold t.mu.
func (t *TerminalPane) setFallbackState(message string) {
	t.fallback = true
	t.fallbackText = lipgloss.JoinVertical(lipgloss.Center, FallBackText, "", message)
	t.content = ""
}

// UpdateContent captures the tmux pane output for the terminal session.
func (t *TerminalPane) UpdateContent(instance *session.Instance) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if instance == nil {
		t.setFallbackState("Selecione uma sessão para abrir um terminal")
		return nil
	}
	if instance.Status == session.Paused {
		t.setFallbackState("Sessão pausada. Retome para usar o terminal.")
		return nil
	}
	if !instance.Started() {
		t.setFallbackState("Sessão ainda não foi iniciada.")
		return nil
	}

	// Skip content updates while in scroll mode
	if t.isScrolling {
		return nil
	}

	// Ensure we have a terminal session for this instance
	t.fallback = false
	if err := t.ensureSessionLocked(instance); err != nil {
		return err
	}
	// ensureSessionLocked already explained why there is nothing to show.
	if t.fallback {
		return nil
	}

	// ensureSessionLocked just proved the session is alive, so it is not asked
	// again here: this runs ten times a second and each ask is a tmux process.
	s, ok := t.sessions[t.currentTitle]
	if !ok || s.tmuxSession == nil {
		t.setFallbackState("Sessão de terminal indisponível.")
		return nil
	}

	// The mosaic reshapes these same sessions to cell size, so the pane claims
	// its own shape back before reading.
	t.resizeLocked(s, t.width, t.height)

	content, err := s.tmuxSession.CapturePaneContent()
	if err != nil {
		// See CaptureForInstance: the read is the liveness check.
		delete(t.sessions, t.currentTitle)
		return fmt.Errorf("terminal pane: failed to capture content: %w", err)
	}

	t.fallback = false
	t.content = content
	return nil
}

// ensureSession creates or reuses a cached terminal tmux session for the given instance.
func (t *TerminalPane) ensureSession(instance *session.Instance) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ensureSessionLocked(instance)
}

// ensureSessionLocked is the lock-free implementation of ensureSession.
// Caller must hold t.mu.
func (t *TerminalPane) ensureSessionLocked(instance *session.Instance) error {
	if instance == nil || !instance.Started() || instance.Status == session.Paused {
		return nil
	}

	worktreePath := instance.Path
	if worktreePath == "" {
		return nil
	}

	t.currentTitle = instance.ID()

	// Check if we already have a cached session for this instance
	if s, ok := t.sessions[instance.ID()]; ok {
		if s.tmuxSession != nil {
			if time.Since(s.verifiedAt) < verifyEvery {
				return nil
			}
			if s.tmuxSession.DoesSessionExist() {
				s.verifiedAt = time.Now()
				return nil
			}
		}
		// Session died, remove stale entry and recreate below
		delete(t.sessions, instance.ID())
	}

	program := t.program
	if program == "" {
		program = os.Getenv("SHELL")
		if program == "" {
			program = "/bin/sh"
		}
	} else if _, err := exec.LookPath(program); err != nil {
		// A missing binary would make tmux start and die immediately, leaving
		// only a generic "unavailable" message. Name the actual problem.
		t.setFallbackState(fmt.Sprintf("Comando '%s' não encontrado no PATH.", program))
		return nil
	}

	termName := t.prefix + instance.ID()
	ts := tmux.NewTmuxSession(termName, program)

	// Check if session already exists (e.g. from a previous run)
	if ts.DoesSessionExist() {
		if err := ts.Restore(); err != nil {
			// Session exists but can't restore, kill it and start fresh
			_ = ts.Close()
			ts = tmux.NewTmuxSession(termName, program)
			if err := ts.Start(worktreePath); err != nil {
				return fmt.Errorf("terminal pane: failed to start session: %w", err)
			}
		}
	} else {
		if err := ts.Start(worktreePath); err != nil {
			return fmt.Errorf("terminal pane: failed to start session: %w", err)
		}
	}

	s := &terminalSession{
		tmuxSession:  ts,
		worktreePath: worktreePath,
		verifiedAt:   time.Now(),
	}
	t.sessions[instance.ID()] = s
	t.resizeLocked(s, t.width, t.height)

	return nil
}

// Attach attaches to the terminal tmux session (full-screen).
func (t *TerminalPane) Attach() (chan struct{}, error) {
	t.mu.Lock()
	s, ok := t.sessions[t.currentTitle]
	if !ok || s.tmuxSession == nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("no terminal session to attach to")
	}
	if !s.tmuxSession.DoesSessionExist() {
		t.mu.Unlock()
		return nil, fmt.Errorf("terminal session does not exist")
	}
	ts := s.tmuxSession
	t.mu.Unlock()
	return ts.Attach()
}

// Close kills all cached terminal tmux sessions and cleans up.
func (t *TerminalPane) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for title, s := range t.sessions {
		if s.tmuxSession != nil {
			if err := s.tmuxSession.Close(); err != nil {
				log.InfoLog.Printf("terminal pane: failed to close session for %s: %v", title, err)
			}
		}
	}
	t.sessions = make(map[string]*terminalSession)
	t.currentTitle = ""
	t.content = ""
	t.fallback = false
	t.fallbackText = ""
}

// CloseForInstance kills the cached terminal session for a specific instance.
func (t *TerminalPane) CloseForInstance(title string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[title]; ok {
		if s.tmuxSession != nil {
			if err := s.tmuxSession.Close(); err != nil {
				log.InfoLog.Printf("terminal pane: failed to close session for %s: %v", title, err)
			}
		}
		delete(t.sessions, title)
	}
	if t.currentTitle == title {
		t.currentTitle = ""
		t.content = ""
		t.fallback = false
		t.fallbackText = ""
	}
}

func (t *TerminalPane) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	width := t.width
	height := t.height

	if width == 0 || height == 0 {
		return strings.Repeat("\n", height)
	}

	if t.isScrolling {
		return t.viewport.View()
	}

	fallback := t.fallback
	fallbackText := t.fallbackText
	content := t.content

	if fallback {
		// 3 = tab bar height (border + padding + text), 4 = window style frame (top/bottom border + padding)
		availableHeight := height - 3 - 4
		fallbackLines := len(strings.Split(fallbackText, "\n"))
		totalPadding := availableHeight - fallbackLines
		topPadding := 0
		bottomPadding := 0
		if totalPadding > 0 {
			topPadding = totalPadding / 2
			bottomPadding = totalPadding - topPadding
		}

		var lines []string
		if topPadding > 0 {
			lines = append(lines, strings.Repeat("\n", topPadding))
		}
		lines = append(lines, fallbackText)
		if bottomPadding > 0 {
			lines = append(lines, strings.Repeat("\n", bottomPadding))
		}

		return terminalPaneStyle.
			Width(width).
			Align(lipgloss.Center).
			Render(strings.Join(lines, ""))
	}

	// Normal mode: show captured content
	lines := strings.Split(content, "\n")

	if height > 0 {
		if len(lines) > height {
			lines = lines[len(lines)-height:]
		} else {
			padding := height - len(lines)
			lines = append(lines, make([]string, padding)...)
		}
	}

	contentStr := strings.Join(lines, "\n")
	return terminalPaneStyle.Width(width).Render(contentStr)
}

// enterScrollMode captures the full terminal history and enters scroll mode.
// Caller must hold t.mu.
func (t *TerminalPane) enterScrollMode() error {
	s, ok := t.sessions[t.currentTitle]
	if !ok || s.tmuxSession == nil || !s.tmuxSession.DoesSessionExist() {
		return nil
	}

	content, err := s.tmuxSession.CapturePaneContentWithOptions("-", "-")
	if err != nil {
		return fmt.Errorf("terminal pane: failed to capture full history: %w", err)
	}

	footer := terminalFooterStyle.Render("ESC para sair do modo de rolagem")
	contentWithFooter := lipgloss.JoinVertical(lipgloss.Left, content, footer)
	t.viewport.SetContent(contentWithFooter)
	t.viewport.GotoBottom()
	t.isScrolling = true
	return nil
}

// ScrollUp enters scroll mode (if not already) and scrolls up.
func (t *TerminalPane) ScrollUp() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.isScrolling {
		return t.enterScrollMode()
	}
	t.viewport.LineUp(1)
	return nil
}

// ScrollDown enters scroll mode (if not already) and scrolls down.
func (t *TerminalPane) ScrollDown() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.isScrolling {
		return t.enterScrollMode()
	}
	t.viewport.LineDown(1)
	return nil
}

// ResetToNormalMode exits scroll mode and restores normal content display.
func (t *TerminalPane) ResetToNormalMode() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.isScrolling {
		return
	}
	t.isScrolling = false
	t.viewport.SetContent("")
	t.viewport.GotoTop()
}

// IsScrolling returns whether the terminal pane is in scroll mode.
func (t *TerminalPane) IsScrolling() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.isScrolling
}
