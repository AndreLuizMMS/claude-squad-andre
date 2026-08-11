package session

import (
	"claude-squad/log"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"crypto/sha256"
	"path/filepath"

	"fmt"
	"os"
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

	// diffStats stores the current diff statistics for the working directory.
	diffStats *git.DiffStats

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

	if instance.Paused() {
		instance.started = true
		instance.tmuxSession = tmux.NewTmuxSession(instance.ID(), instance.Program)
	} else {
		if err := instance.Start(false); err != nil {
			return nil, err
		}
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
	if !i.started || i.Status == Paused || i.Status == Orphaned {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContent()
}

func (i *Instance) HasUpdated() (updated bool, hasPrompt bool) {
	if !i.started {
		return false, false
	}
	return i.tmuxSession.HasUpdated()
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

func (i *Instance) SetPreviewSize(width, height int) error {
	if !i.started || i.Status == Paused || i.Status == Orphaned {
		return fmt.Errorf("cannot set preview size for instance that has not been started or " +
			"is paused")
	}
	return i.tmuxSession.SetDetachedSize(width, height)
}

func (i *Instance) Started() bool {
	return i.started
}

// SetTitle sets the title of the instance. Returns an error if the instance has started.
// We cant change the title once it's been used for a tmux session etc.
func (i *Instance) SetTitle(title string) error {
	if i.started {
		return fmt.Errorf("cannot change title of a started instance")
	}
	i.Title = title
	return nil
}

func (i *Instance) Paused() bool {
	return i.Status == Paused
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
	if i.Status != Paused {
		return fmt.Errorf("can only resume paused instances")
	}

	// A session whose directory vanished cannot be resumed.
	if !isDir(i.Path) {
		i.SetStatus(Orphaned)
		return fmt.Errorf("working directory no longer exists: %s", i.Path)
	}

	if err := i.tmuxSession.Start(i.Path); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to start new session: %w", err)
	}

	i.SetStatus(Running)
	return nil
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
	if !i.started || i.Status == Paused || i.Status == Orphaned {
		return nil
	}
	return git.DirectDiff(i.Path, false)
}

// ComputeDiffNumstat returns only the added/removed line counts (Content is left
// empty). Safe to call from a background goroutine. Use this for instances whose
// full diff content is not currently needed so we avoid keeping large diffs in
// memory.
func (i *Instance) ComputeDiffNumstat() *git.DiffStats {
	if !i.started || i.Status == Paused || i.Status == Orphaned {
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
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContentWithOptions("-", "-")
}

// SetTmuxSession sets the tmux session for testing purposes
func (i *Instance) SetTmuxSession(session *tmux.TmuxSession) {
	i.tmuxSession = session
}

// SendKeys sends keys to the tmux session
func (i *Instance) SendKeys(keys string) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot send keys to instance that has not been started or is paused")
	}
	return i.tmuxSession.SendKeys(keys)
}
