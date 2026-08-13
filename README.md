# Claude Squad (fork)

Coordenador de terminal para trabalhar com **vários agentes ao mesmo tempo** ([Claude Code](https://github.com/anthropics/claude-code), Codex, Gemini, Aider, Cursor CLI). Cada sessão é um agente rodando numa pasta, em segundo plano, e o coordenador mostra o estado de todas elas numa tela só — quem está trabalhando, quem respondeu, quem travou numa pergunta.

## Índice

- [O que muda em relação ao original](#o-que-muda-em-relação-ao-original)
- [Instalação](#instalação)
- [Guia rápido — primeiros cinco minutos](#guia-rápido--primeiros-cinco-minutos)
- [Funcionalidades](#funcionalidades)
- [Referência de teclas](#referência-de-teclas)
- [Comandos de linha](#comandos-de-linha)
- [Configuração](#configuração)
- [Badge de consumo da janela de 5h](#badge-de-consumo-da-janela-de-5h)
- [Manutenção](#manutenção)
- [Licença](#licença)

## O que muda em relação ao original

O original é um gerenciador de branches: cada sessão nasce como worktree do git, com branch própria, e o fluxo termina em commit/push/checkout. Aqui a sessão é só **um agente rodando numa pasta**.

| Original | Este fork |
|---|---|
| Cria worktree + branch por sessão | Roda direto no diretório escolhido, sem tocar em git |
| Precisa abrir dentro de um repositório | Abre de qualquer lugar, repositório ou não |
| Um coordenador por repositório | Um coordenador só para vários projetos |
| Atalhos de push, checkout e diff de branch | Diretório da sessão no lugar da branch |
| Uma sessão por vez na tela | Modo mosaico com todas as sessões ao mesmo tempo |
| Interface em inglês | Interface em pt-br |

Nada de preparar branch, conciliar worktree ou limpar sujeira depois. Aponta a pasta, escolhe o agente, trabalha.

## Instalação

Precisa de [tmux](https://github.com/tmux/tmux/wiki/Installing), [Go](https://go.dev/dl/) 1.23+ e o agente instalado (`claude`, por padrão).

```bash
git clone git@github.com:AndreLuizMMS/claude-squad-andre.git
cd claude-squad-andre
./install.sh
```

Instala como `cs` em `~/.local/bin` (compilando do código deste fork — não baixa binário de release) e adiciona o diretório ao PATH do seu shell, se faltar.

Variações:

```bash
./install.sh --name meu-cs        # outro nome de binário
BIN_DIR=/usr/local/bin ./install.sh
git pull && ./install.sh          # atualizar
```

## Guia rápido — primeiros cinco minutos

1. **Abra o coordenador** em qualquer pasta: `cs`.
2. **Crie a sessão** com `n`. O formulário pede duas coisas:
   - **nome** (até 32 caracteres) → `enter`;
   - **diretório** de trabalho, com autocomplete por `tab` — a dica embaixo mostra quantas pastas casam com o que você digitou. `enter` cria, `esc` cancela.
3. O agente sobe numa sessão tmux em segundo plano e a lista passa a acompanhá-lo. Você **não precisa entrar** para saber o que ele está fazendo: a lista mostra o estado e a prévia ao lado mostra a tela dele.
4. **Entre na sessão** com `↵` para conversar. Para **voltar à lista**, `ctrl-l` — não use `ctrl-d` nem feche o terminal, isso mata a sessão.
5. Quer disparar trabalho sem entrar? Selecione a sessão e aperte `p`, digite o prompt, `enter`.
6. Quando um agente devolve a vez, ele ganha o selo `⬤ RESPONDEU`, toca um aviso sonoro e sobe uma notificação do sistema com o nome da sessão. `espaço` pula direto para a próxima sessão que respondeu.
7. **Todas na tela ao mesmo tempo**: `v` liga o mosaico.
8. **Terminou**: `D` encerra a sessão (confirma antes). Só o terminal é fechado — o diretório e os arquivos ficam intactos.

Fluxo em uma linha: `n` cria → `↵` entra → `ctrl-l` sai → `c` pausa, `r` retoma, `D` encerra.

## Funcionalidades

### Sessões

- **Criação em duas etapas** — nome e diretório, com **autocomplete de pastas** por `tab` (percorre as candidatas em ciclo). O diretório é validado na hora: precisa existir e ser gravável, senão o campo continua aberto com o que você digitou.
- **Criar já com prompt** (`N`) — depois do formulário, abre uma caixa de prompt com **seletor de perfil de agente**; o agente sobe e recebe o prompt sozinho.
- **Renomear** sessão já criada (`R`). Só o rótulo muda: terminal, diretório e identidade continuam os mesmos.
- **Reordenar** a lista (`J` / `K`), com a ordem persistida.
- **Limite de sessões** configurável (`max_sessions`, padrão 10). Ao estourar, o coordenador avisa em vez de criar.
- **Perfis de agente** — a lista de agentes oferecida na criação vem de `profiles` na configuração; o perfil padrão aparece primeiro.

### Estados e sinais

Cada sessão carrega um marcador do lado do nome:

| Marcador | Significado |
|---|---|
| spinner | agente trabalhando |
| `⬤ RESPONDEU` | devolveu a vez; tem resposta esperando leitura |
| `⏵ APROVAR` | parou numa pergunta e **não anda** até você responder |
| `✖` | o agente caiu sozinho — `r` sobe de novo retomando a conversa |
| pausado | terminal fechado, sessão preservada |
| órfã | o diretório da sessão sumiu; a segunda linha mostra o caminho ausente |

Detalhes que separam ruído de sinal:

- **Pergunta bloqueando ≠ resposta pronta.** Agente parado numa pergunta é bloqueio (`⏵ APROVAR`); agente que terminou o turno é resposta (`⬤ RESPONDEU`). São estados diferentes, com cor e prioridade diferentes.
- **O aviso só toca quando o turno acaba de verdade.** O coordenador exige três leituras seguidas de trabalho antes de "armar" a sessão e seis leituras de silêncio (≈3s) antes de declarar o turno encerrado — spinner piscando e cursor não disparam alarme falso.
- **Notificação do sistema** com o nome da sessão (`notify-send`, `wsl-notify-send.exe` ou `osascript`, detectados automaticamente; pode-se apontar outro comando). O som não resolve com o terminal em outra área de trabalho.
- **Agente que morre** vira estado próprio em vez de continuar com a bolinha verde — e a queda também é anunciada.
- `espaço` **pula para a próxima sessão que respondeu**; `p` **manda prompt sem entrar** na sessão.

### Painéis por sessão

Cada sessão tem três painéis, alternados por `tab`:

| Aba | O que é |
|---|---|
| **Claude Code** | a tela do agente da sessão (prévia ao vivo) |
| **Cursor CLI** | um `agent` do Cursor rodando no mesmo diretório |
| **Bash $** | um shell no mesmo diretório |

O Cursor CLI e o Bash são **os mesmos** nos dois modos de tela: um shell aberto no mosaico é o shell que a aba mostra na lista.

- `shift-↑` / `shift-↓` rolam o painel; **roda do mouse** também rola o histórico.
- **Shift + arraste** seleciona texto para copiar (o tmux entrega a seleção nativa do terminal).
- `esc` sai do modo de rolagem e volta a acompanhar o vivo.

### Mosaico (`v`)

`v` divide a tela igualmente entre **todas** as sessões. Cada célula abre com as mesmas duas linhas da lista — número, nome, marcador de estado e, embaixo, o diretório com o diff — mais a faixa que diz qual painel está vivo ali. A barra de título com o consumo continua no topo.

- `↵` **dá o teclado à célula em destaque** — a borda fica amarela — e o que você digita vai direto para aquele agente enquanto os outros continuam pintando ao lado. Enquanto a célula está focada, **toda** tecla é do agente, inclusive `q` e `D`.
- `ctrl-l` devolve o teclado ao coordenador; `o` abre a sessão em tela cheia; `D` encerra a sessão em destaque na hora (sem confirmação — ela está na sua frente).
- As **setas são literais**: `←` vai para a célula da esquerda, `↓` para a de baixo.
- `tab` troca o painel **daquela célula** — cada sessão pode estar mostrando um painel diferente.
- Com mais de um projeto aberto, o mosaico **separa por pasta** igual à lista, cada projeto com sua faixa colorida, sem misturar dois projetos na mesma linha.
- Quando não cabem todas com tamanho legível, o mosaico **pagina sozinho** ao chegar na borda.
- `n` no mosaico abre o formulário de nome e diretório no centro da tela.
- O modo escolhido é **lembrado** entre execuções.

### Lista

- **Agrupamento por projeto** quando há sessões em diretórios diferentes: cada pasta ganha um cabeçalho em caixa alta com cor própria.
- **Diff da sessão** (`+adicionadas,-removidas`) na linha do diretório, para diretórios versionados. É lido em ritmo próprio (a cada ~2s, escalonado entre as sessões) para não rodar git sobre tudo duas vezes por segundo.
- **Barra de título** com o consumo da janela de 5h e o selo de auto-yes, visível nos dois modos de tela.

### Pausar, retomar e persistência

- `c` **pausa**: fecha o terminal e mantém a sessão. O diretório fica exatamente como está — nada é commitado, nada é removido.
- `r` **retoma** (e também sobe agente caído). Com Claude Code, retomar usa `--continue`, ou seja, **volta na conversa anterior** em vez de começar do zero.
- Sessões são gravadas em `~/.claude-squad/state.json`. Ao reabrir, sessão sem terminal volta **pausada** em vez de aparecer como viva.
- **Registro corrompido não derruba a carga**: o que dá para restaurar é listado, o arquivo original é preservado com backup e o erro aparece na tela.
- Encerrar uma sessão **não reinicia as outras**.

### Abrir no editor

`ctrl-e` abre o diretório da sessão no editor configurado (padrão `cursor`, com fallback para `$VISUAL` e `$EDITOR`). Editor não encontrado vira aviso na tela, não travamento.

### Auto-yes (experimental)

`cs -y` (ou `auto_yes` na configuração) faz as sessões aceitarem automaticamente os pedidos de aprovação. Ao sair do coordenador, um daemon continua respondendo em segundo plano. Sessão em auto-yes nunca aparece como bloqueada — ela mesma responde a pergunta.

### Desempenho

- As telas de todas as sessões são lidas **em um único processo tmux por rodada**, não um por sessão — é o que mantém uma tela cheia de agentes fluida.
- Diff e uso de contexto rodam num ritmo mais lento e **escalonado** entre as sessões.
- Cada sessão é dimensionada para o formato em que está sendo mostrada (prévia, célula do mosaico ou tela cheia), então nada renderiza torto ao trocar de modo.

## Referência de teclas

**Na lista**

| Tecla | Ação |
|---|---|
| `n` / `N` | Criar sessão / criar já com prompt |
| `↵` `o` | Entrar na sessão |
| `ctrl-l` | Sair da sessão e voltar para a lista |
| `c` / `r` / `D` | Pausar / retomar (ou subir agente caído) / encerrar |
| `R` / `ctrl-e` | Renomear / abrir diretório no editor |
| `espaço` / `p` | Pular para a próxima que respondeu / mandar prompt sem entrar |
| `↑↓` `jk` / `J` `K` | Navegar / reordenar |
| `tab` / `shift-↑↓` | Trocar entre Claude Code, Cursor CLI e Bash / rolar a aba |
| `v` | Alternar entre a lista e o mosaico |
| `?` / `q` | Ajuda / sair |

**No mosaico**

| Tecla | Ação |
|---|---|
| `←↑↓→` | Andar pela grade na direção da seta |
| `↵` | Digitar na célula em destaque, sem sair do mosaico |
| `ctrl-l` | Devolver o teclado ao coordenador |
| `tab` | Trocar o painel daquela célula |
| `o` / `D` | Abrir em tela cheia / encerrar a sessão em destaque |
| `n` / `v` | Nova sessão (formulário no centro) / voltar para a lista |

**No formulário de criação**

| Tecla | Ação |
|---|---|
| `enter` | Confirmar o campo (nome → diretório → cria) |
| `tab` | Completar/percorrer os diretórios candidatos |
| `esc` / `ctrl-c` | Cancelar |

`?` abre a ajuda completa dentro do app.

## Comandos de linha

```bash
cs                  # abre na pasta atual
cs -p codex         # troca o agente padrão nesta execução
cs -y               # auto-yes (experimental)
cs debug            # caminho e conteúdo da configuração
cs reset            # apaga todas as sessões salvas e limpa as sessões tmux
cs version          # versão do binário
```

## Configuração

`~/.claude-squad/config.json` (caminho exato em `cs debug`).

```json
{
  "default_program": "claude",
  "auto_yes": false,
  "daemon_poll_interval": 1000,
  "disable_bell": false,
  "disable_notify": false,
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
| `default_program` | Agente usado ao criar sessões (nome de perfil ou comando) |
| `auto_yes` | `true` aceita os pedidos de aprovação automaticamente |
| `daemon_poll_interval` | Intervalo (ms) com que o daemon do auto-yes olha as sessões |
| `disable_bell` | `true` desliga o som de quando o agente devolve a vez |
| `disable_notify` | `true` desliga a notificação do sistema |
| `notify_command` | Comando da notificação. Recebe título e texto como últimos argumentos. Vazio: detecta `notify-send`, `wsl-notify-send.exe` ou `osascript` |
| `editor_command` | Comando da tecla `ctrl-e`. Vazio: `$VISUAL`, depois `$EDITOR`, depois `cursor` |
| `max_sessions` | Quantas sessões cabem ao mesmo tempo (padrão 10) |
| `profiles` | Agentes oferecidos na criação de sessão |

## Badge de consumo da janela de 5h

O `⏳ 59% 2:47` na barra de título é o consumo da janela de 5 horas da conta, com o tempo até virar. O Claude Code entrega esse número **só para o statusline** e não grava em lugar nenhum, então o coordenador lê de um arquivo que o seu statusline precisa escrever:

`~/.claude/squad-quota.json`

```json
{"used_percentage": 59, "resets_at": 1786593000}
```

`resets_at` é epoch em segundos (`0` quando desconhecido). O badge some se o arquivo tiver mais de 15 minutos — nada rodando, número não confiável — e fica em cor de alerta a partir de 80%.

Para produzir o arquivo, adicione ao seu script de statusline (o payload chega no stdin):

```bash
input=$(cat)
python3 - "$input" <<'PY' 2>/dev/null
import json, os, sys
d = json.loads(sys.argv[1]).get("rate_limits", {}).get("five_hour", {})
if d.get("used_percentage") is not None:
    p = os.path.expanduser("~/.claude/squad-quota.json")
    json.dump({"used_percentage": d["used_percentage"], "resets_at": d.get("resets_at") or 0}, open(p + ".tmp", "w"))
    os.replace(p + ".tmp", p)
PY
```

Sem esse arquivo o coordenador funciona igual — só não mostra o badge.

## Manutenção

| Script | O que faz |
|---|---|
| `./install.sh` | Compila e instala como `cs` |
| `./restart.sh` | **Destrutivo.** Mata as sessões tmux `claudesquad_*`, apaga `~/.claude-squad` e reinstala. `--keep-config` preserva o `config.json`, `-y` pula a confirmação |
| `./clean.sh` / `./clean_hard.sh` | Faxina bruta: derruba o servidor tmux e apaga `~/.claude-squad` |
| `cs reset` | Apaga as sessões salvas e limpa as sessões tmux, sem mexer na configuração |

Logs e estado ficam em `~/.claude-squad/`.

## Licença

[AGPL-3.0](LICENSE.md) — fork de [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad).
