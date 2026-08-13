package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDiffBaseCache covers what the remembered answer is allowed to do: a plain
// directory is asked again every time (it can be initialized while the session
// is open), a directory with a commit is only asked once, and a repository that
// disappears is forgotten instead of being trusted forever.
func TestDiffBaseCache(t *testing.T) {
	dir := t.TempDir()

	// A plain directory has no base, and is never remembered as having one.
	if got := DirectDiff(dir, true); got.Error != ErrNoDiffBase {
		t.Fatalf("plain dir: got error %v, want ErrNoDiffBase", got.Error)
	}
	if diffBaseReady(dir) {
		t.Fatal("plain dir was cached as having a diff base")
	}

	// The same directory, once it is a repository with a commit, answers and
	// is remembered.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable or failing (%v): %s", err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "first")

	if got := DirectDiff(dir, true); got.Error != nil {
		t.Fatalf("repo with commit: unexpected error %v", got.Error)
	}
	if !diffBaseReady(dir) {
		t.Fatal("repo with commit was not cached")
	}

	// The counters still come from the real diff while the cache is warm.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DirectDiff(dir, true); got.Added != 1 || got.Removed != 0 {
		t.Fatalf("warm cache: got +%d-%d, want +1-0", got.Added, got.Removed)
	}

	// The repository going away drops the remembered answer.
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	if got := DirectDiff(dir, true); got.Error == nil {
		t.Fatal("removed repo: expected an error")
	}
	if diffBaseReady(dir) {
		t.Fatal("removed repo stayed cached")
	}
}

func TestParseNumstat(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantAdded   int
		wantRemoved int
	}{
		{
			name:        "empty output",
			input:       "",
			wantAdded:   0,
			wantRemoved: 0,
		},
		{
			name:        "single file",
			input:       "3\t1\tfoo.go\n",
			wantAdded:   3,
			wantRemoved: 1,
		},
		{
			name:        "multiple files sum correctly",
			input:       "3\t1\tfoo.go\n10\t2\tbar/baz.go\n",
			wantAdded:   13,
			wantRemoved: 3,
		},
		{
			name:        "binary files are skipped",
			input:       "5\t0\tfoo.go\n-\t-\timage.png\n2\t2\tbar.go\n",
			wantAdded:   7,
			wantRemoved: 2,
		},
		{
			name:        "path with tabs is preserved via SplitN",
			input:       "4\t4\tpath\twith\ttabs.go\n",
			wantAdded:   4,
			wantRemoved: 4,
		},
		{
			name:        "trailing newlines do not add garbage",
			input:       "1\t0\ta.go\n\n\n",
			wantAdded:   1,
			wantRemoved: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAdded, gotRemoved := parseNumstat(tt.input)
			if gotAdded != tt.wantAdded || gotRemoved != tt.wantRemoved {
				t.Errorf("parseNumstat(%q) = (%d, %d), want (%d, %d)",
					tt.input, gotAdded, gotRemoved, tt.wantAdded, tt.wantRemoved)
			}
		})
	}
}
