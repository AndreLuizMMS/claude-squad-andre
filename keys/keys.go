package keys

import (
	"github.com/charmbracelet/bubbles/key"
)

type KeyName int

const (
	KeyUp KeyName = iota
	KeyDown
	KeyEnter
	KeyNew
	KeyKill
	KeyQuit

	KeyTab        // Tab is a special keybinding for switching between panes.
	KeySubmitName // SubmitName is a special keybinding for submitting the name of a new instance.

	KeyPause
	KeyResume
	KeyPrompt // New key for entering a prompt
	KeyHelp   // Key for showing help screen

	// Diff keybindings
	KeyShiftUp
	KeyShiftDown

	// Reorder keybindings
	KeyMoveUp
	KeyMoveDown

	KeyOpenEditor // Open the session directory in the configured editor
	KeyRename     // Rename a session already in the list

	KeyNextAttention // Jump to the next session that handed the turn back
	KeySendPrompt    // Send a prompt to the selected session without attaching

	KeyViewMode // Switch between the list view and the mosaic of every session

	// KeyMouseSelect hands the mouse back to the terminal so text can be
	// selected and copied with it.
	KeyMouseSelect

	// KeyRenameAgent types the agent's own rename command into the session.
	KeyRenameAgent
)

// GlobalKeyStringsMap is a global, immutable map string to keybinding.
var GlobalKeyStringsMap = map[string]KeyName{
	"up":         KeyUp,
	"k":          KeyUp,
	"down":       KeyDown,
	"j":          KeyDown,
	"shift+up":   KeyShiftUp,
	"shift+down": KeyShiftDown,
	"J":          KeyMoveDown,
	"K":          KeyMoveUp,
	"N":          KeyPrompt,
	"enter":      KeyEnter,
	"o":          KeyEnter,
	"n":          KeyNew,
	"D":          KeyKill,
	"q":          KeyQuit,
	"tab":        KeyTab,
	"c":          KeyPause,
	"r":          KeyResume,
	"?":          KeyHelp,
	"ctrl+e":     KeyOpenEditor,
	"R":          KeyRename,
	" ":          KeyNextAttention,
	"p":          KeySendPrompt,
	"v":          KeyViewMode,
	"y":          KeyMouseSelect,
	"ctrl+r":     KeyRenameAgent,
}

// GlobalkeyBindings is a global, immutable map of KeyName tot keybinding.
var GlobalkeyBindings = map[KeyName]key.Binding{
	KeyUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "cima"),
	),
	KeyDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "baixo"),
	),
	KeyShiftUp: key.NewBinding(
		key.WithKeys("shift+up"),
		key.WithHelp("shift+↑", "rolar"),
	),
	KeyShiftDown: key.NewBinding(
		key.WithKeys("shift+down"),
		key.WithHelp("shift+↓", "rolar"),
	),
	KeyEnter: key.NewBinding(
		key.WithKeys("enter", "o"),
		key.WithHelp("↵/o", "entrar"),
	),
	KeyNew: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "nova"),
	),
	KeyKill: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "encerrar"),
	),
	KeyHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "ajuda"),
	),
	KeyQuit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "sair"),
	),
	KeyPrompt: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "nova com prompt"),
	),
	KeyPause: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "pausar"),
	),
	KeyTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "trocar aba"),
	),
	KeyResume: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "retomar"),
	),

	KeyMoveUp: key.NewBinding(
		key.WithKeys("K"),
		key.WithHelp("K", "mover p/ cima"),
	),
	KeyMoveDown: key.NewBinding(
		key.WithKeys("J"),
		key.WithHelp("J", "mover p/ baixo"),
	),

	KeyOpenEditor: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl-e", "abrir no editor"),
	),

	KeyRename: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "renomear"),
	),

	KeyNextAttention: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("espaço", "próxima que respondeu"),
	),

	KeySendPrompt: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "mandar prompt"),
	),

	KeyViewMode: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "mosaico"),
	),

	KeyMouseSelect: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "selecionar com o mouse"),
	),

	KeyRenameAgent: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl-r", "renomear no agente"),
	),

	// -- Special keybindings --

	KeySubmitName: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirmar nome"),
	),
}
