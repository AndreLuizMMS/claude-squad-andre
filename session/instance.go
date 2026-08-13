package session

import (
	"claude-squad/cmd"
	"claude-squad/log"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"crypto/sha256"
	"encoding/json"
	"io"
	"path/filepath"

	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Status int

const (
	// Running is the status when the instance is running and claude is working.
	Running Status = iota
	// Ready is if the claude instance is ready to be interacted with (waiting for user input).
	Ready
	// Loading is if the instance is loading (if we are starting it up or something).
	Loading
	// Paused is if the instance's terminal was closed but the session is kept.
	Paused
	// Orphaned is if the instance's working directory no longer exists.
	Orphaned
	// Exited is if the agent's process ended on its own — it crashed, was
	// killed from outside, or simply quit. The terminal is gone but the session
	// and its directory are still here, so it can be started again.
	Exited
)

// Instance is a running agent working in a directory. The coordinator manages
// agents, not code: it runs the agent where it was told to and never touches
// versioning.
type Instance struct {
	// Title is the title of the instance.
	Title string
	// Path is the working directory the agent runs in. Immutable after start.
	Path string
	// Status is the status of the instance.
	Status Status
	// Program is the program to run in the instance.
	Program string
	// Height is the height of the instance.
	Height int
	// Width is the width of the instance.
	Width int
	// CreatedAt is the time the instance was created.
	CreatedAt time.Time
	// UpdatedAt is the time the instance was last updated.
	UpdatedAt time.Time
	// AutoYes is true if the instance should automatically press enter when prompted.
	AutoYes bool
	// Prompt is the initial prompt to pass to the instance on startup
	Prompt string
	// SessionID is the stable identity of the session. Titles may repeat, this
	// may not. Empty for instances stored by older versions, which fall back to
	// the title.
	SessionID string
	// NeedsAttention marks a session that finished working and is waiting on the
	// developer. It stays on until the session is opened, so an answer that
	// arrives while you are looking elsewhere is still there when you come back.
	NeedsAttention bool
	// NeedsApproval marks a session stopped on a question it cannot answer by
	// itself. Unlike NeedsAttention, which is an answer waiting to be read,
	// this is the agent blocked: nothing moves until the developer replies.
	NeedsApproval bool

	// diffStats stores the current diff statistics for the working directory.
	diffStats *git.DiffStats

	// usageStats stores the most recently read context-window usage for the
	// Claude Code session running in this instance.
	usageStats *UsageStats

	// The below fields are initialized upon calling Start().

	started bool
	// tmuxSession is the tmux session for the instance.
	tmuxSession *tmux.TmuxSession
}

// ToInstanceData converts an Instance to its serializable form
func (i *Instance) ToInstanceData() InstanceData {
	data := InstanceData{
		Title:     i.Title,
		Path:      i.Path,
		Status:    i.Status,
		Height:    i.Height,
		Width:     i.Width,
		CreatedAt: i.CreatedAt,
		UpdatedAt: time.Now(),
		Program:   i.Program,
		AutoYes:   i.AutoYes,
		SessionID: i.ID(),
	}

	// Only include diff stats if they exist
	if i.diffStats != nil {
		data.DiffStats = DiffStatsData{
			Added:   i.diffStats.Added,
			Removed: i.diffStats.Removed,
			Content: i.diffStats.Content,
		}
	}

	return data
}

// FromInstanceData creates a new Instance from serialized data
func FromInstanceData(data InstanceData) (*Instance, error) {
	instance := &Instance{
		Title:     data.Title,
		Path:      data.Path,
		Status:    data.Status,
		Height:    data.Height,
		Width:     data.Width,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
		Program:   data.Program,
		SessionID: data.SessionID,
		diffStats: &git.DiffStats{
			Added:   data.DiffStats.Added,
			Removed: data.DiffStats.Removed,
			Content: data.DiffStats.Content,
		},
	}

	// The working directory is the one thing a session cannot live without.
	if !isDir(instance.Path) {
		instance.started = true
		instance.SetStatus(Orphaned)
		instance.tmuxSession = tmux.NewTmuxSession(instance.ID(), instance.Program)
		return instance, nil
	}

	instance.tmuxSession = tmux.NewTmuxSession(instance.ID(), instance.Program)

	// A reboot (or any kill of the tmux server) takes the terminal down while the
	// record survives. There is nothing to reattach to, so the session comes back
	// paused instead of half-alive: it can be resumed in place.
	if instance.Paused() || !instance.tmuxSession.DoesSessionExist() {
		instance.started = true
		instance.SetStatus(Paused)
		return instance, nil
	}

	if err := instance.Start(false); err != nil {
		return nil, err
	}

	return instance, nil
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Options for creating a new instance
type InstanceOptions struct {
	// Title is the title of the instance.
	Title string
	// Path is the working directory the agent runs in.
	Path string
	// Program is the program to run in the instance (e.g. "claude", "aider --model ollama_chat/gemma3:1b")
	Program string
	// If AutoYes is true, then
	AutoYes bool
}

func NewInstance(opts InstanceOptions) (*Instance, error) {
	t := time.Now()

	absPath, err := ResolvePath(opts.Path)
	if err != nil {
		return nil, err
	}

	return &Instance{
		Title:     opts.Title,
		Status:    Ready,
		Path:      absPath,
		Program:   opts.Program,
		Height:    0,
		Width:     0,
		CreatedAt: t,
		UpdatedAt: t,
		AutoYes:   false,
	}, nil
}

// ResolvePath expands "~" and turns a relative path into an absolute one.
//
// Relative paths resolve against the home directory, not the current one: the
// coordinator can be launched from anywhere (including a directory that no
// longer exists), so where it happens to sit must never change what a typed
// path means. This also matches what the directory field's completion offers.
//
// It does not check that the path exists — see ValidateWorkingDir for that.
func ResolvePath(path string) (string, error) {
	path = strings.TrimSpace(path)

	home, homeErr := os.UserHomeDir()
	if path == "" || path == "~" {
		if homeErr != nil {
			return "", fmt.Errorf("failed to locate home directory: %w", homeErr)
		}
		return home, nil
	}

	path = strings.TrimPrefix(path, "~/")
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if homeErr != nil {
		return "", fmt.Errorf("failed to locate home directory: %w", homeErr)
	}
	return filepath.Join(home, path), nil
}

// ValidateWorkingDir checks that path can host a session. Any directory will do
// — versioned or not — as long as it exists and can be written to.
func ValidateWorkingDir(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		// A path pointing at a file is treated the same as a missing directory.
		return fmt.Errorf("directory does not exist: %s", path)
	}
	if err := checkWritable(path); err != nil {
		return fmt.Errorf("directory is not writable: %s", path)
	}
	return nil
}

// checkWritable reports whether we can create files in dir, by actually trying.
// Permission bits alone lie on too many filesystems.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".cs-writecheck-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// ID returns the stable identity of the session, used to name the tmux session
// and to key storage. Falls back to the title for instances stored by older
// versions, which had no explicit identity.
func (i *Instance) ID() string {
	if i.SessionID != "" {
		return i.SessionID
	}
	return i.Title
}

// ensureSessionID assigns a unique identity derived from the title. Titles may
// repeat; tmux session names may not.
func (i *Instance) ensureSessionID() {
	if i.SessionID != "" {
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", i.Title, i.Path, i.CreatedAt.UnixNano())))
	i.SessionID = fmt.Sprintf("%s-%x", i.Title, sum[:3])
}

// DirName is the short name of the working directory, used when several
// sessions from different places are listed together.
func (i *Instance) DirName() string {
	return filepath.Base(i.Path)
}

// Orphaned reports whether the session lost its working directory.
func (i *Instance) Orphaned() bool {
	return i.Status == Orphaned
}

// SetPath sets the working directory of the instance. Returns an error if the
// instance has started — the directory is immutable after creation.
func (i *Instance) SetPath(path string) error {
	if i.started {
		return fmt.Errorf("cannot change directory of a started instance")
	}
	abs, err := ResolvePath(path)
	if err != nil {
		return err
	}
	i.Path = abs
	return nil
}

// WorkingDirExists reports whether the session's working directory is still there.
func (i *Instance) WorkingDirExists() bool {
	return isDir(i.Path)
}

// CheckOrphaned marks the instance orphaned if its working directory vanished.
// Returns true if the instance is orphaned after the check.
func (i *Instance) CheckOrphaned() bool {
	if !i.started || i.Status == Orphaned {
		return i.Status == Orphaned
	}
	if !isDir(i.Path) {
		i.SetStatus(Orphaned)
		return true
	}
	return false
}

func (i *Instance) SetStatus(status Status) {
	i.Status = status
}

// firstTimeSetup is true if this is a new instance. Otherwise, it's one loaded from storage.
func (i *Instance) Start(firstTimeSetup bool) error {
	if i.Title == "" {
		return fmt.Errorf("instance title cannot be empty")
	}

	if firstTimeSetup {
		if err := ValidateWorkingDir(i.Path); err != nil {
			return err
		}
		i.ensureSessionID()
	}

	var tmuxSession *tmux.TmuxSession
	if i.tmuxSession != nil {
		// Use existing tmux session (useful for testing)
		tmuxSession = i.tmuxSession
	} else {
		// Create new tmux session
		tmuxSession = tmux.NewTmuxSession(i.ID(), i.Program)
	}
	i.tmuxSession = tmuxSession

	// Setup error handler to cleanup resources on any error
	var setupErr error
	defer func() {
		if setupErr != nil {
			if cleanupErr := i.Kill(); cleanupErr != nil {
				setupErr = fmt.Errorf("%v (cleanup error: %v)", setupErr, cleanupErr)
			}
		} else {
			i.started = true
		}
	}()

	if !firstTimeSetup {
		// Reuse existing session
		if err := tmuxSession.Restore(); err != nil {
			setupErr = fmt.Errorf("failed to restore existing session: %w", err)
			return setupErr
		}
	} else {
		// Nothing to prepare: run the agent where the developer pointed us.
		if err := i.tmuxSession.Start(i.Path); err != nil {
			setupErr = fmt.Errorf("failed to start new session: %w", err)
			return setupErr
		}
	}

	i.SetStatus(Running)

	return nil
}

// Kill terminates the instance. Only the terminal is closed — the working
// directory is left exactly as it is.
func (i *Instance) Kill() error {
	if !i.started {
		// If instance was never started, just return success
		return nil
	}

	var errs []error

	if i.tmuxSession != nil {
		if err := i.tmuxSession.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close tmux session: %w", err))
		}
	}

	return i.combineErrors(errs)
}

// combineErrors combines multiple errors into a single error
func (i *Instance) combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple cleanup errors occurred:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}

func (i *Instance) Preview() (string, error) {
	if i.inactive() {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContent()
}

func (i *Instance) HasUpdated() (updated bool, hasPrompt bool, busy bool, err error) {
	if !i.started {
		return false, false, false, nil
	}
	return i.tmuxSession.HasUpdated()
}

// ApplyCapture answers the same questions HasUpdated does, from a screen that
// was read in a batch instead of one process at a time.
func (i *Instance) ApplyCapture(content string) (updated bool, hasPrompt bool, busy bool) {
	if !i.started || i.tmuxSession == nil {
		return false, false, false
	}
	return i.tmuxSession.Observe(content)
}

// CapturePanes reads the agent screens of several sessions with a single tmux
// process and returns them keyed by session ID. Sessions with no terminal to
// read are skipped, and so is any session whose read failed — the caller sees
// them missing from the map and decides what that means.
func CapturePanes(instances []*Instance) map[string]string {
	byName := make(map[string]*Instance, len(instances))
	names := make([]string, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || inst.inactive() || inst.tmuxSession == nil {
			continue
		}
		name := inst.tmuxSession.Name()
		byName[name] = inst
		names = append(names, name)
	}

	captured := tmux.CaptureMany(cmd.MakeExecutor(), names)
	out := make(map[string]string, len(captured))
	for name, content := range captured {
		out[byName[name].ID()] = content
	}
	return out
}

// HasBusyMarker reports whether this session's agent says out loud when it is
// working. When it does, that is the signal to trust; when it does not, all we
// have is the pane changing.
func (i *Instance) HasBusyMarker() bool {
	if !i.started || i.tmuxSession == nil {
		return false
	}
	return i.tmuxSession.HasBusyMarker()
}

// CheckAndHandleTrustPrompt checks for and dismisses the trust prompt for supported programs.
func (i *Instance) CheckAndHandleTrustPrompt() bool {
	if !i.started || i.tmuxSession == nil {
		return false
	}
	program := i.Program
	if !strings.HasSuffix(program, tmux.ProgramClaude) &&
		!strings.HasSuffix(program, tmux.ProgramAider) &&
		!strings.HasSuffix(program, tmux.ProgramGemini) {
		return false
	}
	return i.tmuxSession.CheckAndHandleTrustPrompt()
}

// TapEnter sends an enter key press to the tmux session if AutoYes is enabled.
func (i *Instance) TapEnter() {
	if !i.started || !i.AutoYes {
		return
	}
	if err := i.tmuxSession.TapEnter(); err != nil {
		log.ErrorLog.Printf("error tapping enter: %v", err)
	}
}

func (i *Instance) Attach() (chan struct{}, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot attach instance that has not been started")
	}
	if i.Status == Paused {
		return nil, fmt.Errorf("cannot attach a paused session, resume it first")
	}
	if i.Status == Orphaned {
		return nil, fmt.Errorf("cannot attach an orphaned session, kill it instead")
	}
	if i.Status == Exited {
		return nil, fmt.Errorf("the agent exited, press r to start it again")
	}
	return i.tmuxSession.Attach()
}

func (i *Instance) SetPreviewSize(width, height int) error {
	if i.inactive() {
		return fmt.Errorf("cannot set preview size for instance that has not been started or " +
			"is paused")
	}
	return i.tmuxSession.SetDetachedSize(width, height)
}

func (i *Instance) Started() bool {
	return i.started
}

// SetTitle sets the title of the instance. A running session can be renamed as
// long as it has an explicit identity: the terminal is named after the
// SessionID, so the title is only a label. Records written before identities
// existed still key everything off the title and cannot be renamed.
func (i *Instance) SetTitle(title string) error {
	// Not trimmed: during creation the title is typed one key at a time, and a
	// trailing space is the developer in the middle of a word.
	if i.started {
		if i.SessionID == "" {
			return fmt.Errorf("sessão antiga sem identidade própria não pode ser renomeada")
		}
		if strings.TrimSpace(title) == "" {
			return fmt.Errorf("o nome não pode ficar vazio")
		}
	}
	i.Title = title
	return nil
}

func (i *Instance) Paused() bool {
	return i.Status == Paused
}

// HasExited reports whether the agent's process ended without being asked to.
func (i *Instance) HasExited() bool {
	return i.Status == Exited
}

// inactive reports whether the session has no terminal to talk to. Every read
// of the agent's screen has to check this first: there is no pane to capture.
func (i *Instance) inactive() bool {
	return !i.started || i.Status == Paused || i.Status == Orphaned || i.Status == Exited
}

// TmuxAlive returns true if the tmux session is alive. This is a sanity check before attaching.
func (i *Instance) TmuxAlive() bool {
	return i.tmuxSession.DoesSessionExist()
}

// Pause closes the terminal and keeps the session in the list. The working
// directory is left untouched — there is nothing to release and nothing to
// commit.
func (i *Instance) Pause() error {
	if !i.started {
		return fmt.Errorf("cannot pause instance that has not been started")
	}
	if i.Status == Paused {
		return fmt.Errorf("instance is already paused")
	}
	if i.Status == Orphaned {
		return fmt.Errorf("cannot pause an orphaned session, kill it instead")
	}
	if i.Status == Exited {
		return fmt.Errorf("the agent already exited, there is no terminal to close")
	}

	var errs []error

	if err := i.tmuxSession.DetachSafely(); err != nil {
		errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
		log.ErrorLog.Print(err)
	}
	if err := i.tmuxSession.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close tmux session: %w", err))
		log.ErrorLog.Print(err)
	}

	i.SetStatus(Paused)
	return i.combineErrors(errs)
}

// Resume reopens the terminal in the same directory, in whatever state it is in
// now. Nothing is compared and nothing is restored.
func (i *Instance) Resume() error {
	if !i.started {
		return fmt.Errorf("cannot resume instance that has not been started")
	}
	// A session whose agent quit on its own resumes the same way a paused one
	// does: a fresh terminal in the same directory, picking the conversation
	// back up. Nothing else is left to restore.
	if i.Status != Paused && i.Status != Exited {
		return fmt.Errorf("can only resume paused or exited instances")
	}

	// A session whose directory vanished cannot be resumed.
	if !isDir(i.Path) {
		i.SetStatus(Orphaned)
		return fmt.Errorf("working directory no longer exists: %s", i.Path)
	}

	i.tmuxSession = tmux.NewTmuxSession(i.ID(), resumeCommand(i.Program))
	if err := i.tmuxSession.Start(i.Path); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to start new session: %w", err)
	}

	i.SetStatus(Running)
	return nil
}

// resumeCommand picks up the previous conversation when the agent supports it,
// falling back to a fresh run when there is nothing to pick up.
// ponytail: --continue takes the newest conversation of the directory; two
// sessions sharing one directory can pick up each other's chat.
func resumeCommand(program string) string {
	fields := strings.Fields(program)
	if len(fields) == 0 || !strings.HasSuffix(fields[0], tmux.ProgramClaude) {
		return program
	}
	if strings.Contains(program, "--continue") || strings.Contains(program, "--resume") {
		return program
	}
	return fmt.Sprintf("%s --continue || %s", program, program)
}

// UpdateDiffStats refreshes the uncommitted changes present in the working
// directory.
func (i *Instance) UpdateDiffStats() error {
	if !i.started {
		i.diffStats = nil
		return nil
	}

	if i.Status == Paused || i.Status == Orphaned {
		// Keep the previous diff stats if the instance is paused
		return nil
	}

	stats := i.ComputeDiff()
	if stats != nil && stats.Error != nil && stats.Error != git.ErrNoDiffBase {
		return fmt.Errorf("failed to get diff stats: %w", stats.Error)
	}

	i.diffStats = stats
	return nil
}

// ComputeDiff runs the expensive diff I/O and returns the result without
// mutating instance state. Safe to call from a background goroutine.
func (i *Instance) ComputeDiff() *git.DiffStats {
	if i.inactive() {
		return nil
	}
	return git.DirectDiff(i.Path, false)
}

// ComputeDiffNumstat returns only the added/removed line counts (Content is left
// empty). Safe to call from a background goroutine. Use this for instances whose
// full diff content is not currently needed so we avoid keeping large diffs in
// memory.
func (i *Instance) ComputeDiffNumstat() *git.DiffStats {
	if i.inactive() {
		return nil
	}
	return git.DirectDiff(i.Path, true)
}

// SetDiffStats sets the diff statistics on the instance. Should be called from
// the main event loop to avoid data races with View.
func (i *Instance) SetDiffStats(stats *git.DiffStats) {
	i.diffStats = stats
}

// GetDiffStats returns the current diff statistics
func (i *Instance) GetDiffStats() *git.DiffStats {
	return i.diffStats
}

// claudeContextWindow is the token budget /status measures usage against.
// Claude Code sessions run on the 200k-token model tier.
const claudeContextWindow = 200_000

// usageTailBytes bounds how much of a transcript file we read to find the
// latest usage line, so a long-running session's multi-MB transcript doesn't
// get read in full on every poll.
const usageTailBytes = 64 * 1024

// UsageStats is the context-window usage of the Claude Code session running
// in an instance, read from its transcript file — the same numbers /status
// reports inside the interactive session.
type UsageStats struct {
	// Percent is how full the context window is, 0-100.
	Percent int
}

// tokenUsage is the usage field of a Claude Code transcript assistant
// message.
type tokenUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// transcriptLine is the subset of a Claude Code transcript JSONL line we
// care about. Only assistant messages carry a usage field.
type transcriptLine struct {
	Message struct {
		Usage *tokenUsage `json:"usage"`
	} `json:"message"`
}

// ComputeUsage reads the instance's most recent Claude Code transcript and
// returns its current context-window usage. Returns nil if the instance
// isn't running or no transcript is found yet. Safe to call from a
// background goroutine.
func (i *Instance) ComputeUsage() *UsageStats {
	if i.inactive() {
		return nil
	}
	return computeUsageFromTranscripts(i.Path)
}

// SetUsageStats sets the usage statistics on the instance. Should be called
// from the main event loop to avoid data races with View.
func (i *Instance) SetUsageStats(stats *UsageStats) {
	i.usageStats = stats
}

// GetUsageStats returns the current context-window usage statistics.
func (i *Instance) GetUsageStats() *UsageStats {
	return i.usageStats
}

// claudeProjectDirName mirrors Claude Code's own transcript-directory naming:
// every '/' and '.' in the working directory path becomes '-'.
func claudeProjectDirName(path string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(path)
}

// computeUsageFromTranscripts finds the most recently modified transcript for
// workDir under ~/.claude/projects and returns the usage of its last
// assistant message.
func computeUsageFromTranscripts(workDir string) *UsageStats {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	projectDir := filepath.Join(home, ".claude", "projects", claudeProjectDirName(workDir))
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}

	var latest string
	var latestMod time.Time
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMod) {
			latestMod = info.ModTime()
			latest = filepath.Join(projectDir, e.Name())
		}
	}
	if latest == "" {
		return nil
	}

	usage := lastUsageInFile(latest)
	if usage == nil {
		return nil
	}

	total := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	percent := total * 100 / claudeContextWindow
	if percent > 100 {
		percent = 100
	}
	return &UsageStats{Percent: percent}
}

// lastUsageInFile scans the tail of a transcript file for the last line
// carrying a usage field.
func lastUsageInFile(path string) *tokenUsage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	start := int64(0)
	if info.Size() > usageTailBytes {
		start = info.Size() - usageTailBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	for idx := len(lines) - 1; idx >= 0; idx-- {
		line := strings.TrimSpace(lines[idx])
		if line == "" {
			continue
		}
		var tl transcriptLine
		if err := json.Unmarshal([]byte(line), &tl); err != nil {
			continue
		}
		if tl.Message.Usage != nil {
			return tl.Message.Usage
		}
	}
	return nil
}

// SendPrompt sends a prompt to the tmux session
func (i *Instance) SendPrompt(prompt string) error {
	if !i.started {
		return fmt.Errorf("instance not started")
	}
	if i.tmuxSession == nil {
		return fmt.Errorf("tmux session not initialized")
	}
	if err := i.tmuxSession.SendKeys(prompt); err != nil {
		return fmt.Errorf("error sending keys to tmux session: %w", err)
	}

	// Brief pause to prevent carriage return from being interpreted as newline
	time.Sleep(100 * time.Millisecond)
	if err := i.tmuxSession.TapEnter(); err != nil {
		return fmt.Errorf("error tapping enter: %w", err)
	}

	return nil
}

// PreviewFullHistory captures the entire tmux pane output including full scrollback history
func (i *Instance) PreviewFullHistory() (string, error) {
	if i.inactive() {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContentWithOptions("-", "-")
}

// PreviewWindow captures a screenful of the session as it looked `offset` lines
// above the live screen. Offset zero is the live screen itself, so the same call
// serves a cell that is following the agent and one that is reading back.
//
// tmux counts line 0 at the top of what is on screen and counts into the
// scrollback with negative numbers, which is why a window of `height` lines
// ending `offset` above the bottom is exactly -offset to height-1-offset. tmux
// clamps a start beyond the oldest line it kept, so scrolling past the top of
// the history stops there instead of coming back empty.
func (i *Instance) PreviewWindow(offset, height int) (string, error) {
	if i.inactive() {
		return "", nil
	}
	if offset <= 0 || height <= 0 {
		return i.tmuxSession.CapturePaneContent()
	}
	return i.tmuxSession.CapturePaneContentWithOptions(
		strconv.Itoa(-offset), strconv.Itoa(height-1-offset))
}

// HistorySize is how far back this session can be read.
func (i *Instance) HistorySize() (int, error) {
	if i.inactive() {
		return 0, nil
	}
	return i.tmuxSession.HistorySize()
}

// CursorPosition is where the caret sits on this session's screen.
func (i *Instance) CursorPosition() (x, y int, visible bool, err error) {
	if i.inactive() {
		return 0, 0, false, nil
	}
	return i.tmuxSession.CursorPosition()
}

// SetTmuxSession sets the tmux session for testing purposes
func (i *Instance) SetTmuxSession(session *tmux.TmuxSession) {
	i.tmuxSession = session
}

// SendKeys sends keys to the tmux session
func (i *Instance) SendKeys(keys string) error {
	if i.inactive() {
		return fmt.Errorf("cannot send keys to instance that has not been started or is paused")
	}
	return i.tmuxSession.SendKeys(keys)
}
