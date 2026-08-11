# Claude Squad (fork)

Aplicação de terminal que gerencia várias sessões de agente ([Claude Code](https://github.com/anthropics/claude-code), Codex, Gemini, Aider) ao mesmo tempo, cada uma no seu próprio diretório.

## O que muda em relação ao original

O original é um gerenciador de branches: cada sessão nasce como worktree do git, com branch própria, e o fluxo termina em commit/push/checkout. Aqui a sessão é só **um agente rodando numa pasta**.

| Original | Este fork |
|---|---|
| Cria worktree + branch por sessão | Roda direto no diretório escolhido, sem tocar em git |
| Precisa abrir dentro de um repositório | Abre de qualquer lugar, repositório ou não |
| Um coordenador por repositório | Um coordenador só para vários projetos |
| Entrar na sessão toma a tela inteira | Entra na sessão com a lista continuando visível; `ctrl-l` volta |
| Atalhos de push, checkout e diff de branch | Diretório da sessão no lugar da branch |
| Interface em inglês | Interface em pt-br |

Na prática: nada de preparar branch, nada de conciliar worktree, nada de limpar sujeira depois. Aponta a pasta, escolhe o agente, trabalha.

Outras adições:

- **Renomear** sessão já criada (`R`) e **abrir a pasta no editor** (`e`, padrão `cursor`).
- **Configuração** de editor, limite de sessões e som de aviso.
- Aviso sonoro toca **só quando o agente devolve a vez**, e o texto do terminal continua **selecionável para copiar**.
- Encerrar uma sessão não reinicia as outras; registro corrompido não derruba a carga.
- Sessão sem terminal volta **pausada** e retoma a conversa anterior em vez de começar do zero.
- Instalador local (`install.sh`) — compila do código deste fork, sem baixar binário de release.

## Instalação

Precisa de [tmux](https://github.com/tmux/tmux/wiki/Installing), [Go](https://go.dev/dl/) 1.23+ e o agente (`claude`, por padrão).

```bash
git clone git@github.com:AndreLuizMMS/claude-squad-andre.git
cd claude-squad-andre
./install.sh
```

Instala como `cs` em `~/.local/bin`. Variações: `./install.sh --name meu-cs`, `BIN_DIR=/usr/local/bin ./install.sh`. Atualizar: `git pull && ./install.sh`.

## Uso

```bash
cs              # abre na pasta atual
cs -p codex     # troca o agente padrão nesta execução
```

Fluxo: `n` cria a sessão (pede título, diretório — com autocomplete — e agente) → `↵` entra → `ctrl-l` sai para a lista → `c` pausa, `r` retoma, `D` encerra.

| Tecla | Ação |
|---|---|
| `n` / `N` | Criar sessão / criar já com prompt |
| `↵` `o` / `ctrl-l` | Entrar na sessão / voltar para a lista |
| `c` / `r` / `D` | Pausar / retomar / encerrar |
| `R` / `e` | Renomear / abrir diretório no editor |
| `↑↓` `jk` / `J` `K` | Navegar / reordenar |
| `tab` / `shift-↑↓` | Trocar de aba / rolar a aba |
| `?` / `q` | Ajuda / sair |

## Configuração

`~/.claude-squad/config.json` (caminho exato em `cs debug`).

```json
{
  "default_program": "claude",
  "disable_bell": false,
  "editor_command": "cursor",
  "max_sessions": 10,
  "profiles": [
    { "name": "claude", "program": "claude" },
    { "name": "codex", "program": "codex" }
  ]
}
```

| Campo | Descrição |
|---|---|
| `default_program` | Agente usado ao criar sessões |
| `disable_bell` | `true` desliga o som de quando o agente devolve a vez |
| `editor_command` | Comando da tecla `e`. Vazio: `$VISUAL`, depois `$EDITOR`, depois `cursor` |
| `max_sessions` | Quantas sessões cabem ao mesmo tempo (padrão 10) |
| `profiles` | Agentes oferecidos na criação de sessão |

## Licença

[AGPL-3.0](LICENSE.md) — fork de [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad).
