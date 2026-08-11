package session

import (
	"claude-squad/config"
	"claude-squad/log"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}

// initRepo makes dir a git repository with one commit so it has a HEAD.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		require.NoError(t, cmd.Run(), "git %v", args)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\n"), 0644))
	require.NoError(t, exec.Command("git", "-C", dir, "add", ".").Run())
	require.NoError(t, exec.Command("git", "-C", dir, "commit", "-m", "init").Run())
}

func TestValidateWorkingDir(t *testing.T) {
	plain := t.TempDir()
	repo := t.TempDir()
	initRepo(t, repo)

	t.Run("missing directory is rejected", func(t *testing.T) {
		err := ValidateWorkingDir(filepath.Join(plain, "nope"))
		assert.ErrorContains(t, err, "does not exist")
	})

	t.Run("a file is treated as a missing directory", func(t *testing.T) {
		file := filepath.Join(plain, "afile")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0644))
		assert.ErrorContains(t, ValidateWorkingDir(file), "does not exist")
	})

	t.Run("a plain directory is enough", func(t *testing.T) {
		assert.NoError(t, ValidateWorkingDir(plain), "no repository required")
	})

	t.Run("a repository works too", func(t *testing.T) {
		assert.NoError(t, ValidateWorkingDir(repo))
	})
}

func TestResolvePathExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := ResolvePath("~/somewhere")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "somewhere"), got)

	got, err = ResolvePath("~")
	require.NoError(t, err)
	assert.Equal(t, home, got)

	// Relative paths resolve against home, never against the current directory —
	// the coordinator must behave the same wherever it was launched.
	got, err = ResolvePath("projects/thing")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "projects", "thing"), got)

	got, err = ResolvePath("")
	require.NoError(t, err)
	assert.Equal(t, home, got)

	got, err = ResolvePath("/etc")
	require.NoError(t, err)
	assert.Equal(t, "/etc", got, "absolute paths are left alone")
}

// TestResolvePathIgnoresTheCurrentDirectory is the guarantee that lets the
// coordinator run from any pwd, including one that was deleted underneath it.
func TestResolvePathIgnoresTheCurrentDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	// Resolve the same input from two different working directories.
	original, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(original) }()

	require.NoError(t, os.Chdir(t.TempDir()))
	fromA, err := ResolvePath("some/where")
	require.NoError(t, err)

	require.NoError(t, os.Chdir(t.TempDir()))
	fromB, err := ResolvePath("some/where")
	require.NoError(t, err)

	assert.Equal(t, fromA, fromB)
	assert.Equal(t, filepath.Join(home, "some", "where"), fromA)
}

// TestSessionCreationWorksFromADeletedWorkingDirectory covers the nastiest pwd
// there is: one that no longer exists.
func TestSessionCreationWorksFromADeletedWorkingDirectory(t *testing.T) {
	target := t.TempDir()

	original, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(original) }()

	gone := filepath.Join(t.TempDir(), "vanishing")
	require.NoError(t, os.Mkdir(gone, 0755))
	require.NoError(t, os.Chdir(gone))
	require.NoError(t, os.RemoveAll(gone))

	inst, err := NewInstance(InstanceOptions{Title: "nowhere", Path: target, Program: "bash"})
	require.NoError(t, err, "the launch directory must not matter")
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	assert.Equal(t, target, inst.Path)
	assert.Equal(t, Running, inst.Status)
}

func TestDirectoryIsImmutableAfterStart(t *testing.T) {
	dir := t.TempDir()
	inst, err := NewInstance(InstanceOptions{Title: "frozen", Path: dir, Program: "bash"})
	require.NoError(t, err)

	require.NoError(t, inst.SetPath(dir), "settable before start")

	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	assert.Error(t, inst.SetPath(t.TempDir()))
	assert.Error(t, inst.SetTitle("other"))
}

// TestSessionLifecycle is the core promise: the coordinator runs the agent in
// the given directory and never touches its contents or its versioning.
func TestSessionLifecycle(t *testing.T) {
	repo := t.TempDir()
	initRepo(t, repo)
	statusBefore := gitStatus(t, repo)

	inst, err := NewInstance(InstanceOptions{Title: "run", Path: repo, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	assert.Equal(t, Running, inst.Status)

	// No stray branch was created and the index was not touched.
	assert.Equal(t, statusBefore, gitStatus(t, repo))
	assert.Equal(t, []string{"main"}, gitBranches(t, repo))
	assert.Equal(t, []string{repo}, gitWorktrees(t, repo), "no working copy is created")

	// Pause closes the terminal and leaves the directory alone.
	require.NoError(t, inst.Pause())
	assert.Equal(t, Paused, inst.Status)
	assert.FileExists(t, filepath.Join(repo, "file.txt"))

	// Resume reopens in the same directory, in whatever state it is in now.
	require.NoError(t, os.WriteFile(filepath.Join(repo, "file.txt"), []byte("one\ntwo\n"), 0644))
	require.NoError(t, inst.Resume())
	assert.Equal(t, Running, inst.Status)

	// The changes present in the directory show up in the diff.
	stats := inst.ComputeDiff()
	require.NotNil(t, stats)
	require.NoError(t, stats.Error)
	assert.Equal(t, 1, stats.Added)
	assert.Contains(t, stats.Content, "two")

	// Killing leaves the directory exactly as it was.
	require.NoError(t, inst.Kill())
	assert.FileExists(t, filepath.Join(repo, "file.txt"))
	assert.Equal(t, []string{"main"}, gitBranches(t, repo))
	assert.Equal(t, []string{repo}, gitWorktrees(t, repo))
}

func TestSessionOnPlainDirectoryHasNoDiffBase(t *testing.T) {
	dir := t.TempDir()
	inst, err := NewInstance(InstanceOptions{Title: "plain", Path: dir, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	stats := inst.ComputeDiff()
	require.NotNil(t, stats)
	assert.ErrorContains(t, stats.Error, "no comparison base")

	// Not an error state: the list must not blow up over it.
	assert.NoError(t, inst.UpdateDiffStats())
}

func TestSessionOnMissingDirectoryIsRefused(t *testing.T) {
	inst, err := NewInstance(InstanceOptions{
		Title: "nope", Path: filepath.Join(t.TempDir(), "gone"), Program: "bash"})
	require.NoError(t, err)

	err = inst.Start(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
	assert.False(t, inst.Started(), "nothing is left half-created")
}

func TestTwoSessionsShareADirectory(t *testing.T) {
	repo := t.TempDir()
	initRepo(t, repo)

	a, err := NewInstance(InstanceOptions{Title: "same", Path: repo, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, a.Start(true))
	defer func() { _ = a.Kill() }()

	// Same title, same directory — allowed, and they do not collide.
	b, err := NewInstance(InstanceOptions{Title: "same", Path: repo, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, b.Start(true))
	defer func() { _ = b.Kill() }()

	assert.NotEqual(t, a.ID(), b.ID(), "identity does not depend on the title")

	require.NoError(t, os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0644))
	statsA, statsB := a.ComputeDiff(), b.ComputeDiff()
	require.NoError(t, statsA.Error)
	require.NoError(t, statsB.Error)
	assert.Equal(t, statsA.Content, statsB.Content, "both see the same directory")
}

func TestSessionGoesOrphanWhenDirectoryDisappears(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workdir")
	require.NoError(t, os.Mkdir(dir, 0755))

	inst, err := NewInstance(InstanceOptions{Title: "orphan", Path: dir, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	require.NoError(t, os.RemoveAll(dir))

	assert.False(t, inst.WorkingDirExists())
	assert.True(t, inst.CheckOrphaned())
	assert.Equal(t, Orphaned, inst.Status)

	// Invalid transitions are refused with an explicit message, changing nothing.
	assert.ErrorContains(t, inst.Pause(), "orphaned")
	assert.Equal(t, Orphaned, inst.Status)
	_, err = inst.Attach()
	assert.ErrorContains(t, err, "orphaned")
}

func TestResumeRefusedWhenNotPausedAndWhenDirectoryVanished(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workdir")
	require.NoError(t, os.Mkdir(dir, 0755))

	inst, err := NewInstance(InstanceOptions{Title: "resume", Path: dir, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	assert.ErrorContains(t, inst.Resume(), "only resume paused")

	require.NoError(t, inst.Pause())
	require.NoError(t, os.RemoveAll(dir))
	assert.ErrorContains(t, inst.Resume(), "no longer exists")
	assert.Equal(t, Orphaned, inst.Status, "a vanished directory orphans instead of resuming")
}

// memState is a minimal in-memory InstanceStorage for storage round-trip tests.
type memState struct{ data json.RawMessage }

func (m *memState) GetInstances() json.RawMessage { return m.data }
func (m *memState) SaveInstances(d json.RawMessage) error {
	m.data = d
	return nil
}
func (m *memState) DeleteAllInstances() error { m.data = json.RawMessage("[]"); return nil }

var _ config.InstanceStorage = (*memState)(nil)

func TestRecordsFromOlderVersionsStillLoad(t *testing.T) {
	dir := t.TempDir()

	// A record written when sessions still had branches, modes and worktrees.
	raw := `[{"title":"legacy","path":"` + dir + `","branch":"b","status":3,"program":"bash","mode":0,
		"worktree":{"repo_path":"/tmp","worktree_path":"/tmp/wt","session_name":"legacy",
		"branch_name":"b","base_commit_sha":"abc","is_existing_branch":false},
		"diff_stats":{"added":0,"removed":0,"content":""}}]`

	var datas []InstanceData
	require.NoError(t, json.Unmarshal([]byte(raw), &datas), "unknown fields are ignored")
	require.Len(t, datas, 1)

	inst, err := FromInstanceData(datas[0])
	require.NoError(t, err)
	assert.Equal(t, "legacy", inst.ID(), "identity falls back to the title")
	assert.Equal(t, dir, inst.Path)
	assert.Equal(t, Paused, inst.Status, "a paused session stays paused")
}

func TestStorageRoundTripKeepsDirectoryAndIdentity(t *testing.T) {
	dir := t.TempDir()
	state := &memState{data: json.RawMessage("[]")}
	storage, err := NewStorage(state)
	require.NoError(t, err)

	inst, err := NewInstance(InstanceOptions{Title: "kept", Path: dir, Program: "bash"})
	require.NoError(t, err)
	require.NoError(t, inst.Start(true))
	defer func() { _ = inst.Kill() }()

	require.NoError(t, storage.SaveInstances([]*Instance{inst}))

	assert.NotContains(t, string(state.data), "worktree", "no git leftovers are written")

	var datas []InstanceData
	require.NoError(t, json.Unmarshal(state.data, &datas))
	require.Len(t, datas, 1)
	assert.Equal(t, inst.ID(), datas[0].SessionID)
	assert.Equal(t, dir, datas[0].Path, "the directory travels with the session")
}

func TestOrphanedSessionRestoresAsOrphaned(t *testing.T) {
	data := InstanceData{
		Title:   "gone",
		Path:    filepath.Join(t.TempDir(), "vanished"),
		Program: "bash",
		Status:  Ready,
	}
	inst, err := FromInstanceData(data)
	require.NoError(t, err)
	assert.Equal(t, Orphaned, inst.Status)
	assert.Contains(t, inst.Path, "vanished", "the lost path stays visible")
}

func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	require.NoError(t, err)
	return string(out)
}

func gitBranches(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "for-each-ref", "--format=%(refname:short)", "refs/heads").Output()
	require.NoError(t, err)
	return nonEmptyLines(string(out))
}

func gitWorktrees(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").Output()
	require.NoError(t, err)
	var paths []string
	for _, line := range nonEmptyLines(string(out)) {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
