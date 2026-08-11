package app

import (
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Run is the main entrypoint into the application. It can be launched from
// anywhere: each session carries its own working directory.
func Run(ctx context.Context, program string, autoYes bool) error {
	p := tea.NewProgram(
		newHome(ctx, program, autoYes),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // Mouse scroll
	)
	_, err := p.Run()
	return err
}

type state int

const (
	stateDefault state = iota
	// stateNew is the state when the user is creating a new instance.
	stateNew
	// statePrompt is the state when the user is entering a prompt.
	statePrompt
	// stateHelp is the state when a help screen is displayed.
	stateHelp
	// stateConfirm is the state when a confirmation modal is displayed.
	stateConfirm
	// stateRename is the state when an existing session is being renamed.
	stateRename
)

// newInstanceField is which field of the new-session form has focus. The form
// runs inline in the list: title, then working directory.
type newInstanceField int

const (
	fieldTitle newInstanceField = iota
	fieldPath
)

type home struct {
	ctx context.Context

	// -- Storage and Configuration --

	program string
	autoYes bool

	// storage is the interface for saving/loading data to/from the app's state
	storage *session.Storage
	// appConfig stores persistent application configuration
	appConfig *config.Config
	// appState stores persistent application state like seen help screens
	appState config.AppState

	// -- State --

	// state is the current discrete state of the application
	state state
	// newInstanceFinalizer is called when the state is stateNew and then you press enter.
	// It registers the new instance in the list after the instance has been started.
	newInstanceFinalizer func()

	// promptAfterName tracks if we should enter prompt mode after naming
	promptAfterName bool

	// newField is which field of the new-session form is being edited.
	newField newInstanceField
	// pathInput is the raw text typed into the directory field.
	pathInput string
	// pathCandidates are the directory completions for the current pathInput,
	// recomputed whenever the text changes.
	pathCandidates []string
	// pathCycle is the candidate the next Tab press should offer.
	pathCycle int
	// loadErr holds a storage read failure so it can be surfaced once the UI is up.
	loadErr error

	// busyTicks counts, per session, how many consecutive observations saw the
	// agent working. The pane content flickers (spinners, cursors), so a single
	// observation means nothing — see applyMetadataResults.
	busyTicks map[string]int
	// armed marks sessions that worked long enough that finishing is worth
	// announcing.
	armed map[string]bool
	// idleTicks counts consecutive quiet observations per session — see
	// idleTicksToFinish.
	idleTicks map[string]int
	// metaTicks counts rounds of background observation, used to space out the
	// expensive diff reads — see diffEveryNTicks.
	metaTicks int
	// diffDirty asks the next observation round to read every diff, regardless of
	// the periodic schedule. Set when the selection moves: the newly selected
	// session needs the full diff content the pane renders, and waiting for the
	// next scheduled read would leave the pane blank.
	diffDirty bool

	// winWidth/winHeight are the last known dimensions of the whole terminal.
	// Attaching hands the agent the entire window, so we need them.
	winWidth, winHeight int

	// keySent is used to manage underlining menu items
	keySent bool

	// -- UI Components --

	// list displays the list of instances
	list *ui.List
	// menu displays the bottom menu
	menu *ui.Menu
	// tabbedWindow displays the tabbed window with preview and diff panes
	tabbedWindow *ui.TabbedWindow
	// errBox displays error messages
	errBox *ui.ErrBox
	// global spinner instance. we plumb this down to where it's needed
	spinner spinner.Model
	// textInputOverlay handles text input with state
	textInputOverlay *overlay.TextInputOverlay
	// textOverlay displays text information
	textOverlay *overlay.TextOverlay
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay
}

func newHome(ctx context.Context, program string, autoYes bool) *home {
	// Load application config
	appConfig := config.LoadConfig()

	// Load application state
	appState := config.LoadState()

	// Initialize storage
	storage, err := session.NewStorage(appState)
	if err != nil {
		fmt.Printf("Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:       ui.NewErrBox(),
		storage:      storage,
		appConfig:    appConfig,
		program:      program,
		autoYes:      autoYes,
		state:        stateDefault,
		appState:     appState,
		busyTicks:    make(map[string]int),
		armed:        make(map[string]bool),
	}
	h.list = ui.NewList(&h.spinner, autoYes)

	// Load saved instances. A record we cannot read must not stop the coordinator
	// from opening — whatever could be restored is listed, the stored file is kept
	// untouched, and the problem is reported once the UI is up.
	instances, err := storage.LoadInstances()
	if err != nil {
		log.ErrorLog.Printf("failed to load instances: %v", err)
		config.BackupStateFile()
		h.loadErr = err
	}

	// Add loaded instances to the list
	for _, instance := range instances {
		// Call the finalizer immediately.
		h.list.AddInstance(instance)()
		if autoYes {
			instance.AutoYes = true
		}
	}

	return h
}

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	m.winWidth, m.winHeight = msg.Width, msg.Height

	// List takes 30% of width, preview takes 70%
	listWidth := int(float32(msg.Width) * 0.3)
	tabsWidth := msg.Width - listWidth

	// Menu takes 10% of height, list and window take 90%
	contentHeight := int(float32(msg.Height) * 0.9)
	menuHeight := msg.Height - contentHeight - 1     // minus 1 for error box
	m.errBox.SetSize(int(float32(msg.Width)*0.9), 1) // error box takes 1 row

	m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	m.list.SetSize(listWidth, contentHeight)

	if m.textInputOverlay != nil {
		m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}
	if m.textOverlay != nil {
		m.textOverlay.SetWidth(int(float32(msg.Width) * 0.6))
	}

	previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
	if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
		log.ErrorLog.Print(err)
	}
	m.menu.SetSize(msg.Width, menuHeight)
}

func (m *home) Init() tea.Cmd {
	// Upon starting, we want to start the spinner. Whenever we get a spinner.TickMsg, we
	// update the spinner, which sends a new spinner.TickMsg. I think this lasts forever lol.
	cmds := []tea.Cmd{
		m.spinner.Tick,
		func() tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return previewTickMsg{}
		},
		tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance(), true),
	}
	if m.loadErr != nil {
		err := m.loadErr
		m.loadErr = nil
		cmds = append(cmds, func() tea.Msg { return err })
	}
	return tea.Batch(cmds...)
}

func (m *home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hideErrMsg:
		m.errBox.Clear()
	case previewTickMsg:
		cmd := m.instanceChanged()
		return m, tea.Batch(
			cmd,
			func() tea.Msg {
				time.Sleep(100 * time.Millisecond)
				return previewTickMsg{}
			},
		)
	case keyupMsg:
		m.menu.ClearKeydown()
		return m, nil
	case metadataUpdateDoneMsg:
		finished := m.applyMetadataResults(msg.results)
		m.metaTicks++
		next := tickUpdateMetadataCmd(
			m.snapshotActiveInstances(), m.list.GetSelectedInstance(), m.forceDiffRead())
		if finished && !m.appConfig.DisableBell {
			return m, tea.Batch(next, ringBell())
		}
		return m, next
	case tea.MouseMsg:
		// Handle mouse wheel events for scrolling the diff/preview pane
		if msg.Action == tea.MouseActionPress {
			if msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelUp {
				selected := m.list.GetSelectedInstance()
				if selected == nil || selected.Status == session.Paused {
					return m, nil
				}

				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.tabbedWindow.ScrollUp()
				case tea.MouseButtonWheelDown:
					m.tabbedWindow.ScrollDown()
				}
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.updateHandleWindowSizeEvent(msg)
		return m, nil
	case error:
		// Handle errors from confirmation actions
		return m, m.handleError(msg)
	case instanceChangedMsg:
		// Handle instance changed after confirmation action
		return m, m.instanceChanged()
	case instanceStartedMsg:
		// Select the instance that just started (or failed)
		m.list.SelectInstance(msg.instance)

		if msg.err != nil {
			m.list.Kill()
			return m, tea.Batch(m.handleError(msg.err), m.instanceChanged())
		}

		// Save after successful start
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}
		if m.autoYes {
			msg.instance.AutoYes = true
		}

		if msg.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = m.newPromptOverlay()
		} else {
			// If instance has a prompt (set from Shift+N flow), send it now
			if msg.instance.Prompt != "" {
				if err := msg.instance.SendPrompt(msg.instance.Prompt); err != nil {
					log.ErrorLog.Printf("failed to send prompt: %v", err)
				}
				msg.instance.Prompt = ""
			}
			m.menu.SetState(ui.StateDefault)
			m.showHelpScreen(helpStart(msg.instance), nil)
		}

		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *home) handleQuit() (tea.Model, tea.Cmd) {
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, m.handleError(err)
	}
	return m, tea.Quit
}

func (m *home) handleMenuHighlighting(msg tea.KeyMsg) (cmd tea.Cmd, returnEarly bool) {
	// Handle menu highlighting when you press a button. We intercept it here and immediately return to
	// update the ui while re-sending the keypress. Then, on the next call to this, we actually handle the keypress.
	if m.keySent {
		m.keySent = false
		return nil, false
	}
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm ||
		m.state == stateRename {
		return nil, false
	}
	// If it's in the global keymap, we should try to highlight it.
	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return nil, false
	}

	if m.list.GetSelectedInstance() != nil && m.list.GetSelectedInstance().Paused() && name == keys.KeyEnter {
		return nil, false
	}
	if name == keys.KeyShiftDown || name == keys.KeyShiftUp {
		return nil, false
	}

	// Skip the menu highlighting if the key is not in the map or we are using the shift up and down keys.
	// TODO: cleanup: when you press enter on stateNew, we use keys.KeySubmitName. We should unify the keymap.
	if name == keys.KeyEnter && m.state == stateNew {
		name = keys.KeySubmitName
	}
	m.keySent = true
	return tea.Batch(
		func() tea.Msg { return msg },
		m.keydownCallback(name)), true
}

// newBlankInstance builds the empty session the creation form fills in. The
// launch directory is only a suggested default; the mode follows the saved
// preference.
func (m *home) newBlankInstance() (*session.Instance, error) {
	return session.NewInstance(session.InstanceOptions{
		Title:   "",
		Path:    homePrefix,
		Program: m.program,
	})
}

// beginNewInstance puts the app into the inline creation form.
func (m *home) beginNewInstance(instance *session.Instance) {
	m.newInstanceFinalizer = m.list.AddInstance(instance)
	m.list.SetSelectedInstance(m.list.NumInstances() - 1)
	m.state = stateNew
	m.menu.SetState(ui.StateNewInstance)
	m.newField = fieldTitle
	m.setPathInput(homePrefix)
	m.list.SetNewInstanceHint(m.newInstanceHint(instance))
}

// setPathInput replaces the typed directory and refreshes its completions.
func (m *home) setPathInput(v string) {
	m.pathInput = v
	m.pathCandidates = completeDirs(v)
	m.pathCycle = 0
}

// cancelNewInstance drops the half-created session and returns to the list.
// Nothing was created yet at this point, so nothing needs undoing.
func (m *home) cancelNewInstance() tea.Cmd {
	m.list.Kill()
	m.list.SetNewInstanceHint("")
	m.newField = fieldTitle
	m.setPathInput("")
	m.promptAfterName = false
	m.state = stateDefault
	m.instanceChanged()
	return tea.Sequence(
		tea.WindowSize(),
		func() tea.Msg {
			m.menu.SetState(ui.StateDefault)
			return nil
		},
	)
}

// newInstanceHint is the second line shown under the session being created:
// which field has focus and what is in it.
func (m *home) newInstanceHint(instance *session.Instance) string {
	switch m.newField {
	case fieldPath:
		// Kept short on purpose: the list pane is narrow and the path itself is
		// what the developer needs to read.
		switch n := len(m.pathCandidates); {
		case n == 0:
			return fmt.Sprintf("dir: %s_ [sem correspondência]", m.pathInput)
		case n == 1:
			return fmt.Sprintf("dir: %s_ [tab]", m.pathInput)
		default:
			return fmt.Sprintf("dir: %s_ [tab %d]", m.pathInput, n)
		}
	default:
		return "nome  (enter para confirmar)"
	}
}

// handleNewInstanceField drives the working-directory field of the creation
// form. The directory is immutable once the session starts, so this is the only
// place it can be set.
func (m *home) handleNewInstanceField(msg tea.KeyMsg, instance *session.Instance) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Existence and writability are checked here so the developer stays on
		// the directory field, with the typed value preserved, if it is wrong.
		resolved, err := session.ResolvePath(m.pathInput)
		if err != nil {
			return m, m.handleError(err)
		}
		if err := session.ValidateWorkingDir(resolved); err != nil {
			return m, m.handleError(err)
		}
		if err := instance.SetPath(m.pathInput); err != nil {
			return m, m.handleError(err)
		}
		return m.startNewInstance(instance)
	case tea.KeyTab:
		completed, next, cycled := completePath(m.pathInput, m.pathCandidates, m.pathCycle)
		candidates := m.pathCandidates
		m.setPathInput(completed)
		if cycled {
			m.pathCandidates = candidates
		}
		m.pathCycle = next
	case tea.KeyRunes:
		m.setPathInput(m.pathInput + string(msg.Runes))
	case tea.KeySpace:
		m.setPathInput(m.pathInput + " ")
	case tea.KeyBackspace:
		runes := []rune(m.pathInput)
		if len(runes) > 0 {
			m.setPathInput(string(runes[:len(runes)-1]))
		}
	case tea.KeyEsc:
		return m, m.cancelNewInstance()
	}
	m.list.SetNewInstanceHint(m.newInstanceHint(instance))
	return m, nil
}

// startNewInstance finalizes the form: either hand off to the prompt overlay or
// start the agent right away.
func (m *home) startNewInstance(instance *session.Instance) (tea.Model, tea.Cmd) {
	m.newField = fieldTitle
	m.list.SetNewInstanceHint("")

	// If promptAfterName, show the prompt overlay before starting
	if m.promptAfterName {
		m.promptAfterName = false
		m.state = statePrompt
		m.menu.SetState(ui.StatePrompt)
		m.textInputOverlay = m.newPromptOverlay()
		return m, tea.WindowSize()
	}

	// Set Loading status and finalize into the list immediately
	instance.SetStatus(session.Loading)
	m.newInstanceFinalizer()
	m.state = stateDefault
	m.menu.SetState(ui.StateDefault)

	// Return a tea.Cmd that runs instance.Start in the background
	startCmd := func() tea.Msg {
		err := instance.Start(true)
		return instanceStartedMsg{
			instance:        instance,
			err:             err,
			promptAfterName: false,
		}
	}

	return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
}

func (m *home) handleKeyPress(msg tea.KeyMsg) (mod tea.Model, cmd tea.Cmd) {
	cmd, returnEarly := m.handleMenuHighlighting(msg)
	if returnEarly {
		return m, cmd
	}

	if m.state == stateHelp {
		return m.handleHelpState(msg)
	}

	if m.state == stateNew {
		// Handle quit commands first. Don't handle q because the user might want to type that.
		if msg.String() == "ctrl+c" {
			return m, m.cancelNewInstance()
		}

		instance := m.list.GetInstances()[m.list.NumInstances()-1]

		// The directory and mode fields have their own key handling.
		if m.newField == fieldPath {
			return m.handleNewInstanceField(msg, instance)
		}

		switch msg.Type {
		// Start the instance (enable previews etc) and go back to the main menu state.
		case tea.KeyEnter:
			if len(instance.Title) == 0 {
				return m, m.handleError(fmt.Errorf("title cannot be empty"))
			}
			// Move on to the working directory field, anchored at home.
			m.newField = fieldPath
			m.setPathInput(homePrefix)
			m.list.SetNewInstanceHint(m.newInstanceHint(instance))
			return m, nil
		case tea.KeyRunes:
			if runewidth.StringWidth(instance.Title) >= 32 {
				return m, m.handleError(fmt.Errorf("title cannot be longer than 32 characters"))
			}
			if err := instance.SetTitle(instance.Title + string(msg.Runes)); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyBackspace:
			runes := []rune(instance.Title)
			if len(runes) == 0 {
				return m, nil
			}
			if err := instance.SetTitle(string(runes[:len(runes)-1])); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeySpace:
			if err := instance.SetTitle(instance.Title + " "); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyEsc:
			return m, m.cancelNewInstance()
		default:
		}
		m.list.SetNewInstanceHint(m.newInstanceHint(instance))
		return m, nil
	} else if m.state == statePrompt {
		// Handle cancel via ctrl+c before delegating to the overlay
		if msg.String() == "ctrl+c" {
			return m, m.cancelPromptOverlay()
		}

		// Use the new TextInputOverlay component to handle all key events
		shouldClose := m.textInputOverlay.HandleKeyPress(msg)

		// Check if the form was submitted or canceled
		if shouldClose {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}

			if m.textInputOverlay.IsCanceled() {
				return m, m.cancelPromptOverlay()
			}

			if m.textInputOverlay.IsSubmitted() {
				prompt := m.textInputOverlay.GetValue()
				selectedProgram := m.textInputOverlay.GetSelectedProgram()

				if !selected.Started() {
					// Shift+N flow: instance not started yet — start, then send prompt
					if selectedProgram != "" {
						selected.Program = selectedProgram
					}
					selected.Prompt = prompt

					// Finalize into list and start
					selected.SetStatus(session.Loading)
					m.newInstanceFinalizer()
					m.textInputOverlay = nil
					m.state = stateDefault
					m.menu.SetState(ui.StateDefault)

					startCmd := func() tea.Msg {
						err := selected.Start(true)
						return instanceStartedMsg{
							instance:        selected,
							err:             err,
							promptAfterName: false,
						}
					}

					return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
				}

				// Regular flow: instance already running, just send prompt
				if err := selected.SendPrompt(prompt); err != nil {
					return m, m.handleError(err)
				}
			}

			// Close the overlay and reset state
			m.textInputOverlay = nil
			m.state = stateDefault
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					m.showHelpScreen(helpStart(selected), nil)
					return nil
				},
			)
		}

		return m, nil
	}

	if m.state == stateRename {
		return m.handleRenameState(msg)
	}

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

	// Exit scrolling mode when ESC is pressed and preview pane is in scrolling mode
	// Check if Escape key was pressed and we're not in the diff tab (meaning we're in preview tab)
	// Always check for escape key first to ensure it doesn't get intercepted elsewhere
	if msg.Type == tea.KeyEsc {
		// If in preview tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInPreviewTab() && m.tabbedWindow.IsPreviewInScrollMode() {
			// Use the selected instance from the list
			selected := m.list.GetSelectedInstance()
			err := m.tabbedWindow.ResetPreviewToNormalMode(selected)
			if err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		// If in terminal tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsTerminalInScrollMode() {
			m.tabbedWindow.ResetTerminalToNormalMode()
			return m, m.instanceChanged()
		}
	}

	// Handle quit commands first
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m.handleQuit()
	}

	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return m, nil
	}

	switch name {
	case keys.KeyHelp:
		return m.showHelpScreen(helpTypeGeneral{}, nil)
	case keys.KeyPrompt:
		if limit := m.appConfig.GetMaxSessions(); m.list.NumInstances() >= limit {
			return m, m.handleError(
				fmt.Errorf("limite de %d sessões atingido — ajuste max_sessions na configuração", limit))
		}

		instance, err := m.newBlankInstance()
		if err != nil {
			return m, m.handleError(err)
		}

		m.beginNewInstance(instance)
		m.promptAfterName = true
		return m, nil
	case keys.KeyNew:
		if limit := m.appConfig.GetMaxSessions(); m.list.NumInstances() >= limit {
			return m, m.handleError(
				fmt.Errorf("limite de %d sessões atingido — ajuste max_sessions na configuração", limit))
		}
		instance, err := m.newBlankInstance()
		if err != nil {
			return m, m.handleError(err)
		}

		m.beginNewInstance(instance)
		return m, nil
	case keys.KeyUp:
		m.list.Up()
		m.diffDirty = true
		return m, m.instanceChanged()
	case keys.KeyDown:
		m.list.Down()
		m.diffDirty = true
		return m, m.instanceChanged()
	case keys.KeyShiftUp:
		m.tabbedWindow.ScrollUp()
		return m, m.instanceChanged()
	case keys.KeyShiftDown:
		m.tabbedWindow.ScrollDown()
		return m, m.instanceChanged()
	case keys.KeyTab:
		m.tabbedWindow.Toggle()
		m.menu.SetActiveTab(m.tabbedWindow.GetActiveTab())
		return m, m.instanceChanged()
	case keys.KeyKill:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the kill action as a tea.Cmd
		killAction := func() tea.Msg {
			// Clean up terminal session for this instance
			m.tabbedWindow.CleanupTerminalForInstance(selected.ID())

			// Delete from storage first
			if err := m.storage.DeleteInstance(selected.ID()); err != nil {
				return err
			}

			// Then kill the instance
			m.list.Kill()
			return instanceChangedMsg{}
		}

		// Show confirmation modal
		message := fmt.Sprintf(
			"[!] Encerrar a sessão '%s'? Apenas o terminal é fechado; o diretório fica intacto.",
			selected.Title)
		return m, m.confirmAction(message, killAction)
	case keys.KeyPause:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		if selected.Orphaned() {
			return m, m.handleError(fmt.Errorf(
				"session '%s' lost its directory — kill it instead of pausing", selected.Title))
		}
		if selected.Paused() {
			return m, m.handleError(fmt.Errorf("session '%s' is already paused", selected.Title))
		}

		// Show help screen before pausing
		return m.showHelpScreen(helpTypeInstancePause{}, func() {
			if err := selected.Pause(); err != nil {
				m.handleError(err)
			}
			m.tabbedWindow.CleanupTerminalForInstance(selected.ID())
			m.instanceChanged()
		})
	case keys.KeyOpenEditor:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if selected.Orphaned() {
			return m, m.handleError(fmt.Errorf(
				"a sessão '%s' perdeu seu diretório", selected.Title))
		}
		editor := m.appConfig.GetEditorCommand()
		if _, err := exec.LookPath(editor[0]); err != nil {
			return m, m.handleError(fmt.Errorf(
				"editor '%s' não encontrado — ajuste editor_command na configuração", editor[0]))
		}
		cmd := exec.Command(editor[0], append(editor[1:], selected.Path)...)
		if err := cmd.Start(); err != nil {
			return m, m.handleError(fmt.Errorf("não foi possível abrir o editor: %w", err))
		}
		go func() { _ = cmd.Wait() }()
		return m, nil
	case keys.KeyRename:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		m.state = stateRename
		m.textInputOverlay = overlay.NewTextInputOverlay("Novo nome da sessão", selected.Title)
		return m, tea.WindowSize()
	case keys.KeyMoveUp:
		if m.list.MoveUp() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyMoveDown:
		if m.list.MoveDown() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyResume:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if !selected.Paused() {
			return m, m.handleError(fmt.Errorf("session '%s' is not paused", selected.Title))
		}
		if err := selected.Resume(); err != nil {
			return m, m.handleError(err)
		}
		return m, tea.WindowSize()
	case keys.KeyEnter:
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if selected.Paused() {
			return m, m.handleError(fmt.Errorf(
				"session '%s' is paused — resume it first", selected.Title))
		}
		if selected.Orphaned() {
			return m, m.handleError(fmt.Errorf(
				"session '%s' lost its directory — kill it", selected.Title))
		}
		if !selected.TmuxAlive() {
			return m, nil
		}
		// Terminal tab: attach to terminal session
		if m.tabbedWindow.IsInTerminalTab() {
			return m.showHelpScreen(helpTypeInstanceAttach{}, func() {
				ch, err := m.tabbedWindow.AttachTerminal()
				if err != nil {
					m.handleError(err)
					return
				}
				<-ch
				m.state = stateDefault
				m.restorePreviewSize()
			})
		}
		// Opening the session is the developer seeing the answer.
		selected.NeedsAttention = false

		// Show help screen before attaching
		return m.showHelpScreen(helpTypeInstanceAttach{}, func() {
			// Hand the agent the whole terminal. Its session is sized for the
			// preview pane while detached, and it would otherwise stay that
			// small — a window's worth of screen showing a pane's worth of agent.
			if m.winWidth > 0 && m.winHeight > 0 {
				if err := selected.SetPreviewSize(m.winWidth, m.winHeight); err != nil {
					log.ErrorLog.Print(err)
				}
			}

			ch, err := m.list.Attach()
			if err != nil {
				m.handleError(err)
				return
			}
			<-ch
			m.state = stateDefault
			// Attaching resized the agent's terminal to the whole window. Put it
			// back to the preview shape right here, rather than waiting for the
			// next resize event to notice.
			m.restorePreviewSize()
			m.instanceChanged()
		})
	default:
		return m, nil
	}
}

// handleRenameState drives the overlay that renames a session already in the
// list. Only the label changes: the terminal, the directory and the stored
// identity stay exactly as they are.
func (m *home) handleRenameState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, m.closeRenameOverlay()
	}
	if !m.textInputOverlay.HandleKeyPress(msg) {
		return m, nil
	}

	if m.textInputOverlay.IsCanceled() {
		return m, m.closeRenameOverlay()
	}

	name := strings.TrimSpace(m.textInputOverlay.GetValue())
	selected := m.list.GetSelectedInstance()
	if selected == nil {
		return m, m.closeRenameOverlay()
	}
	if err := selected.SetTitle(name); err != nil {
		return m, tea.Batch(m.closeRenameOverlay(), m.handleError(err))
	}
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, tea.Batch(m.closeRenameOverlay(), m.handleError(err))
	}
	return m, tea.Batch(m.closeRenameOverlay(), m.instanceChanged())
}

// closeRenameOverlay drops the rename overlay and returns to the list.
func (m *home) closeRenameOverlay() tea.Cmd {
	m.textInputOverlay = nil
	m.state = stateDefault
	return tea.Sequence(
		tea.WindowSize(),
		func() tea.Msg {
			m.menu.SetState(ui.StateDefault)
			return nil
		},
	)
}

// restorePreviewSize re-applies the preview dimensions to every session's
// terminal. Called after detaching, when the attached session was resized to the
// full window and would otherwise render at the wrong shape inside the pane.
func (m *home) restorePreviewSize() {
	previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
	if previewWidth <= 0 || previewHeight <= 0 {
		return
	}
	if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
		log.ErrorLog.Print(err)
	}
}

// busyTicksToArm is how many consecutive "working" observations a session needs
// before finishing counts as an answer. The status detector reports any change
// in the pane, so spinners and blinking cursors alone would otherwise look like
// an agent starting and stopping over and over.
const busyTicksToArm = 3

// idleTicksToFinish is how many consecutive quiet observations a session needs
// before the turn counts as over. At 500ms a round this waits 3s, which covers
// the pauses an agent takes mid-answer while it thinks or waits on a tool.
const idleTicksToFinish = 6

// applyMetadataResults folds a round of background observations into the list.
// It reports whether any agent finished a real piece of work and handed the
// turn back — the moment worth announcing.
func (m *home) applyMetadataResults(results []instanceMetaResult) (finished bool) {
	// A nil map here would panic in the main loop and take the whole coordinator
	// down over a notification, so make sure they exist.
	if m.busyTicks == nil {
		m.busyTicks = make(map[string]int)
	}
	if m.armed == nil {
		m.armed = make(map[string]bool)
	}
	if m.idleTicks == nil {
		m.idleTicks = make(map[string]int)
	}

	for _, r := range results {
		// Skip instances that were paused while metadata was being computed
		if r.instance == nil || r.instance.Status == session.Paused {
			continue
		}
		id := r.instance.ID()
		if r.dirMissing {
			r.instance.SetStatus(session.Orphaned)
			delete(m.busyTicks, id)
			delete(m.armed, id)
			delete(m.idleTicks, id)
			continue
		}
		if r.instance.Status == session.Orphaned {
			// The directory came back; let the normal status detection resume.
			r.instance.SetStatus(session.Ready)
		}

		if r.updated || r.busy {
			r.instance.SetStatus(session.Running)
			m.busyTicks[id]++
			m.idleTicks[id] = 0
			if m.busyTicks[id] >= busyTicksToArm {
				m.armed[id] = true
			}
			// Work restarted: whatever the developer had not seen is moot.
			r.instance.NeedsAttention = false
		} else {
			if r.hasPrompt {
				r.instance.TapEnter()
			} else {
				r.instance.SetStatus(session.Ready)
			}
			m.idleTicks[id]++
			// Announce once per stretch of work, never for a flicker, and only
			// after the session has been quiet long enough that the turn is
			// really over instead of the agent pausing mid-answer.
			if m.armed[id] && m.idleTicks[id] >= idleTicksToFinish {
				finished = true
				r.instance.NeedsAttention = true
				m.armed[id] = false
			}
			m.busyTicks[id] = 0
		}

		if !r.diffRead {
			continue
		}
		if r.diffStats != nil && r.diffStats.Error != nil {
			if !strings.Contains(r.diffStats.Error.Error(), "base commit SHA not set") {
				log.WarningLog.Printf("could not update diff stats: %v", r.diffStats.Error)
			}
			r.instance.SetDiffStats(nil)
		} else {
			r.instance.SetDiffStats(r.diffStats)
		}
		r.instance.SetUsageStats(r.usageStats)
	}
	// The 5-hour quota is account-wide, so it is read once per round rather
	// than per session.
	m.list.SetQuota(session.ReadQuota())
	return finished
}

// ringBell asks the terminal to make its notification sound. It goes to stderr
// so it never lands in the drawing the coordinator is doing on stdout.
func ringBell() tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(os.Stderr, "\a")
		return nil
	}
}

// instanceChanged updates the preview pane, menu, and diff pane based on the selected instance. It returns an error
// Cmd if there was any error.
func (m *home) instanceChanged() tea.Cmd {
	// selected may be nil
	selected := m.list.GetSelectedInstance()

	m.tabbedWindow.UpdateDiff(selected)
	m.tabbedWindow.SetInstance(selected)
	// Update menu with current instance
	m.menu.SetInstance(selected)

	// If there's no selected instance, we don't need to update the preview.
	if err := m.tabbedWindow.UpdatePreview(selected); err != nil {
		return m.handleError(err)
	}
	if err := m.tabbedWindow.UpdateTerminal(selected); err != nil {
		return m.handleError(err)
	}
	return nil
}

type keyupMsg struct{}

// keydownCallback clears the menu option highlighting after 500ms.
func (m *home) keydownCallback(name keys.KeyName) tea.Cmd {
	m.menu.Keydown(name)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}

		return keyupMsg{}
	}
}

// hideErrMsg implements tea.Msg and clears the error text from the screen.
type hideErrMsg struct{}

// previewTickMsg implements tea.Msg and triggers a preview update
type previewTickMsg struct{}

type instanceChangedMsg struct{}

type instanceStartedMsg struct {
	instance        *session.Instance
	err             error
	promptAfterName bool
}

// instanceMetaResult holds the results of a single instance's metadata update,
// computed in a background goroutine.
type instanceMetaResult struct {
	instance  *session.Instance
	updated   bool
	hasPrompt bool
	// busy is true while the agent itself reports a turn in flight.
	busy       bool
	diffStats  *git.DiffStats
	usageStats *session.UsageStats
	// dirMissing is true when the session's working directory no longer exists.
	dirMissing bool
	// diffRead is true when this round actually read the diff. When false, the
	// previous numbers stay on screen instead of being wiped by a read that
	// never happened.
	diffRead bool
}

// metadataUpdateDoneMsg is sent when the background metadata update completes.
type metadataUpdateDoneMsg struct {
	results []instanceMetaResult
}

// snapshotActiveInstances returns the currently active (started, not paused)
// instances. Called on the main thread so the filtering doesn't race with
// state mutations.
func (m *home) snapshotActiveInstances() []*session.Instance {
	var out []*session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Started() && !inst.Paused() {
			out = append(out, inst)
		}
	}
	return out
}

// tickUpdateMetadataCmd returns a self-chaining Cmd that sleeps 500ms, then performs
// expensive metadata I/O (tmux capture, git diff) in parallel background goroutines.
// Because it only re-schedules after completing, overlapping ticks are impossible.
// The active instances slice should be snapshotted on the main thread via
// snapshotActiveInstances() before being passed here.
//
// Only the selected instance gets a full diff (with Content); the rest get a
// lightweight numstat-only summary. This keeps per-instance memory bounded
// since the diff pane only ever renders the selected one.
//
// The diff is read only when the session's terminal changed or when forceDiff
// says the periodic refresh is due. Reading it every round means running git
// over the whole working directory twice a second per session, which costs
// real CPU on a large repository and tells us nothing new while the agent is
// idle.
func tickUpdateMetadataCmd(active []*session.Instance, selected *session.Instance, forceDiff bool) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)

		if len(active) == 0 {
			return metadataUpdateDoneMsg{}
		}

		results := make([]instanceMetaResult, len(active))
		var wg sync.WaitGroup
		for idx, inst := range active {
			wg.Add(1)
			go func(i int, instance *session.Instance) {
				defer wg.Done()
				r := &results[i]
				r.instance = instance
				// A session whose directory vanished is orphaned rather than left
				// spinning forever.
				if r.dirMissing = !instance.WorkingDirExists(); r.dirMissing {
					return
				}
				r.updated, r.hasPrompt, r.busy = instance.HasUpdated()
				if !r.updated && !forceDiff {
					return
				}
				r.diffRead = true
				if instance == selected {
					r.diffStats = instance.ComputeDiff()
				} else {
					r.diffStats = instance.ComputeDiffNumstat()
				}
				r.usageStats = instance.ComputeUsage()
			}(idx, inst)
		}
		wg.Wait()

		return metadataUpdateDoneMsg{results: results}
	}
}

// diffEveryNTicks is how many observation rounds pass between diff reads of an
// idle session. At 500ms a round, an untouched session is re-read every 2s.
const diffEveryNTicks = 4

// forceDiffRead reports whether this round should read the diff of every
// session, including the ones whose terminal did not change. Edits made outside
// the agent are invisible to the terminal watcher, so the counters would
// otherwise go stale.
func (m *home) forceDiffRead() bool {
	if m.diffDirty {
		m.diffDirty = false
		return true
	}
	return m.metaTicks%diffEveryNTicks == 0
}

// handleError handles all errors which get bubbled up to the app. sets the error message. We return a callback tea.Cmd that returns a hideErrMsg message
// which clears the error message after 3 seconds.
func (m *home) handleError(err error) tea.Cmd {
	log.ErrorLog.Printf("%v", err)
	m.errBox.SetError(err)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(3 * time.Second):
		}

		return hideErrMsg{}
	}
}

func (m *home) newPromptOverlay() *overlay.TextInputOverlay {
	return overlay.NewTextInputOverlayWithProfiles("Digite o prompt", "", m.appConfig.GetProfiles())
}

// cancelPromptOverlay cancels the prompt overlay, cleaning up unstarted instances.
func (m *home) cancelPromptOverlay() tea.Cmd {
	selected := m.list.GetSelectedInstance()
	if selected != nil && !selected.Started() {
		m.list.Kill()
	}
	m.textInputOverlay = nil
	m.state = stateDefault
	return tea.Sequence(
		tea.WindowSize(),
		func() tea.Msg {
			m.menu.SetState(ui.StateDefault)
			return nil
		},
	)
}

// confirmAction shows a confirmation modal and stores the action to execute on confirm
func (m *home) confirmAction(message string, action tea.Cmd) tea.Cmd {
	m.state = stateConfirm

	// Create and show the confirmation overlay using ConfirmationOverlay
	m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	// Set a fixed width for consistent appearance
	m.confirmationOverlay.SetWidth(50)

	// Set callbacks for confirmation and cancellation
	m.confirmationOverlay.OnConfirm = func() {
		m.state = stateDefault
		// Execute the action if it exists
		if action != nil {
			_ = action()
		}
	}

	m.confirmationOverlay.OnCancel = func() {
		m.state = stateDefault
	}

	return nil
}

func (m *home) View() string {
	listWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.list.String())
	previewWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.tabbedWindow.String())
	listAndPreview := lipgloss.JoinHorizontal(lipgloss.Top, listWithPadding, previewWithPadding)

	mainView := lipgloss.JoinVertical(
		lipgloss.Center,
		listAndPreview,
		m.menu.String(),
		m.errBox.String(),
	)

	if m.state == statePrompt || m.state == stateRename {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
	} else if m.state == stateHelp {
		if m.textOverlay == nil {
			log.ErrorLog.Printf("text overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	}

	return mainView
}
