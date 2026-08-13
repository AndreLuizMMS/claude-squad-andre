package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFakeHome points HOME at a temp directory holding the given subdirectories
// (and one file, to prove files are never offered).
func withFakeHome(t *testing.T, dirs ...string) string {
	t.Helper()
	home := t.TempDir()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "notadir.txt"), []byte("x"), 0644))
	t.Setenv("HOME", home)
	return home
}

func TestCompleteDirsListsHomeByDefault(t *testing.T) {
	withFakeHome(t, "projects", "pictures", ".hidden")

	got := completeDirs("~/")
	assert.Equal(t, []string{"~/pictures/", "~/projects/"}, got,
		"only directories, hidden ones left out")
}

func TestCompleteDirsIgnoresFiles(t *testing.T) {
	withFakeHome(t, "notes")

	assert.NotContains(t, completeDirs("~/"), "~/notadir.txt/")
}

func TestCompleteDirsFiltersByWhatWasTyped(t *testing.T) {
	withFakeHome(t, "projects", "proto", "pictures")

	assert.Equal(t, []string{"~/projects/", "~/proto/"}, completeDirs("~/pro"))
	assert.Empty(t, completeDirs("~/zzz"))
}

func TestCompleteDirsWithoutSlashCompletesInsideHome(t *testing.T) {
	withFakeHome(t, "projects")

	assert.Equal(t, []string{"~/projects/"}, completeDirs("pro"),
		"a bare fragment is completed from home, not from the launch directory")
}

func TestCompleteDirsShowsHiddenOnlyWhenAsked(t *testing.T) {
	withFakeHome(t, ".config", "config")

	assert.Equal(t, []string{"~/.config/"}, completeDirs("~/."))
}

func TestCompleteDirsDescendsIntoSubdirectories(t *testing.T) {
	withFakeHome(t, "projects/alpha", "projects/beta")

	assert.Equal(t, []string{"~/projects/alpha/", "~/projects/beta/"}, completeDirs("~/projects/"))
	assert.Equal(t, []string{"~/projects/alpha/"}, completeDirs("~/projects/a"))
}

func TestCompletePathFillsTheSharedPartThenCycles(t *testing.T) {
	candidates := []string{"~/projects/", "~/proto/"}

	// First press fills in everything the candidates agree on.
	got, next, cycled := completePath("~/p", candidates, 0)
	assert.Equal(t, "~/pro", got)
	assert.False(t, cycled)
	assert.Equal(t, 0, next)

	// With nothing left to agree on, presses cycle through the candidates.
	got, next, cycled = completePath(got, candidates, next)
	assert.Equal(t, "~/projects/", got)
	assert.True(t, cycled)

	got, next, cycled = completePath(got, candidates, next)
	assert.Equal(t, "~/proto/", got)
	assert.True(t, cycled)

	got, _, _ = completePath(got, candidates, next)
	assert.Equal(t, "~/projects/", got, "the cycle wraps around")
}

func TestCompletePathWithSingleCandidateCompletesIt(t *testing.T) {
	got, _, cycled := completePath("~/pro", []string{"~/projects/"}, 0)
	assert.Equal(t, "~/projects/", got)
	assert.False(t, cycled)
}

func TestCompletePathWithNoCandidatesLeavesTheTextAlone(t *testing.T) {
	got, _, _ := completePath("~/zzz", nil, 0)
	assert.Equal(t, "~/zzz", got)
}

func TestDirectoryFieldStartsAtHomeAndCompletesOnTab(t *testing.T) {
	home := withFakeHome(t, "projects", "proto")

	h := newTestHome(t)
	startForm(t, h)

	assert.Equal(t, homePrefix, h.pathInput,
		"the directory field always starts at home, not at the launch directory")

	// Typing narrows the candidates.
	typeRunes(t, h, "pro")
	assert.Len(t, h.pathCandidates, 2)

	// Tab has nothing left to fill in here, so it offers the first candidate.
	press(h, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "~/projects/", h.pathInput)

	// Another Tab moves to the next one.
	press(h, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "~/proto/", h.pathInput)

	// Confirming resolves the shortcut into a real directory.
	inst := h.list.GetInstances()[h.list.NumInstances()-1]
	pressEnter(h)
	assert.Equal(t, filepath.Join(home, "proto"), inst.Path)
}

func TestTabFillsTheSharedPartWhenTypingIsAmbiguous(t *testing.T) {
	withFakeHome(t, "projects", "proto")

	h := newTestHome(t)
	startForm(t, h)

	typeRunes(t, h, "p")
	press(h, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "~/pro", h.pathInput, "tab fills in as far as the candidates agree")
}

func TestBackspaceRefreshesTheCandidates(t *testing.T) {
	withFakeHome(t, "projects", "pictures")

	h := newTestHome(t)
	startForm(t, h)

	typeRunes(t, h, "pro")
	assert.Len(t, h.pathCandidates, 1)

	press(h, tea.KeyMsg{Type: tea.KeyBackspace})
	press(h, tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "~/p", h.pathInput)
	assert.Len(t, h.pathCandidates, 2)
}
