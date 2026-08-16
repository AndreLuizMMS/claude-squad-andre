package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type helpText interface {
	// toContent returns the help UI content.
	toContent() string
	// mask returns the bit mask for this help text. These are used to track which help screens
	// have been seen in the config and app state.
	mask() uint32
}

type helpTypeGeneral struct{}

type helpTypeInstanceStart struct {
	instance *session.Instance
}

type helpTypeInstanceAttach struct{}

func helpStart(instance *session.Instance) helpText {
	return helpTypeInstanceStart{instance: instance}
}

func (h helpTypeGeneral) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Claude Squad"),
		"",
		"Interface de terminal que gerencia várias sessões do Claude Code (e outros agentes locais), cada uma no seu diretório.",
		"",
		headerStyle.Render("Gerenciar:"),
		keyStyle.Render("n")+descStyle.Render("         - Criar uma nova sessão"),
		keyStyle.Render("N")+descStyle.Render("         - Criar uma nova sessão com prompt"),
		keyStyle.Render("D")+descStyle.Render("         - Encerrar (excluir) a sessão selecionada"),
		keyStyle.Render("↑/j, ↓/k")+descStyle.Render("  - Navegar entre as sessões"),
		keyStyle.Render("J/K")+descStyle.Render("       - Reordenar as sessões"),
		keyStyle.Render("↵/o")+descStyle.Render("       - Entrar na sessão selecionada"),
		keyStyle.Render("ctrl-l")+descStyle.Render("    - Sair da sessão e voltar para a lista"),
		keyStyle.Render("espaço")+descStyle.Render("    - Pular para a próxima sessão que respondeu"),
		keyStyle.Render("p")+descStyle.Render("         - Mandar um prompt sem entrar na sessão"),
		keyStyle.Render("R")+descStyle.Render("         - Renomear a sessão aqui no Claude Squad"),
		keyStyle.Render("ctrl-r")+descStyle.Render("    - Copiar para a sessão o nome que o agente deu para a conversa"),
		keyStyle.Render("ctrl-e")+descStyle.Render("    - Abrir o diretório da sessão no editor"),
		"",
		headerStyle.Render("Mosaico (todas as sessões na tela):"),
		keyStyle.Render("v")+descStyle.Render("         - Alternar entre a lista e o mosaico"),
		keyStyle.Render("←↑↓→")+descStyle.Render("      - Andar pela grade na direção da seta"),
		keyStyle.Render("↵")+descStyle.Render("         - Digitar na célula em destaque, sem sair do mosaico"),
		keyStyle.Render("tab")+descStyle.Render("       - Trocar o painel da célula: Claude Code, Cursor CLI, Bash, Docker"),
		keyStyle.Render("shift-↑/↓")+descStyle.Render(" - Ler o histórico da célula em destaque; a roda do mouse faz o mesmo"),
		keyStyle.Render("esc")+descStyle.Render("       - Voltar a célula para a tela ao vivo"),
		keyStyle.Render("ctrl-l")+descStyle.Render("    - Devolver o teclado ao coordenador"),
		keyStyle.Render("o")+descStyle.Render("         - Abrir a sessão em destaque em tela cheia"),
		keyStyle.Render("D")+descStyle.Render("         - Encerrar a sessão em destaque, sem confirmação"),
		"",
		headerStyle.Render("Estado da sessão:"),
		keyStyle.Render("r")+descStyle.Render("         - Retomar uma sessão que caiu sozinha ou subir um agente que saiu"),
		"",
		headerStyle.Render("Marcadores da sessão (lista e mosaico):"),
		descStyle.Render("⬤ RESPONDEU - o agente devolveu a vez; leia quando puder"),
		descStyle.Render("⏵ APROVAR   - o agente parou numa pergunta e não anda até você responder"),
		descStyle.Render("✖           - o agente saiu sozinho; 'r' sobe de novo"),
		"",
		headerStyle.Render("Outros:"),
		keyStyle.Render("tab")+descStyle.Render("       - Alternar entre as abas Claude Code, Cursor CLI, Bash e Docker"),
		keyStyle.Render("shift-↓/↑")+descStyle.Render(" - Rolar a prévia ou o terminal"),
		keyStyle.Render("ctrl-space")+descStyle.Render("- Devolver o mouse ao terminal para selecionar e copiar texto; enquanto isso a roda não rola"),
		keyStyle.Render("ctrl-c")+descStyle.Render("    - Voltar do modo de seleção com o mouse para o modo normal"),
		keyStyle.Render("q")+descStyle.Render("         - Sair do aplicativo"),
	)
	return content
}

func (h helpTypeInstanceStart) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Sessão criada"),
		"",
		descStyle.Render("Nova sessão criada:"),
		descStyle.Render(fmt.Sprintf("• Diretório de trabalho: %s",
			lipgloss.NewStyle().Bold(true).Render(h.instance.Path))),
		descStyle.Render(fmt.Sprintf("• %s rodando em sessão tmux em segundo plano",
			lipgloss.NewStyle().Bold(true).Render(h.instance.Program))),
		"",
		descStyle.Render("O agente edita esse diretório diretamente. O versionamento fica por sua conta."),
		"",
		headerStyle.Render("Gerenciar:"),
		keyStyle.Render("↵/o")+descStyle.Render("   - Entrar na sessão para interagir diretamente"),
		keyStyle.Render("tab")+descStyle.Render("   - Alternar entre a prévia e o terminal da sessão"),
		keyStyle.Render("D")+descStyle.Render("     - Encerrar (excluir) a sessão selecionada"),
	)
	return content
}

func (h helpTypeInstanceAttach) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Entrando na sessão"),
		"",
		descStyle.Render("Para voltar para a lista de sessões, pressione ")+keyStyle.Render("ctrl-l"),
	)
	return content
}

func (h helpTypeGeneral) mask() uint32 {
	return 1
}

func (h helpTypeInstanceStart) mask() uint32 {
	return 1 << 1
}
func (h helpTypeInstanceAttach) mask() uint32 {
	return 1 << 2
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#7D56F4"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#36CFC9"))
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFCC00"))
	descStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
)

// showHelpScreen displays the help screen overlay if it hasn't been shown before
func (m *home) showHelpScreen(helpType helpText, onDismiss func()) (tea.Model, tea.Cmd) {
	// Get the flag for this help type
	var alwaysShow bool
	switch helpType.(type) {
	case helpTypeGeneral:
		alwaysShow = true
	}

	flag := helpType.mask()

	// Check if this help screen has been seen before
	// Only show if we're showing the general help screen or the corresponding flag is not set
	// in the seen bitmask.
	if alwaysShow || (m.appState.GetHelpScreensSeen()&flag) == 0 {
		// Mark this help screen as seen and save state
		if err := m.appState.SetHelpScreensSeen(m.appState.GetHelpScreensSeen() | flag); err != nil {
			log.WarningLog.Printf("Failed to save help screen state: %v", err)
		}

		content := helpType.toContent()

		m.textOverlay = overlay.NewTextOverlay(content)
		m.textOverlay.OnDismiss = onDismiss
		m.state = stateHelp
		return m, nil
	}

	// Skip displaying the help screen. The dismissal callback still runs, and it
	// still needs the redraw that dismissing the overlay would have produced:
	// attaching resizes the agent's terminal to the full window, so without a
	// resize on the way back the panes stay drawn at the wrong size.
	if onDismiss != nil {
		onDismiss()
		return m, tea.WindowSize()
	}
	return m, nil
}

// handleHelpState handles key events when in help state
func (m *home) handleHelpState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key press will close the help overlay
	shouldClose := m.textOverlay.HandleKeyPress(msg)
	if shouldClose {
		m.state = stateDefault
		return m, tea.Sequence(
			tea.WindowSize(),
			func() tea.Msg {
				m.menu.SetState(ui.StateDefault)
				return nil
			},
		)
	}

	return m, nil
}
