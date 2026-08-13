# Claude Squad (fork)

Coordenador de terminal para trabalhar com **vários agentes ao mesmo tempo** ([Claude Code](https://github.com/anthropics/claude-code), Codex, Gemini, Aider, Cursor CLI). Cada sessão é um agente rodando numa pasta, em segundo plano, e o coordenador mostra o estado de todas elas numa tela só — quem está trabalhando, quem respondeu, quem travou numa pergunta.

Fork de [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad).

- [1. O que muda em relação ao original](#1-o-que-muda-em-relação-ao-original)
- [2. Instalação](#2-instalação)
- [3. Como usar](#3-como-usar)

---

## 1. O que muda em relação ao original

O original é um **gerenciador de branches**: cada sessão nasce como worktree do git, com branch própria, e o fluxo termina em commit/push/checkout. Aqui a sessão é só **um agente rodando numa pasta** — e em volta disso foi construído tudo que faltava para acompanhar dez agentes sem ficar entrando em cada um.

| Original | Este fork |
|---|---|
| Cria worktree + branch por sessão | Roda direto no diretório escolhido, sem tocar em git |
| Precisa abrir dentro de um repositório | Abre de qualquer lugar, repositório ou não |
| Um coordenador por repositório | Um coordenador só para vários projetos, agrupados na tela |
| Fluxo termina em push/checkout/diff de branch | Fluxo termina quando o agente devolve a vez |
| Uma sessão por vez na tela | Modo mosaico com todas as sessões ao mesmo tempo |
| Tela cheia ao entrar na sessão | Digita na sessão com a lista (ou o mosaico) visível |
| "Está rodando" ou "está parado" | Trabalhando, respondeu, bloqueado em pergunta, caiu, pausado, órfã |
| Só o sino do terminal | Som + notificação do sistema com o nome da sessão |
| Baixa binário de release | Compila do código deste fork |
| Interface em inglês | Interface em pt-br |

Nada de preparar branch, conciliar worktree ou limpar sujeira depois. Aponta a pasta, escolhe o agente, trabalha.

### 1.1 Sessão sem git

- **Sessão = diretório.** O manejo de worktrees, branches, commit, push e checkout foi removido inteiro. O agente edita a pasta que você apontou; o versionamento é seu.
- **Abre em qualquer lugar** — o coordenador não exige mais estar dentro de um repositório.
- **Criação em duas etapas**: nome (até 32 caracteres) e diretório de trabalho, com **autocomplete de pastas** por `tab` (percorre as candidatas em ciclo e mostra quantas casam com o que você digitou). O diretório é validado na hora — precisa existir e ser gravável, senão o campo continua aberto com o que você escreveu.
- **Criar já com prompt** (`N`): depois do formulário, abre uma caixa de prompt com **seletor de perfil de agente**; o agente sobe e recebe o prompt sozinho.
- **Renomear** sessão já criada (`R`) — só o rótulo muda, terminal e diretório continuam os mesmos.
- **Reordenar** a lista (`J` / `K`), com a ordem persistida entre execuções.
- **Limite de sessões** configurável (`max_sessions`, padrão 10). Ao estourar, o coordenador avisa em vez de criar.
- **Perfis de agente**: a lista oferecida na criação vem de `profiles` na configuração, com o perfil padrão em primeiro.

### 1.2 Estados que dizem o que fazer

Cada sessão carrega um marcador do lado do nome, na lista e no mosaico:

| Marcador | Significado |
|---|---|
| spinner | agente trabalhando |
| `⬤ RESPONDEU` | devolveu a vez; tem resposta esperando leitura |
| `⏵ APROVAR` | parou numa pergunta e **não anda** até você responder |
| `✖` | o agente caiu sozinho — `r` sobe de novo retomando a conversa |
| pausado | terminal fechado, sessão preservada |
| órfã | o diretório da sessão sumiu; a segunda linha mostra o caminho ausente |

- **Pergunta bloqueando ≠ resposta pronta.** Agente parado numa pergunta é bloqueio (`⏵ APROVAR`); agente que terminou o turno é resposta (`⬤ RESPONDEU`). São estados diferentes, com cor e prioridade diferentes — o original tratava os dois como "esperando".
- **O aviso só toca quando o turno acaba de verdade.** O coordenador exige três leituras seguidas de trabalho antes de "armar" a sessão e seis leituras de silêncio (≈3s) antes de declarar o turno encerrado. Spinner piscando e cursor não disparam alarme falso.
- **Notificação do sistema** com o nome da sessão (`notify-send`, `wsl-notify-send.exe` ou `osascript`, detectados automaticamente; dá para apontar outro comando). O som sozinho não resolve com o terminal em outra área de trabalho.
- **Agente que morre vira estado próprio** em vez de continuar com a bolinha verde — e a queda também é anunciada. O estado é lido pelo **nome do comando** da sessão, não por heurística frágil de texto na tela.
- `espaço` **pula para a próxima sessão que respondeu**. `p` **manda prompt sem entrar** na sessão.
- `R` renomeia a sessão **aqui**; `ctrl-r` manda `/rename` **para o agente**, que renomeia a conversa dele.
- `y` **devolve o mouse ao terminal** para selecionar e copiar texto com o mouse, como em qualquer outro programa. Enquanto está ligado a roda não rola nada (o terminal fica com os eventos); `y` de novo devolve o mouse ao aplicativo.

### 1.3 Mosaico — todas as sessões na tela (`v`)

Modo de tela novo, que não existe no original. `v` divide a tela igualmente entre **todas** as sessões.

- Cada célula abre com as mesmas duas linhas da lista — número, nome, marcador de estado e, embaixo, o diretório com o diff — mais a faixa que diz qual painel está vivo ali. A barra de título com o consumo continua no topo.
- **Só a célula onde você está fica acesa**: as outras são repintadas em cinza uniforme, legíveis mas sem competir. Nada é cortado, quebrado ou deslocado — só a cor muda.
- **Duas bordas grossas, dois significados**: roxa é onde estão as setas, verde é para onde vai o que você digitar.
- `↵` **dá o teclado à célula em destaque** e o que você digita vai direto para aquele agente enquanto os outros continuam pintando ao lado. Enquanto a célula está focada, **toda** tecla é do agente, inclusive `q` e `D`.
- `ctrl-l` devolve o teclado ao coordenador; `o` abre a sessão em tela cheia; `D` encerra a sessão em destaque na hora (sem confirmação — ela está na sua frente).
- A célula em destaque mostra o **cursor do agente**, no mesmo lugar em que ele apareceria com a sessão aberta em tela cheia — é como se vê que ela está esperando o que você digitar.
- As **setas são literais**: `←` vai para a célula da esquerda, `↓` para a de baixo.
- `shift-↑` / `shift-↓` (ou a **roda do mouse**) leem o **histórico daquela célula** sem parar as outras: a célula congela no trecho que você está lendo, marca `↑N` no título e volta ao vivo com `esc` — ou sozinha, assim que você digitar nela.
- `tab` troca o painel **daquela célula** — cada sessão pode estar mostrando um painel diferente.
- Com mais de um projeto aberto, o mosaico **separa por pasta** igual à lista, cada projeto com sua faixa colorida, sem misturar dois projetos na mesma linha.
- Quando não cabem todas com tamanho legível, o mosaico **pagina sozinho** ao chegar na borda.
- `n` no mosaico abre o formulário de nome e diretório no centro da tela.
- O modo escolhido (lista ou mosaico) é **lembrado** entre execuções.

### 1.4 Painéis por sessão

Cada sessão tem três painéis, alternados por `tab`:

| Aba | O que é |
|---|---|
| **Claude Code** | a tela do agente da sessão (prévia ao vivo) |
| **Cursor CLI** | um `agent` do Cursor rodando no mesmo diretório |
| **Bash $** | um shell no mesmo diretório |

O painel de **diff** do original foi removido — ocupava uma aba inteira para mostrar o que a linha do diretório já resume.

- O Cursor CLI e o Bash são **os mesmos** nos dois modos de tela: um shell aberto no mosaico é o shell que a aba mostra na lista.
- `shift-↑` / `shift-↓` rolam o painel; **roda do mouse** também rola o histórico (o tmux entra com o mouse ligado).
- **Shift + arraste** seleciona texto para copiar, com a seleção nativa do terminal.
- `esc` sai do modo de rolagem e volta a acompanhar o vivo.

### 1.5 Lista

- **Agrupamento por projeto** quando há sessões em diretórios diferentes: cada pasta ganha um cabeçalho em caixa alta com cor própria.
- **Diretório no lugar da branch** na segunda linha de cada sessão.
- **Diff da sessão** (`+adicionadas,-removidas`) ao lado do diretório, para diretórios versionados. É lido em ritmo próprio (a cada ~2s, escalonado entre as sessões) para não rodar git sobre tudo duas vezes por segundo.
- **Barra de título** com o consumo da janela de 5h e o selo de auto-yes, visível nos dois modos de tela.
- **Atalhos com cor forte e feedback visual** ao pressionar; layout que não quebra ao redimensionar a janela.

### 1.6 Entrar, pausar, retomar

- **Entrar não vira tela cheia**: `↵` dá o teclado à sessão com a lista (ou o mosaico) ainda visível. `ctrl-l` devolve o teclado ao coordenador — `ctrl-d` ou fechar o terminal mata a sessão.
- `c` **pausa**: fecha o terminal e mantém a sessão. O diretório fica exatamente como está — nada é commitado, nada é removido.
- `r` **retoma** (e também sobe agente caído). Com Claude Code, retomar usa `--continue`: **volta na conversa anterior** em vez de começar do zero.
- **Encerrar uma sessão não reinicia as outras** — e pede confirmação antes.
- Sessões são gravadas em `~/.claude-squad/state.json`. Ao reabrir, sessão sem terminal volta **pausada** em vez de aparecer como viva.
- **Registro corrompido não derruba a carga**: o que dá para restaurar é listado, o arquivo original é preservado com backup e o erro aparece na tela.

### 1.7 Ferramentas e ajustes

- **`ctrl-e` abre o diretório da sessão no editor** configurado (padrão `cursor`, com fallback para `$VISUAL` e `$EDITOR`). Editor não encontrado vira aviso na tela, não travamento.
- **Badge de consumo da janela de 5h** na barra de título (`⏳ 59% 2:47`), lido de um arquivo escrito pelo statusline do Claude Code — ver §3.5.
- **Som e notificação desligáveis** (`disable_bell`, `disable_notify`), com comando de notificação customizável.
- **Editor e limite de sessões configuráveis.**
- **Auto-yes** (`cs -y`) continua existindo: as sessões aceitam sozinhas os pedidos de aprovação e um daemon segue respondendo depois que você sai do coordenador. Sessão em auto-yes nunca aparece como bloqueada.

### 1.8 Desempenho

- As telas de todas as sessões são lidas **em um único processo tmux por rodada**, não um por sessão — é o que mantém uma tela cheia de agentes fluida.
- A sessão tmux é endereçada pelo **nome exato**, sem casar prefixo com outra sessão parecida.
- Diff e uso de contexto rodam num ritmo mais lento e **escalonado** entre as sessões, fora do laço de observação.
- Cada sessão é **dimensionada para o formato em que está sendo mostrada** (prévia, célula do mosaico ou tela cheia), então nada renderiza torto ao trocar de modo.

### 1.9 Instalação e manutenção

- **`install.sh` compila deste repositório** em vez de baixar binário de release, instala como `cs` em `~/.local/bin` e ajusta o PATH. A versão do binário vem da tag de release deste fork.
- **`restart.sh`** faz reinstalação limpa: derruba as sessões tmux, apaga o estado e recompila.
- **`clean.sh` / `clean_hard.sh`** para faxina bruta.

---

## 2. Instalação

Precisa de [Go](https://go.dev/dl/) 1.23+, [tmux](https://github.com/tmux/tmux/wiki/Installing) (o script instala se faltar) e o agente que você vai usar (`claude`, por padrão).

```bash
git clone git@github.com:AndreLuizMMS/claude-squad-andre.git
cd claude-squad-andre
./install.sh
```

Pronto. O script compila o código deste fork, instala o binário como `cs` em `~/.local/bin` e adiciona o diretório ao PATH do seu shell se faltar (zsh, bash, fish ou ash). Abra um shell novo e rode `cs`.

Variações:

```bash
./install.sh --name meu-cs        # outro nome de binário
BIN_DIR=/usr/local/bin ./install.sh
git pull && ./install.sh          # atualizar
```

Manutenção:

| Script | O que faz |
|---|---|
| `./restart.sh` | **Destrutivo.** Mata as sessões tmux `claudesquad_*`, apaga `~/.claude-squad` e reinstala. `--keep-config` preserva o `config.json`, `-y` pula a confirmação |
| `./clean.sh` / `./clean_hard.sh` | Faxina bruta: derruba o servidor tmux e apaga `~/.claude-squad` |
| `cs reset` | Apaga as sessões salvas e limpa as sessões tmux, sem mexer na configuração |

Logs e estado ficam em `~/.claude-squad/`.

---

## 3. Como usar

### 3.1 Primeiros cinco minutos

1. **Abra o coordenador** em qualquer pasta: `cs`.
2. **Crie a sessão** com `n`. O formulário pede duas coisas:
   - **nome** (até 32 caracteres) → `enter`;
   - **diretório** de trabalho, com autocomplete por `tab` — a dica embaixo mostra quantas pastas casam com o que você digitou. `enter` cria, `esc` cancela.
3. O agente sobe numa sessão tmux em segundo plano e a lista passa a acompanhá-lo. Você **não precisa entrar** para saber o que ele está fazendo: a lista mostra o estado e a prévia ao lado mostra a tela dele.
4. **Entre na sessão** com `↵` para conversar. Para **voltar à lista**, `ctrl-l` — não use `ctrl-d` nem feche o terminal, isso mata a sessão.
5. Quer disparar trabalho sem entrar? Selecione a sessão, aperte `p`, digite o prompt, `enter`.
6. Quando um agente devolve a vez, ele ganha o selo `⬤ RESPONDEU`, toca um aviso sonoro e sobe uma notificação do sistema com o nome da sessão. `espaço` pula direto para a próxima sessão que respondeu.
7. **Todas na tela ao mesmo tempo**: `v` liga o mosaico. Ande com as setas, `↵` para digitar na célula em destaque, `ctrl-l` para sair dela.
8. **Terminou**: `D` encerra a sessão (confirma antes). Só o terminal é fechado — o diretório e os arquivos ficam intactos.

Fluxo em uma linha: `n` cria → `↵` entra → `ctrl-l` sai → `c` pausa, `r` retoma, `D` encerra.

### 3.2 Teclas

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

### 3.3 Comandos de linha

```bash
cs                  # abre na pasta atual
cs -p codex         # troca o agente padrão nesta execução
cs -y               # auto-yes (experimental)
cs debug            # caminho e conteúdo da configuração
cs reset            # apaga todas as sessões salvas e limpa as sessões tmux
cs version          # versão do binário
```

### 3.4 Configuração

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

### 3.5 Badge de consumo da janela de 5h

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

---

[AGPL-3.0](LICENSE.md) — fork de [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad).
