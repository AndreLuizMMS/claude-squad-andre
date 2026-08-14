package ui

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// namesFor builds the project list planGrid takes: n sessions, all in the same
// project, which is the ungrouped case.
func namesFor(n int) []string {
	return make([]string, n)
}

// The grid arithmetic is where the mosaic can actually be wrong: everything else
// is drawing. The screen has to be filled — no leftover strip at the bottom, no
// gap on the right — and every session has to be on it.
func TestPlanGridFillsTheScreen(t *testing.T) {
	tests := []struct {
		name            string
		names           []string
		width, height   int
		wantRows        int
		wantCellsPerRow []int
	}{
		{
			name:  "quatro sessões dividem a tela em quatro",
			names: namesFor(4), width: 200, height: 50,
			wantRows: 2, wantCellsPerRow: []int{2, 2},
		},
		{
			name:  "três sessões: duas em cima, uma larga embaixo",
			names: namesFor(3), width: 200, height: 50,
			wantRows: 2, wantCellsPerRow: []int{2, 1},
		},
		{
			name:  "uma sessão ocupa a tela inteira",
			names: namesFor(1), width: 200, height: 50,
			wantRows: 1, wantCellsPerRow: []int{1},
		},
		{
			name:  "duas sessões ficam uma sobre a outra",
			names: namesFor(2), width: 200, height: 50,
			wantRows: 2, wantCellsPerRow: []int{1, 1},
		},
		{
			name:  "nove sessões cabem em três por três",
			names: namesFor(9), width: 240, height: 60,
			wantRows: 3, wantCellsPerRow: []int{3, 3, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := planGrid(tt.names, tt.width, tt.height)
			if len(cols) != 1 {
				t.Fatalf("colunas = %d, esperado 1 (um projeto só)", len(cols))
			}
			rows := cols[0].rows
			if len(rows) != tt.wantRows {
				t.Fatalf("linhas = %d, esperado %d", len(rows), tt.wantRows)
			}

			used := 0
			for r, row := range rows {
				if len(row.idx) != tt.wantCellsPerRow[r] {
					t.Fatalf("linha %d com %d células, esperado %d", r, len(row.idx), tt.wantCellsPerRow[r])
				}
				// The row uses the whole width: cells, their borders and the
				// column left between two neighbours.
				w := cellGap * (len(row.width) - 1)
				for _, cw := range row.width {
					w += cw + cellFrameWidth
				}
				if w != tt.width {
					t.Fatalf("linha %d ocupa %d colunas de %d", r, w, tt.width)
				}
				used += row.height + cellFrameHeight
				// The rows still leave one line between them.
				if r > 0 {
					used++
				}
			}
			// And the rows together use the whole height: an empty strip at the
			// bottom is screen the agents could have been using.
			if used != tt.height {
				t.Fatalf("grade ocupa %d linhas de %d", used, tt.height)
			}
		})
	}
}

// Two projects open means two strips side by side: the second project narrows
// the first instead of pushing it up the screen.
func TestPlanGridPutsProjectsSideBySide(t *testing.T) {
	names := []string{"alpha", "alpha", "alpha", "beta"}
	width, height := 200, 50
	cols := planGrid(names, width, height)

	if len(cols) != 2 {
		t.Fatalf("colunas = %d, esperado 2 (alpha e beta)", len(cols))
	}
	if cols[0].group != "alpha" || cols[1].group != "beta" {
		t.Fatalf("projetos = %q e %q", cols[0].group, cols[1].group)
	}

	// The two strips and the air between them cover the screen.
	if total := cols[0].width + cols[1].width + cellGap; total != width {
		t.Fatalf("as colunas ocupam %d de %d", total, width)
	}

	for _, col := range cols {
		// The project header costs one line, once for the strip.
		used := 1
		for r, row := range col.rows {
			for _, i := range row.idx {
				if names[i] != col.group {
					t.Fatalf("célula %d (%s) na coluna de %s", i, names[i], col.group)
				}
			}
			w := cellGap * (len(row.width) - 1)
			for _, cw := range row.width {
				w += cw + cellFrameWidth
			}
			if w != col.width {
				t.Fatalf("linha %d de %s ocupa %d de %d", r, col.group, w, col.width)
			}
			used += row.height + cellFrameHeight
			if r > 0 {
				used++
			}
		}
		if used != height {
			t.Fatalf("coluna %s ocupa %d linhas de %d", col.group, used, height)
		}
	}
}

// Four projects on one line gave each a sliver too narrow to read. They wrap
// into a 2x2 instead, which buys every cell twice the width for its text.
func TestPlanGridWrapsProjectsIntoBands(t *testing.T) {
	names := []string{"doxar", "squad", "regula", "claude"}
	width, height := 240, 60
	cols := planGrid(names, width, height)

	if len(cols) != 4 {
		t.Fatalf("colunas = %d, esperado 4", len(cols))
	}
	want := []int{0, 0, 1, 1}
	for i, col := range cols {
		if col.band != want[i] {
			t.Fatalf("projeto %s na banda %d, esperado %d", col.group, col.band, want[i])
		}
	}

	// Each band covers the full width: two strips and the air between them.
	for b := 0; b < 2; b++ {
		total := cellGap
		for _, col := range cols {
			if col.band == b {
				total += col.width
			}
		}
		if total != width {
			t.Fatalf("banda %d ocupa %d de %d", b, total, width)
		}
	}

	// A strip is now wide enough for a readable cell, which is the whole point.
	if cols[0].width < minStripWidth {
		t.Fatalf("faixa de %d colunas, abaixo do mínimo %d", cols[0].width, minStripWidth)
	}

	// The two bands and the blank line between them cover the screen, the
	// leftover line going to the first band.
	avail := height - 1
	for _, col := range cols {
		used := 1 // the project header
		for r, row := range col.rows {
			used += row.height + cellFrameHeight
			if r > 0 {
				used++
			}
		}
		bandHeight := avail / 2
		if col.band < avail%2 {
			bandHeight++
		}
		if used != bandHeight {
			t.Fatalf("coluna %s ocupa %d linhas de %d", col.group, used, bandHeight)
		}
	}
}

// A screen too short for two bands keeps the projects on one line: a crowded
// line beats a band nobody can read.
func TestPlanGridKeepsOneBandWhenTooShort(t *testing.T) {
	names := []string{"doxar", "squad", "regula", "claude"}
	cols := planGrid(names, 240, 24)

	for _, col := range cols {
		if col.band != 0 {
			t.Fatalf("projeto %s na banda %d numa tela baixa", col.group, col.band)
		}
	}
}

// A project opened again later belongs to the strip it already has: two strips
// with the same name read as two different projects.
func TestPlanGridMergesAProjectOpenedAgain(t *testing.T) {
	names := []string{"cortz", "squad", "cortz", "cortz"}
	cols := planGrid(names, 200, 50)

	if len(cols) != 2 || cols[0].group != "cortz" || cols[1].group != "squad" {
		t.Fatalf("colunas = %+v, esperado [cortz squad]", cols)
	}
	seen := map[int]bool{}
	for _, col := range cols {
		for _, row := range col.rows {
			for _, i := range row.idx {
				if names[i] != col.group {
					t.Fatalf("célula %d (%s) na coluna de %s", i, names[i], col.group)
				}
				seen[i] = true
			}
		}
	}
	if len(seen) != len(names) {
		t.Fatalf("células desenhadas = %d, esperado %d", len(seen), len(names))
	}
}

// The mosaic does not paginate: a screen crowded with projects shrinks the cells
// instead of hiding half the agents behind a page nobody knows to turn.
func TestPlanGridNeverHidesASession(t *testing.T) {
	var names []string
	for i := 0; i < 12; i++ {
		names = append(names, fmt.Sprintf("proj%d", i))
	}

	for _, size := range [][2]int{{200, 50}, {120, 30}, {60, 12}} {
		seen := make(map[int]bool)
		cellsOf(planGrid(names, size[0], size[1]), func(i, _, _ int) {
			if seen[i] {
				t.Fatalf("sessão %d desenhada duas vezes em %dx%d", i, size[0], size[1])
			}
			seen[i] = true
		})
		if len(seen) != len(names) {
			t.Fatalf("em %dx%d apareceram %d sessões de %d", size[0], size[1], len(seen), len(names))
		}
	}
}

// A terminal too small for the legibility floor still has to draw something —
// cramped cells beat a blank screen.
func TestPlanGridTinyTerminal(t *testing.T) {
	total := 0
	cellsOf(planGrid(namesFor(4), 30, 8), func(_, width, height int) {
		total++
		if height < 1 || width < 1 {
			t.Fatalf("célula degenerada: %dx%d", width, height)
		}
	})
	if total != 4 {
		t.Fatalf("sessões desenhadas = %d, esperado 4", total)
	}
}

func TestPlanGridNoSessions(t *testing.T) {
	if rows := planGrid(nil, 200, 50); rows != nil {
		t.Fatalf("sem sessões deveria render grade vazia, veio %+v", rows)
	}
}

// A session with no terminal to type into refuses the keyboard instead of
// swallowing the keys.
func TestFocusRefusesSessionsThatCannotType(t *testing.T) {
	m := NewMosaic(nil, nil)

	if err := m.Focus(nil); err == nil {
		t.Fatal("focar em nada deveria falhar")
	}

	notStarted, err := session.NewInstance(session.InstanceOptions{
		Title: "nova", Path: t.TempDir(), Program: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Focus(notStarted); err == nil {
		t.Fatal("focar em sessão que não subiu deveria falhar")
	}
	if m.Focused() {
		t.Fatal("teclado não deveria ter ido para uma sessão parada")
	}

	paused, err := session.FromInstanceData(session.InstanceData{
		Title: "pausada", Path: t.TempDir(), Program: "claude", Status: session.Paused,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Focus(paused); err == nil {
		t.Fatal("focar em sessão pausada deveria falhar")
	}
}

// Each cell remembers its own panel, and the cached screen of one panel must
// never show up under the title of another.
func TestCyclePanelIsPerCell(t *testing.T) {
	m := NewMosaic(nil, nil)
	inst, err := session.FromInstanceData(session.InstanceData{
		Title: "um", Path: t.TempDir(), Program: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := m.panelOf(inst); got != PreviewTab {
		t.Fatalf("painel inicial = %d, esperado o do agente", got)
	}
	m.CyclePanel(inst)
	if got := m.panelOf(inst); got != AgentTab {
		t.Fatalf("depois de um tab = %d, esperado Cursor", got)
	}
	m.CyclePanel(inst)
	m.CyclePanel(inst)
	if got := m.panelOf(inst); got != PreviewTab {
		t.Fatalf("a rotação deveria voltar ao agente, veio %d", got)
	}

	if contentKey(inst.ID(), PreviewTab) == contentKey(inst.ID(), TerminalTab) {
		t.Fatal("painéis diferentes não podem compartilhar o cache de conteúdo")
	}
}

// One project means no strips at all, exactly like the list — otherwise the
// grid would waste a line on a header that separates nothing.
func TestPlanGridPacksWhenNotGrouped(t *testing.T) {
	cols := planGrid([]string{"", "", "", ""}, 200, 50)
	if len(cols) != 1 || cols[0].group != "" {
		t.Fatalf("colunas = %+v, esperado uma sem cabeçalho", cols)
	}
	if len(cols[0].rows) != 2 {
		t.Fatalf("linhas = %d, esperado 2", len(cols[0].rows))
	}
}

// A cell says about a session exactly what a list row says — its number, its
// name, its marker and its directory — and it says it inside its own slot: a
// line wider than the cell would push its neighbours off the screen.
func TestCellReadsLikeAListRow(t *testing.T) {
	m := NewMosaic(nil, nil)
	dir := t.TempDir()
	inst, err := session.FromInstanceData(session.InstanceData{
		Title: "sessao-com-nome-bem-comprido", Path: dir, Program: "claude", Status: session.Ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A blocked session is the loudest thing a row can say; the cell has to say
	// it just as loudly. (A restored session comes back paused, so its state
	// marker is the paused icon.)
	inst.NeedsApproval = true

	for _, width := range []int{120, 60, 40, 20} {
		cell := m.renderCell(inst, 3, width, 10, true)
		if got := lipgloss.Width(cell); got != width+cellFrameWidth {
			t.Fatalf("célula de %d colunas ocupou %d", width, got)
		}
		if got := lipgloss.Height(cell); got != 10+cellFrameHeight {
			t.Fatalf("célula de 10 linhas ocupou %d", got)
		}
	}

	// Wide enough for everything: the numbering matches the list, the badge is
	// there to be seen, and the second line is the directory.
	cell := m.renderCell(inst, 3, 120, 10, true)
	for _, want := range []string{"3.", "APROVAR", dir} {
		if !strings.Contains(cell, want) {
			t.Fatalf("célula não mostrou %q:\n%s", want, cell)
		}
	}
}

// Arrows are literal in the mosaic: right goes right, down goes down, and the
// edges hold instead of wrapping to the other side of the screen.
func TestMoveIsLiteral(t *testing.T) {
	m := NewMosaic(nil, nil)
	m.SetSize(200, 50)
	instances := make([]*session.Instance, 4) // grade 2x2

	tests := []struct {
		name             string
		from, dCol, dRow int
		want             int
	}{
		{"direita", 0, 1, 0, 1},
		{"baixo", 0, 0, 1, 2},
		{"esquerda volta", 1, -1, 0, 0},
		{"cima volta", 2, 0, -1, 0},
		{"borda esquerda segura", 0, -1, 0, 0},
		{"borda de baixo segura", 3, 0, 1, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.Move(instances, tt.from, tt.dCol, tt.dRow); got != tt.want {
				t.Fatalf("de %d = %d, esperado %d", tt.from, got, tt.want)
			}
		})
	}
}

// With the projects side by side, right at the edge of a strip crosses into the
// next project instead of stopping — the strips are neighbours on screen, so
// they have to be neighbours under the arrows too.
func TestMoveCrossesBetweenProjects(t *testing.T) {
	// alpha com três sessões (2 em cima, 1 embaixo) ao lado de beta com uma.
	cols := planGrid([]string{"alpha", "alpha", "alpha", "beta"}, 200, 50)

	tests := []struct {
		name             string
		from, dCol, dRow int
		want             int
	}{
		{"dentro da coluna", 0, 1, 0, 1},
		{"borda direita entra no beta", 1, 1, 0, 3},
		{"do beta volta para o alpha", 3, -1, 0, 1},
		{"desce dentro do alpha", 0, 0, 1, 2},
		{"beta não tem linha de baixo", 3, 0, 1, 3},
		{"borda esquerda segura", 0, -1, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moveOn(cols, tt.from, tt.dCol, tt.dRow); got != tt.want {
				t.Fatalf("de %d = %d, esperado %d", tt.from, got, tt.want)
			}
		})
	}
}

// Dimming a cell may not change what it says, only how loud it says it: the
// printable text has to survive intact, and no original color may leak through.
func TestDimKeepsTheTextAndDropsTheColors(t *testing.T) {
	line := "\x1b[1;31mERRO\x1b[0m: build quebrou"
	got := dim(line)

	if strings.Contains(got, "\x1b[1;31m") || strings.Contains(got, "\x1b[0m") {
		t.Errorf("dim deixou passar a cor original: %q", got)
	}
	if want := "ERRO: build quebrou"; sgrSequence.ReplaceAllString(got, "") != want {
		t.Errorf("dim mudou o texto: %q, esperado %q", sgrSequence.ReplaceAllString(got, ""), want)
	}
	if !strings.HasPrefix(got, dimColor) {
		t.Errorf("dim não pintou o começo da linha: %q", got)
	}
}

// The caret is the only thing the mosaic draws on top of what an agent wrote, so
// it has to land on the right column whatever colors the line arrived with, and
// it has to appear even where the agent wrote nothing — which is exactly where a
// caret waiting for a prompt sits.
func TestPaintCaretLandsOnTheColumnTheTerminalReported(t *testing.T) {
	tests := []struct {
		name string
		line string
		col  int
		want string
	}{
		{
			name: "plain line",
			line: "abc",
			col:  1,
			want: "a" + caretOn + "b" + caretOff + "c",
		},
		{
			name: "colors take no columns",
			line: "\x1b[31ma\x1b[0mbc",
			col:  1,
			want: "\x1b[31ma\x1b[0m" + caretOn + "b" + caretOff + "c",
		},
		{
			name: "past the end of the line",
			line: "ab",
			col:  4,
			want: "ab  " + caretOn + " " + caretOff,
		},
		{
			name: "first column of an empty line",
			line: "",
			col:  0,
			want: caretOn + " " + caretOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paintCaret(tt.line, tt.col); got != tt.want {
				t.Errorf("paintCaret(%q, %d) = %q, esperado %q", tt.line, tt.col, got, tt.want)
			}
		})
	}
}

// Scrolling is what lets a cell be read while its agent keeps writing, so the
// two ends matter: it may never go past the live screen, and it has to stop
// asking for history tmux does not keep.
func TestScrollStopsAtTheLiveScreenAndAtTheTopOfTheHistory(t *testing.T) {
	m := NewMosaic(nil, nil)
	inst := makeStartedInstance(t, "worker")
	// tmux is the one that knows how far back this session can be read, so the
	// mock behind it is what decides where scrolling has to stop.
	inst.SetTmuxSession(newMockTmuxSession(t, "scrollback", cmd_test.MockCmdExec{
		RunFunc: func(*exec.Cmd) error { return nil },
		OutputFunc: func(c *exec.Cmd) ([]byte, error) {
			if strings.Contains(c.String(), "history_size") {
				return []byte("500\n"), nil
			}
			return []byte(""), nil
		},
	}))

	if m.ScrollOf(inst) != 0 {
		t.Fatalf("uma célula nasce seguindo o agente, não em %d", m.ScrollOf(inst))
	}
	if !m.Scroll(inst, 30) || m.ScrollOf(inst) != 30 {
		t.Errorf("subir 30 linhas deixou a célula em %d", m.ScrollOf(inst))
	}
	if m.Scroll(inst, -100); m.ScrollOf(inst) != 0 {
		t.Errorf("descer além da tela ao vivo deixou a célula em %d", m.ScrollOf(inst))
	}
	if m.Scroll(inst, -1) {
		t.Error("descer com a célula já ao vivo não move nada")
	}

	m.Scroll(inst, maxScrollback*2)
	if m.ScrollOf(inst) != 500 {
		t.Errorf("subir além do histórico deixou a célula em %d, esperado parar em 500", m.ScrollOf(inst))
	}
	if m.Scroll(inst, 10) {
		t.Error("no topo do histórico não há mais nada para onde subir")
	}

	// Typing is the terminal's own way back to the bottom.
	m.SendKeys(inst, "oi")
	if m.ScrollOf(inst) != 0 {
		t.Errorf("digitar não trouxe a célula de volta ao vivo: %d", m.ScrollOf(inst))
	}
}

// The screenshot case: two sessions of the same project, in a strip already
// narrowed by the project beside them, stack instead of splitting that strip in
// two 45-column cells.
func TestPlanGridStacksSessionsInsideANarrowStrip(t *testing.T) {
	cols := planGrid([]string{"doxar", "regula", "regula"}, 190, 58)

	if len(cols) != 2 {
		t.Fatalf("colunas = %d, esperado 2", len(cols))
	}
	regula := cols[1]
	if len(regula.rows) != 2 {
		t.Fatalf("regula com %d linhas, esperado 2 (uma sobre a outra)", len(regula.rows))
	}
	for r, row := range regula.rows {
		if len(row.idx) != 1 {
			t.Fatalf("linha %d de regula com %d células, esperado 1", r, len(row.idx))
		}
	}
}
