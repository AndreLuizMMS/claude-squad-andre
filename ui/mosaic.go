package ui

import (
	"claude-squad/session"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// The mosaic is the second view mode: every session on screen at once, the
// terminal split between them. Only one cell holds the keyboard at a time — the
// focused one — and the others keep painting live around it.
//
// Nothing about a session changes to be shown here. Each cell is the same
// capture the preview pane already does, just at cell size, and typing goes out
// through the same SendKeys the prompt already uses. A session never moves, is
// never nested, and is never restructured, so leaving the mosaic can't leave
// damage behind.
//
// Every session is always on screen: the mosaic does not paginate. A grid that
// hides half the agents defeats the point of having one.

const (
	// minCellWidth is what an agent needs to still be readable. It only steers how
	// many columns the grid opens — nothing is ever moved off screen to honour it.
	minCellWidth = 40

	// comfortCellHeight is where a cell stops being a peephole: enough lines to
	// read what an agent just wrote without scrolling. It is what decides when a
	// project's sessions stop stacking and open a second column.
	comfortCellHeight = 16

	// cellFrameWidth/cellFrameHeight are what the border around a cell costs.
	cellFrameWidth  = 2
	cellFrameHeight = 2

	// cellGap is the columns left between two cells side by side. Without it the
	// two borders touch and read as one thick seam, which made the grid look like
	// a table instead of a set of separate agents. Two columns instead of one
	// because the selected cell now draws a heavy border, and a single column of
	// air was not enough to keep it from touching its neighbour.
	cellGap = 2

	// cellTitleHeight is the title block inside a cell: the name line plus a
	// blank line separating it from the agent's own output.
	cellTitleHeight = 2
)

// slowCellEveryNTicks is how often the cells that are not focused are re-read.
// The focused one is captured on every tick because that is where the developer
// is typing; capturing all of them that often would mean one tmux process per
// session ten times a second, for content nobody is looking at that closely.
const slowCellEveryNTicks = 5

// mosaicPanelNames are the panels a cell can show, indexed by the same tab
// constants the list view uses, so a session reads the same in both modes.
var mosaicPanelNames = []string{"Claude Code", "Cursor CLI", "Bash $"}

var (
	// Green, and only for the cell that is actually taking keystrokes. Typing into
	// the wrong agent is the one mistake the mosaic can cause, so the cell that
	// owns the keyboard is the loudest thing on screen.
	mosaicFocusColor = lipgloss.Color("#00ff87")

	mosaicCellStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#3a3a3a"})

	// The cursor and the keyboard both draw a heavy border: at cell size a thin
	// line in another color was easy to lose among nine boxes. The colors keep
	// them apart — purple is "the arrows are here", green is "your typing goes
	// here".
	mosaicFocusedCellStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(mosaicFocusColor)

	mosaicSelectedCellStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(highlightColor)

	// A cell writes the same two lines a list row writes — name then directory.
	// The list's padding is dropped, a cell has no room to breathe, and so is the
	// filled bar the list paints over the selected row: a cell already has a
	// border to color, and a bar the width of a cell covered a third of what the
	// agent was saying. The name carries the mark instead.
	mosaicTitleStyle = lipgloss.NewStyle().
				Foreground(itemTitleColor)

	mosaicSelectedTitleStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(highlightColor)

	mosaicFocusedTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(mosaicFocusColor)

	mosaicDescStyle = lipgloss.NewStyle().
			Foreground(itemDescColor)

	mosaicSelectedDescStyle = lipgloss.NewStyle().
				Foreground(itemDescColor)

	// The project band: the name in the project's own color and a rule carrying
	// it across the grid. The list's filled tag was drawn for a narrow column —
	// over the full width of a mosaic it read as a label stuck to the left edge
	// rather than a heading for the cells under it.
	mosaicGroupNameStyle = lipgloss.NewStyle().Bold(true)

	mosaicGroupRuleStyle = lipgloss.NewStyle()

	// The panel strip says which of the three terminals the cell is showing, by
	// naming all three and marking the one that is live — the same reading the
	// tab row gives in the list view, down to the color it marks the live one
	// with. Without it a Bash prompt and a quiet agent look the same.
	mosaicPanelStyle = lipgloss.NewStyle().
				Foreground(itemDescColor)

	mosaicActivePanelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(highlightColor)

	// The scroll marker borrows the focus color: a cell frozen in its own past is
	// as much a mode as a cell holding the keyboard, and both are states the
	// developer has to be able to leave.
	mosaicScrollStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(mosaicFocusColor)

	mosaicIdleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})

	mosaicFooterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#808080", Dark: "#808080"})
)

// sgrSequence matches the color and style codes tmux hands back with the screen.
var sgrSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// dimColor is deliberately a mid gray and not a dark one: a cell nobody is
// typing into still has to be readable at a glance — that is the whole point of
// having every agent on screen — it just stops competing with the one that is.
const dimColor = "\x1b[38;5;245m"

// dim repaints a captured line in one flat gray, colors and bold included. It is
// what makes the cells around the cursor read as switched off without dropping,
// wrapping or moving a single character of what the agent wrote.
func dim(line string) string {
	return dimColor + sgrSequence.ReplaceAllString(line, dimColor)
}

// The caret is drawn the way a terminal draws its own: the character under it in
// reverse video. Reverse rather than a colored block because the block would
// hide the character being typed over, and reverse is what the same agent shows
// when the session is opened full screen.
const (
	caretOn  = "\x1b[7m"
	caretOff = "\x1b[27m"
)

// paintCaret puts the caret at a column of a captured line. It counts printable
// columns rather than bytes — a line arrives with its colors still in it, and
// those take no space on screen — and pads the line out when the caret sits past
// the last character written, which is where a caret waiting for input is.
func paintCaret(line string, col int) string {
	if col < 0 {
		return line
	}

	var b strings.Builder
	width, inEscape := 0, false
	for _, r := range line {
		switch {
		case inEscape:
			b.WriteRune(r)
			// A sequence ends at its first letter; everything before that is
			// parameters and has no width of its own.
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		case r == '\x1b':
			inEscape = true
			b.WriteRune(r)
			continue
		case width == col:
			b.WriteString(caretOn)
			b.WriteRune(r)
			b.WriteString(caretOff)
		default:
			b.WriteRune(r)
		}
		width += runewidth.RuneWidth(r)
	}

	if width <= col {
		b.WriteString(strings.Repeat(" ", col-width))
		b.WriteString(caretOn + " " + caretOff)
	}
	return b.String()
}

// gridCol is a project's own strip of the screen: its header and the rows of
// its sessions underneath. Projects stand side by side rather than stacked, so
// opening a second project narrows the screen instead of pushing the first one
// up — which is what the eye expects when two projects are being watched at once.
type gridCol struct {
	group string
	rows  []gridRow
	// width is the content width of the whole strip, borders and gaps included,
	// which is what the header rule is drawn across.
	width int
}

// gridRow is one line of a column: cells that sit side by side and all belong to
// the same project, which is what makes the header above them true for every
// cell under it.
type gridRow struct {
	idx []int
	// width is the content width of each cell in the row, border already
	// deducted, and height the content height they share. Rows are measured one
	// by one so a row with a single session gets the whole width instead of
	// leaving a hole where its neighbours would have been.
	width  []int
	height int
}

// isGrouped reports whether the mosaic should separate projects, using the same
// rule as the list: there is nothing to separate when every session lives in
// the same directory.
func isGrouped(instances []*session.Instance) bool {
	seen := make(map[string]struct{})
	for _, inst := range instances {
		if inst == nil || !inst.Started() {
			continue
		}
		seen[inst.DirName()] = struct{}{}
	}
	return len(seen) > 1
}

// groupNames is the project of each session, or all empty when there is only
// one project and nothing to separate.
func groupNames(instances []*session.Instance) []string {
	grouped := isGrouped(instances)
	names := make([]string, len(instances))
	for i, inst := range instances {
		if grouped && inst != nil && inst.Started() {
			names[i] = inst.DirName()
		}
	}
	return names
}

// groupOrder is the sessions rearranged so each project appears once, at the
// position of its first session. Without it a project opened again later — a
// fifth session in the directory of the first — would open a second header with
// the same name further down the screen.
func groupOrder(names []string) []int {
	order := make([]int, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for i, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		for j := i; j < len(names); j++ {
			if names[j] == name {
				order = append(order, j)
			}
		}
	}
	return order
}

// chunk breaks a project's sessions into rows of at most cols cells.
func chunk(idx []int, cols int) []gridRow {
	if cols < 1 {
		cols = 1
	}
	var rows []gridRow
	for i := 0; i < len(idx); i += cols {
		end := i + cols
		if end > len(idx) {
			end = len(idx)
		}
		rows = append(rows, gridRow{idx: append([]int(nil), idx[i:end]...)})
	}
	return rows
}

// planGrid divides width x height between the sessions and hands back the
// columns already measured. Every session gets a cell: when they no longer fit
// at a readable size the cells get smaller — nothing is pushed to a second page.
func planGrid(names []string, width, height int) []gridCol {
	if len(names) == 0 || width <= 0 || height <= 0 {
		return nil
	}

	// The projects in the order they first appear, each with its own sessions.
	var cols []gridCol
	at := map[string]int{}
	for _, i := range groupOrder(names) {
		name := names[i]
		c, ok := at[name]
		if !ok {
			c = len(cols)
			at[name] = c
			cols = append(cols, gridCol{group: name})
		}
		cols[c].rows = append(cols[c].rows, gridRow{idx: []int{i}})
	}

	// Width is split between the projects, and the strips leave a column of air
	// between them for the same reason two cells do.
	inner := width - cellGap*(len(cols)-1)
	if inner < len(cols) {
		inner = len(cols)
	}
	baseW, extraW := inner/len(cols), inner%len(cols)

	// A project header costs the line it is drawn on, once per strip and not
	// once per row: the strip is the project.
	colHeight := height
	if cols[0].group != "" {
		colHeight--
	}

	for c := range cols {
		w := baseW
		if c < extraW {
			w++
		}
		var idx []int
		for _, row := range cols[c].rows {
			idx = append(idx, row.idx...)
		}
		cols[c].width = w
		cols[c].rows = layoutCells(idx, w, colHeight)
	}
	return cols
}

// layoutCells lays one project's sessions out in a width x height box, already
// measured. It opens as many columns inside the box as it takes to keep the
// rows on screen and, failing that, lets the cells get smaller.
func layoutCells(idx []int, width, height int) []gridRow {
	// Sessions of the same project stack: a second column halves a width that was
	// already halved by the project beside it, and two agents at 45 columns are
	// two agents nobody can read. A column is only opened when stacking would
	// squeeze the rows under a comfortable height — which is what makes three
	// sessions split in two and nine split in three, while two just sit one over
	// the other.
	rowCost := comfortCellHeight + cellTitleHeight + cellFrameHeight
	maxCols := width / (minCellWidth + cellFrameWidth)
	if maxCols < 1 {
		maxCols = 1
	}
	comfortRows := height / rowCost
	if comfortRows < 1 {
		comfortRows = 1
	}

	cols := int(math.Ceil(float64(len(idx)) / float64(comfortRows)))
	if cols < 1 {
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	rows := chunk(idx, cols)

	// Height: the leftover lines from the division go to the first rows instead
	// of being left blank at the bottom of the screen. One line between rows,
	// for the same reason cells leave a column between them.
	avail := height - (len(rows) - 1)
	if avail < len(rows) {
		avail = len(rows)
	}
	base, extra := avail/len(rows), avail%len(rows)

	for r := range rows {
		rowHeight := base
		if r < extra {
			rowHeight++
		}
		rows[r].height = rowHeight - cellFrameHeight
		if rows[r].height < 1 {
			rows[r].height = 1
		}

		// Width is shared by the cells of that row alone, so the last row of an
		// odd count stretches across the screen instead of ending in a gap.
		n := len(rows[r].idx)
		inner := width - cellGap*(n-1)
		if inner < n {
			inner = n
		}
		baseW, extraW := inner/n, inner%n
		rows[r].width = make([]int, n)
		for c := range rows[r].idx {
			w := baseW - cellFrameWidth
			if c < extraW {
				w++
			}
			if w < 1 {
				w = 1
			}
			rows[r].width[c] = w
		}
	}
	return rows
}

// posOf locates a session on the grid: its project strip, its row in that strip
// and its place in that row.
func posOf(cols []gridCol, idx int) (col, row, cell int) {
	for ci, c := range cols {
		for ri, r := range c.rows {
			for i, id := range r.idx {
				if id == idx {
					return ci, ri, i
				}
			}
		}
	}
	return 0, 0, 0
}

func clamp(v, max int) int {
	switch {
	case v < 0:
		return 0
	case v > max:
		return max
	default:
		return v
	}
}

// Mosaic renders every session as a cell of an evenly divided screen.
type Mosaic struct {
	width, height int

	// focused is the session that currently owns the keyboard, or nil while the
	// developer is navigating between cells.
	focused *session.Instance

	// content caches what was last captured per session and panel, so cells that
	// are only re-read every few ticks still have something to draw in between.
	content map[string]string

	// panel is which of the three terminals each session is showing. Sessions
	// not in the map are on the agent, which is what the mosaic is for.
	panel map[string]int

	// scroll is how many lines above the live screen each cell is reading, so a
	// cell can be walked back through the conversation while the others keep
	// following their agents. Zero — the default — is following.
	scroll map[string]int

	// cursor is where the caret sits in the cell under the selection, read on its
	// own because a captured screen carries the characters and not the caret.
	// Only one cell is asked for it: it costs a tmux call, and the caret only
	// answers a question about the cell being typed into.
	cursorID      string
	cursorX       int
	cursorY       int
	cursorVisible bool

	// terminal and agent are the same panes the list view uses, so a shell
	// opened in one view is the same shell in the other.
	terminal, agent *TerminalPane
}

func NewMosaic(terminal, agent *TerminalPane) *Mosaic {
	return &Mosaic{
		content:  make(map[string]string),
		panel:    make(map[string]int),
		scroll:   make(map[string]int),
		terminal: terminal,
		agent:    agent,
	}
}

func (m *Mosaic) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Focused reports whether keystrokes belong to an agent rather than to the
// coordinator.
func (m *Mosaic) Focused() bool {
	return m.focused != nil
}

// FocusedInstance returns the session holding the keyboard, or nil.
func (m *Mosaic) FocusedInstance() *session.Instance {
	return m.focused
}

// Focus hands the keyboard to a session. A session that is not running has
// nothing to type into, and says so instead of silently swallowing the keys.
func (m *Mosaic) Focus(instance *session.Instance) error {
	switch {
	case instance == nil:
		return fmt.Errorf("nenhuma sessão selecionada")
	case !instance.Started() || instance.Status == session.Loading:
		return fmt.Errorf("a sessão '%s' ainda não subiu", instance.Title)
	case instance.Paused():
		return fmt.Errorf("a sessão '%s' está pausada — aperte r para retomar", instance.Title)
	case instance.HasExited() && m.panelOf(instance) == PreviewTab:
		return fmt.Errorf("o agente da sessão '%s' caiu — aperte r para subir de novo", instance.Title)
	}
	m.focused = instance
	return nil
}

// Blur takes the keyboard back for the coordinator.
func (m *Mosaic) Blur() {
	m.focused = nil
}

// panelOf is which terminal a session's cell is showing.
func (m *Mosaic) panelOf(instance *session.Instance) int {
	if instance == nil {
		return PreviewTab
	}
	return m.panel[instance.ID()]
}

// CyclePanel moves one cell to the next panel — agent, Cursor, shell — the same
// order the tabs follow in the list view.
func (m *Mosaic) CyclePanel(instance *session.Instance) {
	if instance == nil {
		return
	}
	// The panels have separate histories, so a cell that was reading back through
	// the shell would land at an arbitrary point in the agent's. Every panel
	// opens where it is now.
	m.ScrollToLive(instance)
	m.panel[instance.ID()] = (m.panelOf(instance) + 1) % len(mosaicPanelNames)
}

// maxScrollback is where scrolling up stops asking for more. tmux keeps a
// bounded history and clamps a read that starts before it, so the only thing a
// higher number would buy is a key that appears to do nothing.
const maxScrollback = 10000

// ScrollOf is how far back the cell of a session is reading, in lines.
func (m *Mosaic) ScrollOf(instance *session.Instance) int {
	if instance == nil {
		return 0
	}
	return m.scroll[instance.ID()]
}

// Scroll walks a cell back through its own history — up is into the past — and
// reports whether anything moved. The cell stops following the agent until it
// is scrolled back to the bottom, which is what makes reading a long answer
// possible while the agent keeps writing.
func (m *Mosaic) Scroll(instance *session.Instance, lines int) bool {
	if instance == nil || !instance.Started() || instance.Paused() {
		return false
	}
	id := instance.ID()
	next := m.scroll[id] + lines
	if next < 0 {
		next = 0
	}
	// The ceiling is asked for only when going up, and only on the press that
	// would cross it: it costs a tmux call, and it does not change while the
	// developer is reading. Stopping at it is what keeps a cell full — a window
	// starting before the oldest line tmux kept comes back short.
	if next > m.scroll[id] {
		if limit := m.historyOf(instance); next > limit {
			next = limit
		}
	}
	if next > maxScrollback {
		next = maxScrollback
	}
	if next == m.scroll[id] {
		return false
	}
	if next == 0 {
		delete(m.scroll, id)
	} else {
		m.scroll[id] = next
	}
	return true
}

// historyOf is how far back the terminal behind a cell can be read. A read that
// fails answers zero, which pins the cell to the live screen rather than letting
// it scroll into a window nobody can prove exists.
func (m *Mosaic) historyOf(instance *session.Instance) int {
	var n int
	var err error
	if pane := m.paneOf(instance); pane != nil {
		n, err = pane.HistoryForInstance(instance)
	} else {
		n, err = instance.HistorySize()
	}
	if err != nil {
		return 0
	}
	return n
}

// ScrollToLive puts a cell back on the agent's live screen.
func (m *Mosaic) ScrollToLive(instance *session.Instance) bool {
	if instance == nil || m.scroll[instance.ID()] == 0 {
		return false
	}
	delete(m.scroll, instance.ID())
	return true
}

// SendKeys types into whichever terminal the cell is showing. Typing into a
// Bash cell has to reach that shell, not the agent behind it.
func (m *Mosaic) SendKeys(instance *session.Instance, keys string) error {
	// Typing is a terminal's own way of saying "take me back to the bottom": the
	// answer to what was just typed is at the live screen, not where the cell was
	// left reading.
	m.ScrollToLive(instance)

	switch pane := m.paneOf(instance); {
	case pane != nil:
		return pane.SendKeysToInstance(instance, keys)
	default:
		return instance.SendKeys(keys)
	}
}

// paneOf returns the shell pane backing a cell, or nil when the cell is showing
// the session's own agent.
func (m *Mosaic) paneOf(instance *session.Instance) *TerminalPane {
	switch m.panelOf(instance) {
	case AgentTab:
		return m.agent
	case TerminalTab:
		return m.terminal
	}
	return nil
}

// grid is the arithmetic of one draw: one strip per project, already sized.
func (m *Mosaic) grid(instances []*session.Instance) []gridCol {
	return planGrid(groupNames(instances), m.width, m.height)
}

// cellsOf walks every cell of the grid with the size it was measured at.
func cellsOf(cols []gridCol, visit func(idx, width, height int)) {
	for _, col := range cols {
		for _, row := range col.rows {
			for c, i := range row.idx {
				visit(i, row.width[c], row.height)
			}
		}
	}
}

// SyncSizes gives every session's terminal the shape of its own cell. Cells no
// longer share a size — a row with one session is wider than a row with three —
// so each one is sized on its own.
func (m *Mosaic) SyncSizes(instances []*session.Instance) {
	cellsOf(m.grid(instances), func(i, width, height int) {
		inst := instances[i]
		if inst == nil || !inst.Started() || inst.Paused() {
			return
		}
		height -= cellTitleHeight
		if width < 1 || height < 1 {
			return
		}
		// A session that refuses the resize still draws, just at the old shape
		// until the next one.
		_ = inst.SetPreviewSize(width, height)
	})
}

// Move is arrow navigation as the eye reads it: left goes to the cell on the
// left, down to the cell below. A linear next/previous would jump across the
// screen and look like the selection teleported.
func (m *Mosaic) Move(instances []*session.Instance, selectedIdx, dCol, dRow int) int {
	return moveOn(m.grid(instances), selectedIdx, dCol, dRow)
}

// moveOn is the step itself, on a grid already measured.
func moveOn(cols []gridCol, selectedIdx, dCol, dRow int) int {
	if len(cols) == 0 {
		return selectedIdx
	}
	ci, ri, c := posOf(cols, selectedIdx)

	if dRow != 0 {
		ri = clamp(ri+dRow, len(cols[ci].rows)-1)
	}
	if dCol != 0 {
		// Sideways is first a step inside the row; only at its edge does it cross
		// into the neighbouring project, landing on the cell nearest the seam.
		if next := c + dCol; next >= 0 && next < len(cols[ci].rows[ri].idx) {
			c = next
		} else if nc := clamp(ci+dCol, len(cols)-1); nc != ci {
			ci = nc
			ri = clamp(ri, len(cols[ci].rows)-1)
			if dCol > 0 {
				c = 0
			} else {
				c = len(cols[ci].rows[ri].idx) - 1
			}
		}
	}
	c = clamp(c, len(cols[ci].rows[ri].idx)-1)
	return cols[ci].rows[ri].idx[c]
}

// UpdateContent re-reads every session on screen. The focused cell is read every
// tick because that is where typing shows up; the rest are read on a slower beat
// so a screenful of agents does not mean a screenful of tmux processes ten times
// a second.
func (m *Mosaic) UpdateContent(instances []*session.Instance, selectedIdx, tick int) {
	// A session that paused, crashed or was killed cannot take keystrokes, and
	// holding the keyboard for it would swallow them silently.
	if m.focused != nil && (!m.focused.Started() || m.focused.Paused() ||
		(m.focused.HasExited() && m.panelOf(m.focused) == PreviewTab)) {
		m.focused = nil
	}

	slowRound := tick%slowCellEveryNTicks == 0

	// Cells showing their own agent live are read together, in one tmux process
	// for the whole grid: this runs on the drawing thread, and one process per
	// cell froze the screen for as long as it took to walk the grid. Cells
	// showing a shell, and cells scrolled back into their history, ask for
	// something the batch cannot give — a resize first, or a window of the
	// scrollback — so they keep their own read.
	var agents []*session.Instance
	type ownRead struct {
		inst          *session.Instance
		width, height int
		offset        int
	}
	var own []ownRead

	cellsOf(m.grid(instances), func(i, width, height int) {
		inst := instances[i]
		if inst == nil || !inst.Started() || inst.Paused() {
			return
		}
		if inst != m.focused && !slowRound {
			return
		}
		offset := m.scroll[inst.ID()]
		if m.paneOf(inst) == nil && offset == 0 {
			agents = append(agents, inst)
			return
		}
		own = append(own, ownRead{inst, width, height - cellTitleHeight, offset})
	})

	for id, content := range session.CapturePanes(agents) {
		m.content[contentKey(id, PreviewTab)] = content
	}
	for _, cell := range own {
		content, err := m.capture(cell.inst, cell.width, cell.height, cell.offset)
		if err != nil {
			// A capture that fails is not worth an error box: the next tick
			// tries again, and the cell keeps showing what it had.
			continue
		}
		m.content[contentKey(cell.inst.ID(), m.panelOf(cell.inst))] = content
	}

	m.readCursor(instances, selectedIdx)
}

// readCursor asks tmux where the caret is in the cell under the selection. Only
// that one cell is asked — it is the only one the developer can type into — and
// only while it is following its terminal: a cell scrolled back into the history
// is showing a screen the caret has long left.
func (m *Mosaic) readCursor(instances []*session.Instance, selectedIdx int) {
	m.cursorID, m.cursorVisible = "", false

	if selectedIdx < 0 || selectedIdx >= len(instances) {
		return
	}
	inst := instances[selectedIdx]
	if inst == nil || !inst.Started() || inst.Paused() || m.scroll[inst.ID()] != 0 {
		return
	}
	if inst.HasExited() && m.panelOf(inst) == PreviewTab {
		return
	}

	var x, y int
	var visible bool
	var err error
	if pane := m.paneOf(inst); pane != nil {
		x, y, visible, err = pane.CursorForInstance(inst)
	} else {
		x, y, visible, err = inst.CursorPosition()
	}
	if err != nil {
		return
	}
	m.cursorID, m.cursorX, m.cursorY, m.cursorVisible = inst.ID(), x, y, visible
}

// capture reads whichever terminal the cell is showing, at cell size, from
// `offset` lines above the live screen.
func (m *Mosaic) capture(instance *session.Instance, width, height, offset int) (string, error) {
	if pane := m.paneOf(instance); pane != nil {
		return pane.CaptureForInstance(instance, width, height, offset)
	}
	if instance.HasExited() {
		return "", fmt.Errorf("agente encerrado")
	}
	return instance.PreviewWindow(offset, height)
}

// contentKey separates the cached capture of each panel: switching a cell to
// Bash must not show the agent's last screen under a Bash title.
func contentKey(id string, panel int) string {
	return fmt.Sprintf("%s#%d", id, panel)
}

// Forget drops the cached captures of a session that is gone.
func (m *Mosaic) Forget(id string) {
	for panel := range mosaicPanelNames {
		delete(m.content, contentKey(id, panel))
	}
	delete(m.panel, id)
	delete(m.scroll, id)
	if m.focused != nil && m.focused.ID() == id {
		m.focused = nil
	}
}

// String draws the grid.
func (m *Mosaic) String(instances []*session.Instance, selectedIdx int) string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if len(instances) == 0 {
		return lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Nenhum agente rodando ainda. Pressione 'n' para criar uma sessão.")
	}

	cols := m.grid(instances)

	// One strip per project, side by side, each headed by its own name. Within a
	// strip the rows are separated by a blank line, the same air two cells leave
	// between them.
	var strips []string
	for ci, col := range cols {
		var lines []string
		if col.group != "" {
			lines = append(lines, m.groupHeader(col.group, col.width))
		}
		for r, row := range col.rows {
			if r > 0 {
				lines = append(lines, "")
			}
			var cells []string
			for c, i := range row.idx {
				cell := m.renderCell(instances[i], i+1, row.width[c], row.height, i == selectedIdx)
				if c < len(row.idx)-1 {
					cell = lipgloss.NewStyle().MarginRight(cellGap).Render(cell)
				}
				cells = append(cells, cell)
			}
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		}
		strip := lipgloss.JoinVertical(lipgloss.Left, lines...)
		if ci < len(cols)-1 {
			strip = lipgloss.NewStyle().MarginRight(cellGap).Render(strip)
		}
		strips = append(strips, strip)
	}

	grid := lipgloss.JoinHorizontal(lipgloss.Top, strips...)
	if !m.Focused() {
		// The menu under the mosaic already lists the keys; a second line
		// repeating them said the same thing twice, in two wordings.
		return grid
	}
	return lipgloss.JoinVertical(lipgloss.Left, grid, m.footer())
}

// groupHeader draws the band over a project's strip: the name in the project's
// color and a rule that carries it to the edge of that strip, so the header
// reads as belonging to the cells beneath it rather than to the whole screen.
func (m *Mosaic) groupHeader(name string, width int) string {
	color := groupColor(name)
	label := mosaicGroupNameStyle.Foreground(color).Render(strings.ToUpper(name) + " ")
	rule := width - lipgloss.Width(label)
	if rule < 1 {
		return lipgloss.NewStyle().MaxWidth(width).Render(label)
	}
	return label + mosaicGroupRuleStyle.Foreground(color).Render(strings.Repeat("─", rule))
}

// footer says what the keyboard is doing while a cell holds it — the one thing
// the menu below cannot say, because those keys are going to the agent.
func (m *Mosaic) footer() string {
	hint := fmt.Sprintf("digitando em '%s' · ctrl-l solta o teclado", m.focused.Title)
	return mosaicFooterStyle.Width(m.width).Render(hint)
}

// renderCell draws one session: the two lines the list writes about it — its
// name with its state, then its directory with the diff — and under them
// whatever its terminal last showed.
func (m *Mosaic) renderCell(instance *session.Instance, idx, width, height int, selected bool) string {
	style := mosaicCellStyle
	switch {
	case instance == m.focused:
		style = mosaicFocusedCellStyle
	case selected:
		style = mosaicSelectedCellStyle
	}

	title := m.cellTitleLine(instance, idx, width, selected)
	dir := m.cellDirLine(instance, width, selected)
	// Everything except the cell under the cursor is dimmed. The cursor and the
	// keyboard are the same cell in practice — focus is taken on the selected one
	// — so the test reads as "the cell the developer is on stays lit".
	// The caret is drawn only where it was read: the selected cell, following its
	// terminal. A caret painted in a cell nobody can type into would be pointing
	// at the wrong keyboard.
	caretX, caretY := -1, -1
	if m.cursorVisible && instance != nil && instance.ID() == m.cursorID {
		caretX, caretY = m.cursorX, m.cursorY
	}
	body := m.cellBody(instance, width, height-cellTitleHeight,
		!selected && instance != m.focused, caretX, caretY)

	// MaxWidth/MaxHeight are the guarantee that one cell can never push the
	// others off screen: a session that emitted a line wider than its cell —
	// which happens for the moment between a resize and the next capture —
	// would otherwise wrap and make its box taller than its slot.
	return style.
		Width(width).
		Height(height).
		MaxWidth(width + cellFrameWidth).
		MaxHeight(height + cellFrameHeight).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, dir, body))
}

// cellTitleLine is the list's first line inside a cell: the session's number and
// name on the left with its state marker, and on the right the panel the cell is
// showing — what the tab row says in the other view.
func (m *Mosaic) cellTitleLine(instance *session.Instance, idx, width int, selected bool) string {
	titleStyle := mosaicTitleStyle
	switch {
	case instance == m.focused:
		// The cell holding the keyboard names itself in the same color as its
		// border, so the eye lands on the title instead of hunting the frame.
		titleStyle = mosaicFocusedTitleStyle
	case selected:
		titleStyle = mosaicSelectedTitleStyle
	}
	bg := titleStyle.GetBackground()
	titleStyle = instanceTitleStyle(instance, titleStyle)

	// The marker takes its columns first: an alert nobody can read is not an
	// alert. The mosaic has no spinner of its own, so a working session shows
	// its state icon.
	marker, markerWidth := instanceMarker(instance, "", bg)
	prefix := fmt.Sprintf(" %d. ", idx)
	title := instance.Title
	avail := width - runewidth.StringWidth(prefix) - markerWidth - 1
	if avail < 1 {
		avail = 1
	}
	if runewidth.StringWidth(title) > avail {
		title = runewidth.Truncate(title, avail, "...")
	}
	label := prefix + title + " "

	// A cell reading back through its history says so, because it is the one
	// state where the cell is not showing what the agent is doing right now — and
	// nothing else on screen would give that away.
	back := ""
	if n := m.scroll[instance.ID()]; n > 0 {
		back = mosaicScrollStyle.Background(bg).Render(fmt.Sprintf("↑%d ", n))
	}

	// The whole strip when it fits, the active panel alone when it does not:
	// which terminal is live is the part that cannot be guessed from the content.
	badge := back + m.panelStrip(instance, true, bg) + " "
	gap := width - runewidth.StringWidth(label) - markerWidth - lipgloss.Width(badge)
	if gap < 1 {
		badge = back + m.panelStrip(instance, false, bg) + " "
		gap = width - runewidth.StringWidth(label) - markerWidth - lipgloss.Width(badge)
	}
	// Down to the last columns the scroll marker outlives the panel names: which
	// terminal a cell shows can be read from what it prints, that it is frozen in
	// the past cannot.
	if gap < 1 {
		badge = back
		gap = width - runewidth.StringWidth(label) - markerWidth - lipgloss.Width(badge)
	}
	if gap < 1 {
		badge, gap = "", width-runewidth.StringWidth(label)-markerWidth
	}
	if gap < 0 {
		gap = 0
	}

	line := titleStyle.Render(label) + marker + titleStyle.Render(strings.Repeat(" ", gap)) + badge
	return lipgloss.NewStyle().MaxWidth(width).Width(width).Render(line)
}

// cellDirLine is the list's second line inside a cell: the working directory on
// the left and the diff on the right, which is what tells two agents of the same
// project apart.
func (m *Mosaic) cellDirLine(instance *session.Instance, width int, selected bool) string {
	descStyle := mosaicDescStyle
	if selected {
		descStyle = mosaicSelectedDescStyle
	}

	diff, diffWidth := instanceDiff(instance, descStyle.GetBackground(), descStyle.GetForeground())
	room := width - diffWidth
	if room < 1 {
		return descStyle.Width(width).MaxWidth(width).Render("")
	}

	line := fmt.Sprintf(" %s %s", dirIcon, shortenHome(instanceSubtitle(instance)))
	if runewidth.StringWidth(line) > room {
		line = runewidth.Truncate(line, room, "...")
	}
	line += strings.Repeat(" ", room-runewidth.StringWidth(line))
	return lipgloss.NewStyle().MaxWidth(width).Render(descStyle.Render(line) + diff)
}

// homeDir is read once: the directory line is drawn for every cell on every
// tick, and the home directory does not move.
var homeDir, _ = os.UserHomeDir()

// shortenHome writes the home directory as ~, which is how the agents inside the
// cells write it themselves. A cell is narrow enough that the fourteen columns
// of "/home/andreluiz" were pushing the actual project name out of view.
func shortenHome(path string) string {
	if homeDir == "" || !strings.HasPrefix(path, homeDir) {
		return path
	}
	return "~" + strings.TrimPrefix(path, homeDir)
}

// panelStrip is the cell's tab row: the three panel names with the live one
// marked, or just the live one when the cell is too narrow for all three.
func (m *Mosaic) panelStrip(instance *session.Instance, all bool, bg lipgloss.TerminalColor) string {
	active := m.panelOf(instance)
	activeStyle := mosaicActivePanelStyle.Background(bg)
	if !all {
		return activeStyle.Render(mosaicPanelNames[active])
	}
	idleStyle := mosaicPanelStyle.Background(bg)
	parts := make([]string, len(mosaicPanelNames))
	for i, name := range mosaicPanelNames {
		if i == active {
			parts[i] = activeStyle.Render(name)
			continue
		}
		parts[i] = idleStyle.Render(name)
	}
	return strings.Join(parts, idleStyle.Render(" · "))
}

// cellBody is the session's own screen, or the reason there isn't one.
func (m *Mosaic) cellBody(instance *session.Instance, width, height int, dimmed bool, caretX, caretY int) string {
	if height < 1 {
		return ""
	}

	if msg := m.idleMessage(instance); msg != "" {
		return mosaicIdleStyle.
			Width(width).
			Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Render(msg)
	}

	content := m.content[contentKey(instance.ID(), m.panelOf(instance))]
	lines := strings.Split(content, "\n")

	// The rows are settled before anything is drawn on them, because the caret is
	// given as a row of the terminal and dropping rows off the top moves every
	// row under it.
	if over := len(lines) - height; over > 0 {
		// The session is sized to the cell, so extra lines mean the terminal
		// was just resized and the capture has not caught up. Keep the newest.
		lines = lines[over:]
		caretY -= over
	} else {
		lines = append(lines, make([]string, -over)...)
	}

	// Truncating each line is what keeps a cell one cell tall. Padding to the
	// full width on the way out keeps the borders straight.
	fit := lipgloss.NewStyle().MaxWidth(width).Width(width)
	for i, line := range lines {
		if dimmed {
			line = dim(line)
		}
		if i == caretY && caretX >= 0 && caretX < width {
			line = paintCaret(line, caretX)
		}
		lines[i] = fit.Render(line)
	}
	return strings.Join(lines, "\n")
}

// idleMessage returns why a cell has no live terminal to show, or empty when it
// does. The wording matches the preview pane on purpose. A dead agent only
// blanks its own panel: the shell of that worktree is still usable.
func (m *Mosaic) idleMessage(instance *session.Instance) string {
	switch {
	case instance.Status == session.Loading:
		return "Preparando o ambiente de trabalho..."
	case instance.Paused():
		return "Sessão pausada. Pressione 'r' para retomar."
	case !instance.Started():
		return "Sessão ainda não iniciada."
	case instance.HasExited() && m.panelOf(instance) == PreviewTab:
		return "O agente saiu por conta própria. Pressione 'r' para subir de novo."
	}
	return ""
}
