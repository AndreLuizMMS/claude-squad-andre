# Painel de gerenciamento Docker por sessão

## Objetivo

Adicionar uma 4ª aba/painel ("Docker") à sessão do claude-squad, ao lado de
Claude Code / Cursor CLI / Bash, mostrando os logs do `docker compose` do
projeto daquela sessão e permitindo reiniciar, parar e subir o stack — sem
sair do claude-squad.

## Decisões

- **Escopo: por sessão.** Painel novo é a 4ª aba de `TabbedWindow` (list
  view) e o 4º painel do `Mosaic`, reaproveitando `Instance.Path` como raiz
  do projeto — mesmo padrão das abas existentes.
- **Granularidade: stack inteiro.** Ações agem no `docker compose` todo
  (não por container individual). Cobre o pedido original: ver logs,
  reiniciar, parar, subir.
- **Trigger de ação: menu overlay.** Tecla dedicada abre um overlay com as
  opções, não keybindings soltos.
- **Detecção do compose file: só raiz do `Instance.Path`.** Sem busca
  recursiva. Sessão sem compose file → aba existe mas fica num estado
  vazio, sem subir tmux.

## Componentes

### 1. Detecção do compose file

Novo `session.DetectComposeFile(path string) (string, bool)` — stat direto
na raiz de `path`, nesta ordem: `docker-compose.yml`, `docker-compose.yaml`,
`compose.yml`, `compose.yaml`. Primeiro que existir, vence. Sem cache — é
uma chamada `os.Stat`, roda de novo a cada troca de instância exibida, não
a cada frame de render.

### 2. Painel (4ª aba)

Reaproveita `TerminalPane` (mesma infra de Bash/Cursor CLI, que já
sustenta uma sessão tmux por instância):

- `NewDockerPane()` — análogo a `NewAgentPane()`, programa padrão
  `docker compose logs -f --tail=200`.
- `TabbedWindow.tabs` ganha `"Docker"` como 4º item fixo (`DockerTab = 3`).
  `TabbedWindow` passa a guardar também o painel docker e decide, na hora
  de montar o conteúdo da aba, se a sessão selecionada tem compose file:
  - tem → delega pro `TerminalPane` do docker, igual às outras abas.
  - não tem → renderiza texto estático: "Nenhum docker-compose encontrado
    em `<path>`." Nenhuma sessão tmux é criada nesse caso.
- `ui/mosaic.go`: `mosaicPanelNames` ganha `"Docker"` como 4º item; o
  `Mosaic` guarda uma referência à mesma instância de `TerminalPane` usada
  pelo `TabbedWindow` (mesmo padrão de reuso que já existe entre
  `terminal`/`agent`). `CyclePanel` já usa `len(mosaicPanelNames)` como
  módulo — não precisa mexer na lógica de ciclo.
- `app/app.go`: constrói o `TerminalPane` docker em `newHome`, passa pra
  `NewTabbedWindow` e `NewMosaic`; adiciona limpeza da sessão docker nos
  pontos que já chamam `CleanupTerminal`/`CleanupTerminalForInstance`.

### 3. Ações (menu overlay)

`ui/overlay/dockerActionOverlay.go`, mesmo estilo de
`ui/overlay/confirmationOverlay.go` (tecla direta, sem navegação por
setas):

| Tecla | Ação | Comando |
|---|---|---|
| `l` | Logs | volta pro tail (`docker compose logs -f --tail=200`) |
| `r` | Restart | `docker compose restart` |
| `x` | Stop | `docker compose stop` |
| `u` | Up | `docker compose up -d` |
| `esc` | Cancela | fecha o overlay, não faz nada |

Execução: `Ctrl-C` (interrompe o `logs -f` em andamento) + comando + Enter,
mandado pro tmux session do painel docker via o mesmo mecanismo que já
existe (`SendKeysToInstance`, `ui/terminal.go:207-237`). Sem confirmação
dupla — nenhuma das ações é destrutiva (não é `down -v`). Depois da ação, o
painel mostra a saída do comando disparado; volta a "tailar" só quando o
usuário escolhe `l` de novo.

### 4. Keybinding

Novo `keys.KeyDockerAction`, tecla `a` (livre no mapa atual). Só tem
efeito quando a aba ativa da sessão selecionada é a aba Docker **e** ela
tem compose file — fora disso, no-op. Escopo: só list view. No mosaico o
painel Docker é só leitura (glance dos logs); pra agir, o usuário abre a
sessão na list view.

### 5. Erros

Sem tratamento especial. `docker`/`docker compose` ausente ou daemon
parado aparece como stderr normal dentro do pane — igual qualquer outro
comando de shell hoje na aba Bash.

### 6. Testes

- `DetectComposeFile`: teste de tabela em tempdir, sem mock (estilo
  `session/instance_test.go`).
- Painel docker: mesmo padrão de `ui/terminal_test.go`
  (`mockCmdExec` + `newMockTmuxSession`).
- Overlay de ação: mesmo estilo de teste do `ConfirmationOverlay`, se
  existir; senão, o padrão do próprio `ConfirmationOverlay`.

## Fora de escopo (YAGNI)

- Controle por container/serviço individual (estilo Portainer completo).
- Busca recursiva por compose file em subpastas.
- View Docker global fora do ciclo de sessões.
- Cache de detecção do compose file.
- Confirmação dupla nas ações (restart/stop/up).
