package ui

import (
	"claude-squad/session"
	"fmt"
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
		name             string
		names            []string
		width, height    int
		wantRows         int
		wantCellsPerRow  []int
		grouped          bool
		wantHeaderPerRow bool
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
			name:  "nove sessões cabem em três por três",
			names: namesFor(9), width: 240, height: 60,
			wantRows: 3, wantCellsPerRow: []int{3, 3, 3},
		},
		{
			name:  "projetos diferentes não misturam linhas",
			names: []string{"alpha", "alpha", "alpha", "beta"},
			width: 200, height: 50,
			wantRows: 3, wantCellsPerRow: []int{2, 1, 1},
			grouped: true, wantHeaderPerRow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := planGrid(tt.names, tt.width, tt.height)
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
				// Grouping spends a line per row on the project band; without it
				// the rows still leave one line between them.
				if tt.wantHeaderPerRow {
					used++
				} else if r > 0 {
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

// The mosaic does not paginate: a screen crowded with projects shrinks the cells
// instead of hiding half the agents behind a page nobody knows to turn.
func TestPlanGridNeverHidesASession(t *testing.T) {
	var names []string
	for i := 0; i < 12; i++ {
		names = append(names, fmt.Sprintf("proj%d", i))
	}

	for _, size := range [][2]int{{200, 50}, {120, 30}, {60, 12}} {
		rows := planGrid(names, size[0], size[1])
		seen := make(map[int]bool)
		for _, row := range rows {
			for _, i := range row.idx {
				if seen[i] {
					t.Fatalf("sessão %d desenhada duas vezes em %dx%d", i, size[0], size[1])
				}
				seen[i] = true
			}
		}
		if len(seen) != len(names) {
			t.Fatalf("em %dx%d apareceram %d sessões de %d", size[0], size[1], len(seen), len(names))
		}
	}
}

// A terminal too small for the legibility floor still has to draw something —
// cramped cells beat a blank screen.
func TestPlanGridTinyTerminal(t *testing.T) {
	rows := planGrid(namesFor(4), 30, 8)
	total := 0
	for _, row := range rows {
		total += len(row.idx)
		if row.height < 1 {
			t.Fatalf("linha degenerada: altura %d", row.height)
		}
		for _, w := range row.width {
			if w < 1 {
				t.Fatalf("célula degenerada: largura %d", w)
			}
		}
	}
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

// The mosaic splits projects the same way the list does: a row never mixes two
// projects, so the header above it is true for every cell under it.
func TestBuildRowsBreaksOnProjectChange(t *testing.T) {
	names := []string{"alpha", "alpha", "alpha", "beta"}
	rows := buildRowsFromNames(names, 2)

	if len(rows) != 3 {
		t.Fatalf("linhas = %d, esperado 3 (alpha 2, alpha 1, beta 1)", len(rows))
	}
	for _, row := range rows {
		first := names[row.idx[0]]
		for _, i := range row.idx {
			if names[i] != first {
				t.Fatalf("linha misturou projetos: %v", row.idx)
			}
		}
	}
	if rows[2].group != "beta" {
		t.Fatalf("última linha = %q, esperado beta", rows[2].group)
	}
}

// A project opened again later belongs under the header it already has: two
// sections with the same name read as two different projects.
func TestBuildRowsMergesAProjectOpenedAgain(t *testing.T) {
	names := []string{"cortz", "squad", "cortz", "cortz"}
	rows := buildRowsFromNames(names, 2)

	var headers []string
	for _, row := range rows {
		if len(headers) == 0 || headers[len(headers)-1] != row.group {
			headers = append(headers, row.group)
		}
	}
	if len(headers) != 2 || headers[0] != "cortz" || headers[1] != "squad" {
		t.Fatalf("cabeçalhos = %v, esperado [cortz squad]", headers)
	}

	seen := map[int]bool{}
	for _, row := range rows {
		for _, i := range row.idx {
			if names[i] != row.group {
				t.Fatalf("célula %d (%s) na linha de %s", i, names[i], row.group)
			}
			seen[i] = true
		}
	}
	if len(seen) != len(names) {
		t.Fatalf("células desenhadas = %d, esperado %d", len(seen), len(names))
	}
}

// One project means no grouping at all, exactly like the list — otherwise every
// row would waste a line on a header that separates nothing.
func TestBuildRowsPacksWhenNotGrouped(t *testing.T) {
	rows := buildRowsFromNames([]string{"", "", "", ""}, 2)
	if len(rows) != 2 {
		t.Fatalf("linhas = %d, esperado 2", len(rows))
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
