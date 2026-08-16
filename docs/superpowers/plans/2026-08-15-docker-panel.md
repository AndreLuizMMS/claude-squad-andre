# Painel de gerenciamento Docker por sessão — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a 4th tab/panel ("Docker") to every session, list view and mosaic alike, that tails `docker compose logs` for the session's working directory and lets the developer restart/stop/bring the stack up via a small action overlay — reusing the same `TerminalPane`/tmux infra the Bash and Cursor CLI tabs already run on.

**Architecture:** `TerminalPane` (already generic: one tmux session per instance, running whatever `program` string it was built with) gets a new optional `precheck` hook that, for the Docker pane only, checks for a compose file at the session root and routes straight into the pane's existing fallback-message rendering when there isn't one — no new rendering code. `TabbedWindow` and `Mosaic` each grow a 4th slot the same way their 2nd and 3rd slots already work. A small new overlay (same shape as `ConfirmationOverlay`) offers Logs/Restart/Stop/Up, executed by writing a real Ctrl-C byte plus the chosen command straight into the Docker pane's PTY.

**Tech Stack:** Go, bubbletea, lipgloss, tmux (via the existing `session/tmux` wrapper), testify/require.

**Spec:** `docs/superpowers/specs/2026-08-15-docker-panel-design.md`

## Global Constraints

- All user-facing strings (fallback messages, errors, overlay labels) are in **pt-BR**, matching every existing string in `ui/`, `app/`, `session/`.
- Comments follow this codebase's existing voice: short, explain *why* not *what*, no restating the code.
- Commits: pt-BR subject only, prefix `feat:`/`fix:`/`refactor:` (no scope), no body, no `Co-Authored-By` footer.
- No new dependencies — everything here is stdlib + packages already imported in the touched files.
- Detection of the compose file only checks the root of `Instance.Path` — no recursive search, no cache (see spec "Fora de escopo").

---

### Task 1: Compose file detection

**Files:**
- Create: `session/docker.go`
- Test: `session/docker_test.go`

**Interfaces:**
- Produces: `session.DetectComposeFile(path string) (string, bool)` — used by Task 2's `NewDockerPane`.

- [ ] **Step 1: Write the failing test**

Create `session/docker_test.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectComposeFile(t *testing.T) {
	names := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(""), 0644))

			path, ok := DetectComposeFile(dir)
			require.True(t, ok)
			require.Equal(t, filepath.Join(dir, name), path)
		})
	}

	t.Run("none present", func(t *testing.T) {
		dir := t.TempDir()
		_, ok := DetectComposeFile(dir)
		require.False(t, ok)
	})

	t.Run("only in subdirectory does not count", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sub")
		require.NoError(t, os.MkdirAll(sub, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sub, "docker-compose.yml"), []byte(""), 0644))

		_, ok := DetectComposeFile(dir)
		require.False(t, ok)
	})

	t.Run("priority order: docker-compose.yml wins over compose.yaml", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(""), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(""), 0644))

		path, ok := DetectComposeFile(dir)
		require.True(t, ok)
		require.Equal(t, filepath.Join(dir, "docker-compose.yml"), path)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/andreluiz/claude-squad-andre && go test ./session/... -run TestDetectComposeFile -v`
Expected: FAIL — `undefined: DetectComposeFile`

- [ ] **Step 3: Write minimal implementation**

Create `session/docker.go`:

```go
package session

import (
	"os"
	"path/filepath"
)

// composeFileNames are the docker compose manifest names looked for at a
// session's working directory, in the same priority order docker compose
// itself uses.
var composeFileNames = []string{
	"docker-compose.yml",
	"docker-compose.yaml",
	"compose.yml",
	"compose.yaml",
}

// DetectComposeFile reports whether a docker compose manifest sits at the
// root of path. No subdirectories are searched — a session's Docker tab is
// about the project at its own working directory, not whatever a monorepo
// might keep further down.
func DetectComposeFile(path string) (string, bool) {
	for _, name := range composeFileNames {
		full := filepath.Join(path, name)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/andreluiz/claude-squad-andre && go test ./session/... -run TestDetectComposeFile -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Commit**

```bash
cd /home/andreluiz/claude-squad-andre
git add session/docker.go session/docker_test.go
git commit -m "feat: detecta docker-compose na raiz do diretório da sessão"
```

---

### Task 2: Docker pane (TerminalPane precheck + NewDockerPane)

**Files:**
- Modify: `ui/terminal.go`
- Test: `ui/terminal_test.go`

**Interfaces:**
- Consumes: `session.DetectComposeFile(path string) (string, bool)` (Task 1).
- Produces: `ui.NewDockerPane() *TerminalPane` — used by Task 3 (`TabbedWindow`) and Task 4 (`Mosaic`).

- [ ] **Step 1: Write the failing tests**

Add to `ui/terminal_test.go` — first add `"os"` and `"path/filepath"` to the import block (it currently has `"claude-squad/cmd/cmd_test"`, `"claude-squad/log"`, `"claude-squad/session"`, `"claude-squad/session/tmux"`, `"fmt"`, `"os/exec"`, `"strings"`, `"testing"`, `"time"`, and testify/require):

```go
import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)
```

Then append these two tests to the end of the file:

```go
func TestDockerPaneFallsBackWithoutComposeFile(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	instance := makeStartedInstance(t, "docker-no-compose")
	defer func() { _ = instance.Kill() }()

	tp := NewDockerPane()
	tp.SetSize(80, 30)

	err := tp.UpdateContent(instance)
	require.NoError(t, err)

	tp.mu.Lock()
	defer tp.mu.Unlock()
	require.True(t, tp.fallback, "should fall back to a message when there is no compose file")
	require.Contains(t, tp.fallbackText, "Nenhum docker-compose encontrado")
}

func TestDockerPaneAcceptsComposeFile(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	instance := makeStartedInstance(t, "docker-with-compose")
	defer func() { _ = instance.Kill() }()
	require.NoError(t, os.WriteFile(
		filepath.Join(instance.Path, "docker-compose.yml"), []byte(""), 0644))

	tp := NewDockerPane()
	tp.SetSize(80, 30)

	err := tp.UpdateContent(instance)
	require.NoError(t, err)

	tp.mu.Lock()
	defer tp.mu.Unlock()
	require.NotContains(t, tp.fallbackText, "Nenhum docker-compose encontrado",
		"a compose file at the session root must clear the precheck (whatever happens next — e.g. no docker binary in PATH — is a different fallback message)")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/andreluiz/claude-squad-andre && go test ./ui/... -run TestDockerPane -v`
Expected: FAIL — `undefined: NewDockerPane`

- [ ] **Step 3: Write minimal implementation**

In `ui/terminal.go`, add the import `"claude-squad/session"` is already imported. Add a `precheck` field to the `TerminalPane` struct — replace:

```go
	prefix  string // tmux session name prefix, keeps panes from colliding
	program string // command run inside the session; empty means $SHELL

	isScrolling bool
	viewport    viewport.Model
}
```

with:

```go
	prefix  string // tmux session name prefix, keeps panes from colliding
	program string // command run inside the session; empty means $SHELL

	// precheck runs before a session is created for an instance; when it
	// reports skip, the pane goes straight to its fallback state instead of
	// starting a shell. Only the Docker pane sets this — Bash and Cursor
	// always start.
	precheck func(instance *session.Instance) (skip bool, message string)

	isScrolling bool
	viewport    viewport.Model
}
```

Add `NewDockerPane` right after `NewAgentPane`:

```go
// NewAgentPane creates a pane running the Cursor CLI (`agent`), with
// permission prompts skipped so the pane opens ready to work.
func NewAgentPane() *TerminalPane {
	return newTerminalPane("agent_", "agent --yolo")
}

// NewDockerPane creates a pane that tails a session's docker compose logs. A
// session with no compose file at the root of its directory has nothing to
// run docker compose against, so the pane shows a message instead of a shell
// — the same fallback rendering a paused or not-yet-started session already
// gets.
func NewDockerPane() *TerminalPane {
	tp := newTerminalPane("docker_", "docker compose logs -f --tail=200")
	tp.precheck = func(instance *session.Instance) (bool, string) {
		if _, ok := session.DetectComposeFile(instance.Path); !ok {
			return true, fmt.Sprintf("Nenhum docker-compose encontrado em %s.", instance.Path)
		}
		return false, ""
	}
	return tp
}
```

In `ensureSessionLocked`, insert the precheck call right after the cache-miss block and before the program is resolved — replace:

```go
		// Session died, remove stale entry and recreate below
		delete(t.sessions, instance.ID())
	}

	program := t.program
```

with:

```go
		// Session died, remove stale entry and recreate below
		delete(t.sessions, instance.ID())
	}

	if t.precheck != nil {
		if skip, message := t.precheck(instance); skip {
			t.setFallbackState(message)
			return nil
		}
	}

	program := t.program
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/andreluiz/claude-squad-andre && go test ./ui/... -run "TestDockerPane|TestTerminal" -v`
Expected: PASS — the new Docker tests and every pre-existing `TestTerminal*` test (the precheck is nil for `NewTerminalPane`/`NewAgentPane`, so their behavior is unchanged).

- [ ] **Step 5: Commit**

```bash
cd /home/andreluiz/claude-squad-andre
git add ui/terminal.go ui/terminal_test.go
git commit -m "feat: painel Docker reaproveita o TerminalPane com fallback sem compose"
```

---

### Task 3: 4th tab in TabbedWindow (list view)

**Files:**
- Modify: `ui/tabbed_window.go`
- Modify: `app/app.go:171` (constructor call — updated fully in Task 6, but the signature change here would break the build until then, so this task also patches that one line and `app/newinstance_test.go:33`)
- Test: Create `ui/tabbed_window_test.go`

**Interfaces:**
- Consumes: `ui.NewDockerPane() *TerminalPane` (Task 2).
- Produces: `ui.DockerTab` (int constant), `TabbedWindow.docker *TerminalPane` field, `NewTabbedWindow(preview *PreviewPane, terminal, agent, docker *TerminalPane) *TabbedWindow` (signature change), `(*TabbedWindow).SendDockerCommand(instance *session.Instance, command string) error` — used by Task 6.

- [ ] **Step 1: Write the failing test**

Create `ui/tabbed_window_test.go`:

```go
package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestTabbedWindow() *TabbedWindow {
	return NewTabbedWindow(NewPreviewPane(), NewTerminalPane(), NewAgentPane(), NewDockerPane())
}

func TestTabbedWindowHasFourTabsIncludingDocker(t *testing.T) {
	w := newTestTabbedWindow()
	require.Len(t, w.tabs, 4)
	require.Equal(t, "Docker", w.tabs[DockerTab])
}

func TestTabbedWindowToggleReachesDocker(t *testing.T) {
	w := newTestTabbedWindow()
	for i := 0; i < DockerTab; i++ {
		w.Toggle()
	}
	require.Equal(t, DockerTab, w.GetActiveTab())

	// One more toggle wraps back to the first tab.
	w.Toggle()
	require.Equal(t, PreviewTab, w.GetActiveTab())
}

func TestTabbedWindowActiveTerminalOnDockerTab(t *testing.T) {
	w := newTestTabbedWindow()
	w.activeTab = DockerTab
	require.Same(t, w.docker, w.activeTerminal())
	require.True(t, w.IsInTerminalTab())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/andreluiz/claude-squad-andre && go build ./... 2>&1 | head -30`
Expected: build FAILS — `NewTabbedWindow` called with 3 args in `app/app.go:171` and `app/newinstance_test.go:33` doesn't match the new 4-arg call in the test, and `DockerTab`/`w.docker` don't exist yet. (This task's implementation step fixes both the type and the two call sites in one pass — see Step 3.)

- [ ] **Step 3: Write minimal implementation**

In `ui/tabbed_window.go`, extend the tab constants — replace:

```go
const (
	PreviewTab int = iota
	AgentTab
	TerminalTab
)
```

with:

```go
const (
	PreviewTab int = iota
	AgentTab
	TerminalTab
	DockerTab
)
```

Add the `docker` field to the struct — replace:

```go
	preview  *PreviewPane
	terminal *TerminalPane
	agent    *TerminalPane
	instance *session.Instance
}
```

with:

```go
	preview  *PreviewPane
	terminal *TerminalPane
	agent    *TerminalPane
	docker   *TerminalPane
	instance *session.Instance
}
```

Update the constructor — replace:

```go
func NewTabbedWindow(preview *PreviewPane, terminal, agent *TerminalPane) *TabbedWindow {
	return &TabbedWindow{
		tabs: []string{
			"Claude Code",
			"Cursor CLI",
			"Bash $",
		},
		preview:  preview,
		terminal: terminal,
		agent:    agent,
	}
}
```

with:

```go
func NewTabbedWindow(preview *PreviewPane, terminal, agent, docker *TerminalPane) *TabbedWindow {
	return &TabbedWindow{
		tabs: []string{
			"Claude Code",
			"Cursor CLI",
			"Bash $",
			"Docker",
		},
		preview:  preview,
		terminal: terminal,
		agent:    agent,
		docker:   docker,
	}
}
```

Update `activeTerminal` — replace:

```go
func (w *TabbedWindow) activeTerminal() *TerminalPane {
	switch w.activeTab {
	case TerminalTab:
		return w.terminal
	case AgentTab:
		return w.agent
	}
	return nil
}
```

with:

```go
func (w *TabbedWindow) activeTerminal() *TerminalPane {
	switch w.activeTab {
	case TerminalTab:
		return w.terminal
	case AgentTab:
		return w.agent
	case DockerTab:
		return w.docker
	}
	return nil
}
```

Update `SetSize` — replace:

```go
	w.preview.SetSize(contentWidth, contentHeight)
	w.terminal.SetSize(contentWidth, contentHeight)
	w.agent.SetSize(contentWidth, contentHeight)
}
```

with:

```go
	w.preview.SetSize(contentWidth, contentHeight)
	w.terminal.SetSize(contentWidth, contentHeight)
	w.agent.SetSize(contentWidth, contentHeight)
	w.docker.SetSize(contentWidth, contentHeight)
}
```

Update `CleanupTerminal` and `CleanupTerminalForInstance` — replace:

```go
// CleanupTerminal closes the terminal and Cursor sessions
func (w *TabbedWindow) CleanupTerminal() {
	w.terminal.Close()
	w.agent.Close()
}

// CleanupTerminalForInstance closes the cached shell sessions for the given instance title.
func (w *TabbedWindow) CleanupTerminalForInstance(title string) {
	w.terminal.CloseForInstance(title)
	w.agent.CloseForInstance(title)
}
```

with:

```go
// CleanupTerminal closes the terminal, Cursor and Docker sessions
func (w *TabbedWindow) CleanupTerminal() {
	w.terminal.Close()
	w.agent.Close()
	w.docker.Close()
}

// CleanupTerminalForInstance closes the cached shell sessions for the given instance title.
func (w *TabbedWindow) CleanupTerminalForInstance(title string) {
	w.terminal.CloseForInstance(title)
	w.agent.CloseForInstance(title)
	w.docker.CloseForInstance(title)
}
```

Add `SendDockerCommand` right after `SendPromptToAgent`:

```go
// SendPromptToAgent types a prompt into the Cursor CLI pane and taps enter —
// used to ask it to rename its own chat.
func (w *TabbedWindow) SendPromptToAgent(instance *session.Instance, prompt string) error {
	return w.agent.SendPromptToInstance(instance, prompt)
}

// SendDockerCommand interrupts whatever the Docker tab's pane is running —
// almost always the log tail — and runs a new docker compose command in its
// place. The interrupt is a real Ctrl-C byte (0x03), not the two characters
// "C-c": SendKeysToInstance writes straight into the pane's PTY, it does not
// go through tmux's own key-name parser.
func (w *TabbedWindow) SendDockerCommand(instance *session.Instance, command string) error {
	if err := w.docker.SendKeysToInstance(instance, "\x03"); err != nil {
		return err
	}
	return w.docker.SendPromptToInstance(instance, command)
}
```

Now fix the two call sites so the build compiles again. In `app/app.go`, replace:

```go
	tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), terminalPane, agentPane),
```

with:

```go
	tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), terminalPane, agentPane, ui.NewDockerPane()),
```

In `app/newinstance_test.go:33`, replace:

```go
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewTerminalPane(), ui.NewAgentPane())
```

with:

```go
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewTerminalPane(), ui.NewAgentPane(), ui.NewDockerPane())
```

(This is a temporary one-off Docker pane for `app.go`'s construction — Task 6 replaces it with the shared `dockerPane` variable also passed to `NewMosaic`. Left as-is here, the build stays green after this task; Task 6 tidies it into the shared-pane pattern.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/andreluiz/claude-squad-andre && go build ./... && go test ./ui/... ./app/... -v 2>&1 | tail -60`
Expected: build succeeds, all tests PASS including the 3 new `TestTabbedWindow*` tests.

- [ ] **Step 5: Commit**

```bash
cd /home/andreluiz/claude-squad-andre
git add ui/tabbed_window.go ui/tabbed_window_test.go app/app.go app/newinstance_test.go
git commit -m "feat: aba Docker na lista de sessões"
```

---

### Task 4: 4th panel in Mosaic

**Files:**
- Modify: `ui/mosaic.go`
- Modify: `ui/mosaic_test.go` (5 existing `NewMosaic(nil, nil)` call sites need a 3rd `nil`)
- Modify: `app/app.go:172` (constructor call, same temporary-shared-var caveat as Task 3 — Task 6 finishes the wiring)

**Interfaces:**
- Consumes: `ui.NewDockerPane() *TerminalPane` (Task 2), `ui.DockerTab` (Task 3).
- Produces: `NewMosaic(terminal, agent, docker *TerminalPane) *Mosaic` (signature change) — used by Task 6.

- [ ] **Step 1: Write the failing test**

Add to `ui/mosaic_test.go` (anywhere among the other `Test...` functions):

```go
func TestMosaicPanelNamesIncludesDocker(t *testing.T) {
	require.Len(t, mosaicPanelNames, 4)
	require.Equal(t, "Docker", mosaicPanelNames[DockerTab])
}

func TestMosaicPaneOfReturnsDockerPane(t *testing.T) {
	docker := NewDockerPane()
	m := NewMosaic(nil, nil, docker)

	instance, err := session.NewInstance(session.InstanceOptions{
		Title: "mosaic-docker", Path: t.TempDir(), Program: "bash",
	})
	require.NoError(t, err)

	m.panel[instance.ID()] = DockerTab
	require.Same(t, docker, m.paneOf(instance))
}
```

Check the top of `ui/mosaic_test.go` for its existing imports (`require` from testify and `claude-squad/session` are almost certainly already imported, since the file already builds instances for other tests) — add whichever of the two is missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/andreluiz/claude-squad-andre && go vet ./ui/... 2>&1 | head -20`
Expected: FAILS to compile — `undefined: DockerTab` and `not enough arguments in call to NewMosaic` (both the new test and the 5 pre-existing `NewMosaic(nil, nil)` call sites, which now need a 3rd argument).

- [ ] **Step 3: Write minimal implementation**

In `ui/mosaic.go`, update the panel names — replace:

```go
// mosaicPanelNames are the panels a cell can show, indexed by the same tab
// constants the list view uses, so a session reads the same in both modes.
var mosaicPanelNames = []string{"Claude Code", "Cursor CLI", "Bash $"}
```

with:

```go
// mosaicPanelNames are the panels a cell can show, indexed by the same tab
// constants the list view uses, so a session reads the same in both modes.
var mosaicPanelNames = []string{"Claude Code", "Cursor CLI", "Bash $", "Docker"}
```

Update the doc comment and field of the `Mosaic` struct — replace:

```go
	// panel is which of the three terminals each session is showing. Sessions
	// not in the map are on the agent, which is what the mosaic is for.
	panel map[string]int
```

with:

```go
	// panel is which of the four terminals each session is showing. Sessions
	// not in the map are on the agent, which is what the mosaic is for.
	panel map[string]int
```

and replace:

```go
	// terminal and agent are the same panes the list view uses, so a shell
	// opened in one view is the same shell in the other.
	terminal, agent *TerminalPane
}

func NewMosaic(terminal, agent *TerminalPane) *Mosaic {
	return &Mosaic{
		content:  make(map[string]string),
		panel:    make(map[string]int),
		scroll:   make(map[string]int),
		terminal: terminal,
		agent:    agent,
	}
}
```

with:

```go
	// terminal, agent and docker are the same panes the list view uses, so a
	// shell or the Docker tab opened in one view is the same one in the other.
	terminal, agent, docker *TerminalPane
}

func NewMosaic(terminal, agent, docker *TerminalPane) *Mosaic {
	return &Mosaic{
		content:  make(map[string]string),
		panel:    make(map[string]int),
		scroll:   make(map[string]int),
		terminal: terminal,
		agent:    agent,
		docker:   docker,
	}
}
```

Update `paneOf` — replace:

```go
// paneOf returns the shell pane backing a cell, or nil when the cell is showing
// the session's own agent.
func (m *Mosaic) paneOf(instance *session.Instance) *TerminalPane {
	switch m.panelOf(instance) {
	case AgentTab:
		return m.agent
	case TerminalTab:
		return m.terminal
	}
	return nil
}
```

with:

```go
// paneOf returns the shell pane backing a cell, or nil when the cell is showing
// the session's own agent.
func (m *Mosaic) paneOf(instance *session.Instance) *TerminalPane {
	switch m.panelOf(instance) {
	case AgentTab:
		return m.agent
	case TerminalTab:
		return m.terminal
	case DockerTab:
		return m.docker
	}
	return nil
}
```

Update the `CyclePanel` doc comment (now four panels, not three) — replace:

```go
// CyclePanel moves one cell to the next panel — agent, Cursor, shell — the same
// order the tabs follow in the list view.
```

with:

```go
// CyclePanel moves one cell to the next panel — agent, Cursor, shell, Docker —
// the same order the tabs follow in the list view.
```

Now fix the 5 pre-existing calls in `ui/mosaic_test.go`: every occurrence of `NewMosaic(nil, nil)` becomes `NewMosaic(nil, nil, nil)` (lines 284, 317, 359, 395, 515 as of this writing — search for the literal string, there are exactly 5).

Finally, update `app/app.go:172` so the build compiles — replace:

```go
		mosaic:       ui.NewMosaic(terminalPane, agentPane),
```

with:

```go
		mosaic:       ui.NewMosaic(terminalPane, agentPane, ui.NewDockerPane()),
```

(Same temporary caveat as Task 3: this creates a second, throwaway Docker pane distinct from the list view's. Task 6 replaces both call sites with one shared `dockerPane` variable, exactly like `terminalPane`/`agentPane` already are shared.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/andreluiz/claude-squad-andre && go build ./... && go test ./ui/... ./app/... -v 2>&1 | tail -60`
Expected: build succeeds, all tests PASS including the 2 new `TestMosaic*` tests.

- [ ] **Step 5: Commit**

```bash
cd /home/andreluiz/claude-squad-andre
git add ui/mosaic.go ui/mosaic_test.go app/app.go
git commit -m "feat: painel Docker no mosaico"
```

---

### Task 5: Docker action overlay

**Files:**
- Create: `ui/overlay/dockerActionOverlay.go`
- Test: Create `ui/overlay/dockerActionOverlay_test.go`

**Interfaces:**
- Produces: `overlay.NewDockerActionOverlay() *DockerActionOverlay`, `(*DockerActionOverlay).HandleKeyPress(msg tea.KeyMsg) bool`, `(*DockerActionOverlay).Render(opts ...WhitespaceOption) string`, `DockerActionOverlay.Command string` field — used by Task 6.

- [ ] **Step 1: Write the failing test**

Create `ui/overlay/dockerActionOverlay_test.go`:

```go
package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestDockerActionOverlayPicksCommand(t *testing.T) {
	tests := []struct {
		key     string
		command string
	}{
		{"l", "docker compose logs -f --tail=200"},
		{"r", "docker compose restart"},
		{"x", "docker compose stop"},
		{"u", "docker compose up -d"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			d := NewDockerActionOverlay()
			shouldClose := d.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			require.True(t, shouldClose)
			require.Equal(t, tt.command, d.Command)
		})
	}
}

func TestDockerActionOverlayEscCancelsWithoutCommand(t *testing.T) {
	d := NewDockerActionOverlay()
	shouldClose := d.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, shouldClose)
	require.Empty(t, d.Command)
}

func TestDockerActionOverlayIgnoresUnknownKeys(t *testing.T) {
	d := NewDockerActionOverlay()
	shouldClose := d.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	require.False(t, shouldClose)
	require.Empty(t, d.Command)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/andreluiz/claude-squad-andre && go test ./ui/overlay/... -run TestDockerActionOverlay -v`
Expected: FAIL — `undefined: NewDockerActionOverlay`

- [ ] **Step 3: Write minimal implementation**

Create `ui/overlay/dockerActionOverlay.go`:

```go
package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dockerActions is a shell command run into the Docker pane's tmux session
// per key, in the order they are listed to the developer. logsAction is what
// the pane goes back to after restart/stop/up, so the developer can watch the
// result without picking Logs again by hand — kept here as the source of
// truth both HandleKeyPress and Render read from.
var dockerActions = []struct {
	key, label, command string
}{
	{"l", "Logs", "docker compose logs -f --tail=200"},
	{"r", "Restart", "docker compose restart"},
	{"x", "Stop", "docker compose stop"},
	{"u", "Up", "docker compose up -d"},
}

// DockerActionOverlay is the menu the Docker tab opens on 'a': one key per
// docker compose action, no navigation — the same directness
// ConfirmationOverlay uses for y/n.
type DockerActionOverlay struct {
	width int
	// Command is the shell command for the action the developer picked, set
	// by HandleKeyPress. Empty when the overlay was cancelled instead.
	Command string
}

func NewDockerActionOverlay() *DockerActionOverlay {
	return &DockerActionOverlay{width: 40}
}

// HandleKeyPress processes a key press and reports whether the overlay
// should close — true on esc and on every action key, false on anything
// else (the overlay stays open on a key it doesn't recognize).
func (d *DockerActionOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	if msg.String() == "esc" {
		return true
	}
	for _, a := range dockerActions {
		if msg.String() == a.key {
			d.Command = a.command
			return true
		}
	}
	return false
}

// Render draws the action menu: one line per key, styled the same purple
// border TextInputOverlay and the profile picker already use for a modal
// that isn't a yes/no confirmation.
func (d *DockerActionOverlay) Render(opts ...WhitespaceOption) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(d.width)

	keyStyle := lipgloss.NewStyle().Bold(true)
	lines := []string{"Docker", ""}
	for _, a := range dockerActions {
		lines = append(lines, keyStyle.Render(a.key)+"  "+a.label)
	}
	lines = append(lines, "", keyStyle.Render("esc")+"  cancelar")

	return style.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/andreluiz/claude-squad-andre && go test ./ui/overlay/... -v`
Expected: PASS — all `TestDockerActionOverlay*` tests.

- [ ] **Step 5: Commit**

```bash
cd /home/andreluiz/claude-squad-andre
git add ui/overlay/dockerActionOverlay.go ui/overlay/dockerActionOverlay_test.go
git commit -m "feat: menu de ações do painel Docker"
```

---

### Task 6: Wire the Docker pane, key and overlay into the coordinator

**Files:**
- Modify: `keys/keys.go`
- Modify: `app/app.go`

**Interfaces:**
- Consumes: `ui.NewDockerPane()` (Task 2), `ui.DockerTab`, `(*TabbedWindow).SendDockerCommand` (Task 3), `NewMosaic(terminal, agent, docker *TerminalPane)` (Task 4), `overlay.NewDockerActionOverlay()`, `(*DockerActionOverlay).HandleKeyPress`, `(*DockerActionOverlay).Render`, `DockerActionOverlay.Command` (Task 5).
- Produces: `keys.KeyDockerAction`, `(*home).runDockerAction(command string) tea.Cmd` — final integration, nothing downstream depends on these.

This task has no new automated test of its own — the behavior (open the overlay, run a docker compose command) depends on a real docker binary and tmux session, which is exactly what Tasks 2–5's mocked tests already exercise piece by piece. Verification here is a manual smoke check (Step 4) plus the full existing suite staying green.

- [ ] **Step 1: Add the keybinding**

In `keys/keys.go`, add `KeyDockerAction` to the const block — replace:

```go
	// KeyRenameAgent copies the name the agent gave its own conversation onto
	// the session.
	KeyRenameAgent
)
```

with:

```go
	// KeyRenameAgent copies the name the agent gave its own conversation onto
	// the session.
	KeyRenameAgent

	// KeyDockerAction opens the action menu of the Docker tab (logs, restart,
	// stop, up).
	KeyDockerAction
)
```

Add `"a"` to `GlobalKeyStringsMap` — replace:

```go
	"ctrl+r":     KeyRenameAgent,
}
```

with:

```go
	"ctrl+r":     KeyRenameAgent,
	"a":          KeyDockerAction,
}
```

Add the help binding to `GlobalkeyBindings` — replace:

```go
	KeyRenameAgent: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl-r", "copiar o nome do agente"),
	),

	// -- Special keybindings --
```

with:

```go
	KeyRenameAgent: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl-r", "copiar o nome do agente"),
	),

	KeyDockerAction: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "ação docker"),
	),

	// -- Special keybindings --
```

- [ ] **Step 2: Run the existing suite to confirm the new key alone breaks nothing**

Run: `cd /home/andreluiz/claude-squad-andre && go build ./... && go test ./... 2>&1 | tail -40`
Expected: PASS (the key exists but nothing reads `KeyDockerAction` yet, so this is a no-op change in behavior).

- [ ] **Step 3: Wire the pane, the state and the overlay into `app/app.go`**

Share one Docker pane between the list view and the mosaic, same as `terminalPane`/`agentPane` already are — replace:

```go
	// The shell panes are shared by both views: a Bash opened in a mosaic cell is
	// the same Bash the list view's tab shows, not a second one.
	terminalPane, agentPane := ui.NewTerminalPane(), ui.NewAgentPane()

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), terminalPane, agentPane, ui.NewDockerPane()),
		mosaic:       ui.NewMosaic(terminalPane, agentPane, ui.NewDockerPane()),
```

with:

```go
	// The shell panes are shared by both views: a Bash opened in a mosaic cell is
	// the same Bash the list view's tab shows, not a second one — same for Docker.
	terminalPane, agentPane, dockerPane := ui.NewTerminalPane(), ui.NewAgentPane(), ui.NewDockerPane()

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), terminalPane, agentPane, dockerPane),
		mosaic:       ui.NewMosaic(terminalPane, agentPane, dockerPane),
```

Add the new state constant — replace:

```go
	// stateRename is the state when an existing session is being renamed.
	stateRename
)
```

with:

```go
	// stateRename is the state when an existing session is being renamed.
	stateRename
	// stateDockerAction is the state when the Docker tab's action menu is displayed.
	stateDockerAction
)
```

Add the overlay field to `home` — replace:

```go
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay
}
```

with:

```go
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay
	// dockerActionOverlay displays the Docker tab's action menu
	dockerActionOverlay *overlay.DockerActionOverlay
}
```

Add `stateDockerAction` to the text-entry-state guard in `handleMenuHighlighting` — the overlay reads single letters (`l`/`r`/`x`/`u`) that double as global shortcuts (`r` is Resume), exactly the reason `stateConfirm` is already in this list for `y`/`n` (`n` is New). Replace:

```go
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm ||
		m.state == stateRename || m.state == stateNew {
		return nil, false
	}
```

with:

```go
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm ||
		m.state == stateRename || m.state == stateNew || m.state == stateDockerAction {
		return nil, false
	}
```

Add the `stateDockerAction` handling block in `handleKeyPress`, right after the `stateConfirm` block — replace:

```go
	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.state = stateDefault
			m.confirmationOverlay = nil
			return m, nil
		}
		return m, nil
	}
```

with:

```go
	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.state = stateDefault
			m.confirmationOverlay = nil
			return m, nil
		}
		return m, nil
	}

	// Handle the Docker tab's action menu
	if m.state == stateDockerAction {
		shouldClose := m.dockerActionOverlay.HandleKeyPress(msg)
		if shouldClose {
			command := m.dockerActionOverlay.Command
			m.state = stateDefault
			m.dockerActionOverlay = nil
			if command != "" {
				return m, m.runDockerAction(command)
			}
			return m, nil
		}
		return m, nil
	}
```

Add the `keys.KeyDockerAction` case to the big switch in `handleKeyPress`, right after the `keys.KeyRenameAgent` case (it ends with `return m, nil` before `case keys.KeyHelp:`) — replace:

```go
		if err := selected.RequestAgentTitle(); err != nil {
			return m, m.handleError(err)
		}
		return m, nil
	case keys.KeyHelp:
```

with:

```go
		if err := selected.RequestAgentTitle(); err != nil {
			return m, m.handleError(err)
		}
		return m, nil
	case keys.KeyDockerAction:
		// The action menu only means something in the list view, on the
		// Docker tab itself — the mosaic keeps Docker as a read-only glance.
		if m.viewMode == viewList && m.tabbedWindow.GetActiveTab() == ui.DockerTab {
			m.state = stateDockerAction
			m.dockerActionOverlay = overlay.NewDockerActionOverlay()
		}
		return m, nil
	case keys.KeyHelp:
```

Add the `runDockerAction` method, right after `confirmAction` (before `func (m *home) View() string {`):

```go
// runDockerAction sends a docker compose command to the Docker tab's pane,
// interrupting whatever it was running first — almost always the log tail.
func (m *home) runDockerAction(command string) tea.Cmd {
	selected := m.list.GetSelectedInstance()
	if selected == nil {
		return nil
	}
	return func() tea.Msg {
		if err := m.tabbedWindow.SendDockerCommand(selected, command); err != nil {
			return err
		}
		return instanceChangedMsg{}
	}
}
```

Add the render branch in `View()` — replace:

```go
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	} else if m.state == stateNew && m.viewMode == viewMosaic {
```

with:

```go
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	} else if m.state == stateDockerAction {
		if m.dockerActionOverlay == nil {
			log.ErrorLog.Printf("docker action overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.dockerActionOverlay.Render(), mainView, true, true)
	} else if m.state == stateNew && m.viewMode == viewMosaic {
```

- [ ] **Step 4: Build, run the full suite, and smoke-test manually**

Run: `cd /home/andreluiz/claude-squad-andre && go build ./... && go vet ./... && go test ./... 2>&1 | tail -60`
Expected: build succeeds, `go vet` clean, all tests PASS.

Manual smoke check (needs a terminal, not scriptable): `go run . ` from a directory that has a `docker-compose.yml`, `tab` to the Docker tab, confirm the log tail renders (or the PATH-not-found fallback if docker isn't installed on this machine), press `a`, confirm the menu opens with `l`/`r`/`x`/`u`/`esc`, press `esc` to confirm it closes without running anything. Then repeat from a directory with no compose file and confirm the tab shows "Nenhum docker-compose encontrado em ...".

- [ ] **Step 5: Commit**

```bash
cd /home/andreluiz/claude-squad-andre
git add keys/keys.go app/app.go
git commit -m "feat: liga o painel Docker, a tecla de ação e o menu no app"
```

---

## Deviations from the spec worth flagging to the reviewer

- The spec described the Docker tab's "no compose file" state as custom static text rendered by `TabbedWindow`. The plan instead reuses `TerminalPane`'s existing fallback-message mechanism (the same one that already renders "sessão pausada" / "sessão ainda não foi iniciada") via a small `precheck` hook — same user-visible message, far less new code, and it also covers the mosaic's Docker cell for free (no separate mosaic-side "no compose" message was in the spec either).
- The spec said the action key should be a no-op outside the Docker tab **and** without a compose file. The plan's key handler still gates on "Docker tab active" (mosaic vs. list, and which tab) but does not separately check for a compose file before opening the menu — if the developer opens the menu without a compose file, picking an action fails with this app's existing "o painel da sessão '...' ainda não subiu" error (already shown via the standard error box), rather than the menu silently never opening. Same outcome (nothing runs), more informative failure, no extra plumbing.
- Menu-bar (`ui/menu.go`) discoverability of the `a` key was left out — it's cosmetic, not in the spec, and the menu's option-group layout uses fixed index ranges that a careless addition could visually misalign. Flagging instead of guessing.
