# Modo de visualização 2 — mosaico interativo

Data: 2026-08-12
Estado: aprovado, em implementação

## Problema

Hoje o coordenador mostra **uma** sessão por vez: lista à esquerda, painel da
sessão selecionada à direita. Com cinco ou seis agentes rodando em paralelo, o
desenvolvedor descobre que alguém parou pedindo aprovação só quando chega
naquela linha da lista.

O modo 2 põe **todas** as sessões na tela ao mesmo tempo, divididas igualmente,
e deixa conversar com uma delas sem que as outras saiam de vista.

## Decisão de arquitetura

O coordenador **desenha** o mosaico e **repassa** o teclado. Não há cirurgia de
tmux: nenhum pane muda de sessão, nenhuma sessão é aninhada.

Foi assim porque as quatro peças necessárias já existiam e só nunca tinham sido
combinadas:

| Peça | Onde já existia |
|---|---|
| Fazer o agente caber num retângulo | `Instance.SetPreviewSize` |
| Ler o que o agente pintou, com cor | `TmuxSession.CapturePaneContent` (`-e`) |
| Mandar tecla sem entrar na sessão | `Instance.SendKeys` |
| Redesenhar 10x por segundo | tick de preview de 100ms |

As alternativas foram recusadas:

- **`join-pane` numa sessão agregadora** — mover o único pane de uma sessão a
  destrói, quebrando `has-session`, restore, pausar e retomar. Exigiria um pane
  fantasma em cada sessão e contaminaria toda a base com "o agente às vezes mora
  noutro lugar".
- **`tmux attach` aninhado por célula** — o coordenador já mantém um cliente
  aberto em cada sessão; dois clientes brigam pelo tamanho da janela e a tela do
  agente fica pulando de forma.

Consequência boa: se o modo 2 falhar, sai-se dele e nada quebrou — nenhuma
sessão foi tocada estruturalmente.

## Estados

| Modo | Sub-estado | Quem recebe as teclas | Evidência na tela |
|---|---|---|---|
| 1 · Lista | — | coordenador | linha selecionada |
| 1 · Sessão | — | agente | tela inteira |
| 2 · Mosaico | navegar | coordenador | borda fina colorida |
| 2 · Mosaico | focado | agente da célula | borda grossa + título invertido |

Transições:

- `v` alterna entre o modo 1 e o modo 2, em qualquer um dos dois.
- `↵` no mosaico **entra em foco** — o mosaico continua na tela, a célula ganha
  borda grossa.
- `ctrl-l` solta o foco e volta a navegar. É a mesma tecla que hoje devolve a
  tela ao coordenador, de propósito.
- `o` continua abrindo a sessão em tela cheia de verdade, para quando o agente
  precisa de espaço.

Enquanto focado, as teclas do coordenador (`n`, `D`, `R`, `e`, `q`…) **não**
funcionam: vão para o agente. Uma tecla, um significado, sem prefixo mágico.

## Geometria

Colunas partem de `ceil(√n)`, mas legibilidade manda: cada célula precisa de no
mínimo **40 colunas × 12 linhas** de conteúdo. Se não couber, a grade encolhe e
o excedente vai para a próxima página.

```
4 sessões, 200×50 → 2×2, células de 98×23
9 sessões, 200×50 → 3×3, células de 65×15
9 sessões, 120×30 → 2×2, 4 por página, 3 páginas
```

**Paginação segue a seleção.** Não há tecla de página: a ordem das células é a
mesma da lista do modo 1, então `↑↓`/`jk` andam pelas sessões e, ao cruzar a
borda da página, a página vira sozinha. `↑↓` significa a mesma coisa nos dois
modos.

## Ciclo de atualização

A cada tick de 100ms, que já existe:

1. Célula focada: captura **todo tick** — é onde se está digitando.
2. Demais células da página: a cada 5º tick (~500ms).
3. Células fora da página: nada. Continuam vivas, só não são lidas.

Ao entrar no modo 2, cada sessão visível recebe `SetPreviewSize(célula)`. Ao
sair, o tamanho do painel do modo 1 é devolvido.

**Risco assumido:** `capture-pane` não desenha o cursor, então digitar pode
parecer cego. Entra sem cursor; se incomodar, `display-message -p
'#{cursor_x} #{cursor_y}'` na sessão focada e inverter aquele caractere. É uma
linha depois, não uma decisão de arquitetura.

## Célula pausada, caída ou carregando

Ocupa célula, esmaecida, com o mesmo texto que o painel de preview já usa hoje
("Pausada. Pressione 'r' para retomar.", "O agente saiu por conta própria.").

Some da grade seria pior por dois motivos: a ordem de `↑↓` passaria a divergir
entre os modos, e "sumiu" é o pior jeito de comunicar "morreu".

`↵` numa célula assim não entra em foco — avisa e não faz nada.

## Fora de escopo

- Abas (Cursor CLI / Bash) — o mosaico mostra só o agente principal.
- Modo de rolagem por célula — `o` abre em tela cheia e rola lá.
- Mouse: clicar para focar.
- Reordenar (`J`/`K`) dentro do mosaico.

## Onde o código encosta

| Arquivo | O quê |
|---|---|
| `ui/mosaic.go` (novo) | geometria, paginação, render, estado de foco |
| `app/app.go` | modo de visualização, roteamento de teclas, `View`, tamanhos |
| `keys/keys.go` | `KeyViewMode` = `v` |
| `config/state.go` | lembra o último modo entre execuções |
| `app/help.go`, `README.md` | ajuda e tabela de atalhos |

## Como se prova que funciona

- **Geometria** (`ui/mosaic_test.go`): n sessões × tamanho de terminal → colunas,
  linhas, tamanho de célula e número de páginas, incluindo o caso em que o
  mínimo de 40×12 estoura e força paginação, e o caso de terminal minúsculo.
- **Página segue a seleção**: seleção no último item leva à última página.
- **Foco**: célula pausada/caída/não iniciada recusa o foco; célula viva aceita.
- **Tradução de tecla**: `↵`, `esc`, `backspace`, setas e `ctrl-c` viram os bytes
  que o terminal espera.

É onde mora o erro real; o resto é desenho.
