# Claude Squad (fork)

Aplicação de terminal que gerencia várias sessões de agente ([Claude Code](https://github.com/anthropics/claude-code), Codex, Gemini, Aider) ao mesmo tempo, cada uma no seu próprio diretório de trabalho.

Diferenças em relação ao projeto de origem:

- Cada sessão roda **direto no diretório escolhido** — sem cópia de trabalho, sem branch própria, sem tocar em versionamento.
- Abre a partir de **qualquer lugar**, inclusive fora de um repositório.
- Um coordenador só atende **vários projetos** ao mesmo tempo.

## Pré-requisitos

- [tmux](https://github.com/tmux/tmux/wiki/Installing)
- [Go](https://go.dev/dl/) 1.23 ou superior
- O agente que você vai usar (`claude`, por padrão)

## Instalação

Clone este repositório e rode o instalador:

```bash
git clone git@github.com:AndreLuizMMS/claude-squad-andre.git
cd claude-squad-andre
./install.sh
```

O binário é compilado a partir do código local e instalado como `cs` em `~/.local/bin`.

Para usar outro nome:

```bash
./install.sh --name meu-cs
```

Para instalar em outro diretório:

```bash
BIN_DIR=/usr/local/bin ./install.sh
```

### Sem o instalador

```bash
go build -o ~/.local/bin/cs .
```

### Atualizar

```bash
git pull && ./install.sh
```

## Uso

```bash
cs
```

Trocar o agente padrão em uma execução:

```bash
cs -p "codex"
```

### Atalhos

| Tecla | Ação |
|---|---|
| `n` | Criar sessão |
| `N` | Criar sessão com prompt |
| `↵` / `o` | Entrar na sessão selecionada |
| `ctrl-l` | Sair da sessão e voltar para a lista |
| `c` | Pausar: fecha o terminal e mantém a sessão |
| `r` | Retomar uma sessão pausada |
| `D` | Encerrar a sessão selecionada |
| `R` | Renomear a sessão selecionada |
| `e` | Abrir o diretório da sessão no editor |
| `↑/j`, `↓/k` | Navegar entre sessões |
| `J`/`K` | Reordenar sessões |
| `tab` | Alternar entre as abas |
| `shift-↑/↓` | Rolar a aba ativa |
| `?` | Ajuda |
| `q` | Sair |

## Configuração

Fica em `~/.claude-squad/config.json` (confirme o caminho com `cs debug`).

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
| `disable_bell` | `true` desliga o som tocado quando um agente devolve a vez |
| `editor_command` | Comando que a tecla `e` usa para abrir o diretório. Vazio: usa `$VISUAL`, depois `$EDITOR`, depois `cursor` |
| `max_sessions` | Quantas sessões cabem ao mesmo tempo (padrão 10) |
| `profiles` | Agentes disponíveis no seletor da criação de sessão |

## Licença

[AGPL-3.0](LICENSE.md) — fork de [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad).
