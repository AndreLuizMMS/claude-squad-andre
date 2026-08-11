package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const readyIcon = "● "
const pausedIcon = "⏸ "
const orphanedIcon = "⚠ "
const exitedIcon = "✖ "

// attentionBadge marks a session that answered while the developer was looking
// somewhere else. It is meant to be impossible to miss in a list of agents.
const attentionBadge = " ⬤ RESPONDEU "

// approvalBadge marks a session stopped on a question. It reads differently
// from an answer on purpose: an answer waits for you, this one blocks the
// agent until you get to it.
const approvalBadge = " ⏵ APROVAR "

var readyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var pausedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

var orphanedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var attentionBadgeStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#1a1a1a")).
	Background(lipgloss.Color("#ffd23f"))

var attentionTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#ffd23f"))

var approvalBadgeStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#ffffff")).
	Background(lipgloss.Color("#d1495b"))

var approvalTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#ff6b81"))

var titleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#1a1a1a"))

var groupHeaderStyle = lipgloss.NewStyle().
	Padding(0, 1).
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#655F5F", Dark: "#8a8a8a"})

var quotaStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#ffd23f"))

var quotaWarnStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

type List struct {
	items         []*session.Instance
	selectedIdx   int
	height, width int
	renderer      *InstanceRenderer
	autoyes       bool

	// quota is the account-wide 5-hour usage, shown next to the list title
	// because it is shared by every session rather than owned by one.
	quota *session.Quota

	// map of repo name to number of instances using it. Used to display the repo name only if there are
	// multiple repos in play.
	repos map[string]int
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:    []*session.Instance{},
		renderer: &InstanceRenderer{spinner: spinner},
		repos:    make(map[string]int),
		autoyes:  autoYes,
	}
}

// SetSize sets the height and width of the list.
// SetNewInstanceHint sets the line shown under the session being created. Empty
// clears it.
func (l *List) SetNewInstanceHint(hint string) {
	l.renderer.newInstanceHint = hint
}

// SetQuota sets the 5-hour usage shown in the list header. Nil hides it.
func (l *List) SetQuota(q *session.Quota) {
	l.quota = q
}

func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	l.renderer.setWidth(width)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.items {
		if !item.Started() || item.Paused() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

func (l *List) NumInstances() int {
	return len(l.items)
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
	// newInstanceHint is the creation-form line shown under a session that has
	// not started yet.
	newInstanceHint string
}

func (r *InstanceRenderer) setWidth(width int) {
	r.width = AdjustPreviewWidth(width)
}

// dirIcon marks the second line, which holds the session's working directory.
const dirIcon = "▸"

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool) string {
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}
	switch {
	case i.NeedsApproval:
		titleS = titleS.Foreground(approvalTitleStyle.GetForeground()).Bold(true)
	case i.NeedsAttention && i.Status == session.Ready:
		titleS = titleS.Foreground(attentionTitleStyle.GetForeground()).Bold(true)
	}

	// add spinner next to title if it's running. markerWidth is how many columns
	// the marker occupies, so the title can be given the rest.
	var join string
	markerWidth := 2
	switch {
	case i.NeedsApproval:
		join = approvalBadgeStyle.Render(approvalBadge)
		markerWidth = runewidth.StringWidth(approvalBadge)
	case i.NeedsAttention && i.Status == session.Ready:
		join = attentionBadgeStyle.Render(attentionBadge)
		markerWidth = runewidth.StringWidth(attentionBadge)
	case i.Status == session.Running, i.Status == session.Loading:
		join = fmt.Sprintf("%s ", r.spinner.View())
	case i.Status == session.Ready:
		join = readyStyle.Render(readyIcon)
	case i.Status == session.Paused:
		join = pausedStyle.Render(pausedIcon)
	case i.Status == session.Orphaned:
		join = orphanedStyle.Render(orphanedIcon)
	case i.Status == session.Exited:
		join = orphanedStyle.Render(exitedIcon)
	}

	// Cut the title if it's too long. The marker takes its columns first: an
	// alert nobody can read is not an alert.
	titleArea := r.width - 1 - markerWidth
	if titleArea < 1 {
		titleArea = 1
	}
	titleText := i.Title
	widthAvail := titleArea - runewidth.StringWidth(prefix) - 1
	if widthAvail > 0 && runewidth.StringWidth(titleText) > widthAvail {
		titleText = runewidth.Truncate(titleText, widthAvail, "...")
	}
	title := titleS.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.Place(titleArea, 1, lipgloss.Left, lipgloss.Center, fmt.Sprintf("%s %s", prefix, titleText)),
		" ",
		join,
	))

	stat := i.GetDiffStats()

	var diff string
	var addedDiff, removedDiff string
	if stat == nil || stat.Error != nil || stat.IsEmpty() {
		// Don't show diff stats if there's an error or if they don't exist
		addedDiff = ""
		removedDiff = ""
		diff = ""
	} else {
		addedDiff = fmt.Sprintf("+%d", stat.Added)
		removedDiff = fmt.Sprintf("-%d ", stat.Removed)
		diff = lipgloss.JoinHorizontal(
			lipgloss.Center,
			addedLinesStyle.Background(descS.GetBackground()).Render(addedDiff),
			lipgloss.Style{}.Background(descS.GetBackground()).Foreground(descS.GetForeground()).Render(","),
			removedLinesStyle.Background(descS.GetBackground()).Render(removedDiff),
		)
	}

	remainingWidth := r.width
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(dirIcon)
	remainingWidth -= 2 // for the literal " " and "-" in the dirLine format string

	diffWidth := runewidth.StringWidth(addedDiff) + runewidth.StringWidth(removedDiff)
	if diffWidth > 0 {
		diffWidth += 1
	}
	// Use fixed width for diff stats to avoid layout issues
	remainingWidth -= diffWidth

	// The second line is the working directory: it is what distinguishes one
	// session from another when several agents run side by side.
	subtitle := i.Path
	if !i.Started() && r.newInstanceHint != "" {
		// Session being created: the second line is the creation form.
		subtitle = r.newInstanceHint
	} else if i.Status == session.Orphaned {
		// Show the path that went missing so the state explains itself.
		subtitle = "ausente: " + i.Path
	} else if i.Status == session.Exited {
		// The directory is fine; the agent is the part that is gone. Say which
		// key brings it back, because the state looks like a dead end.
		subtitle = "agente caiu — r para subir de novo"
	}

	// Don't show it if there's no space. Or show ellipsis if it's too long.
	subtitleWidth := runewidth.StringWidth(subtitle)
	if !i.Started() && r.newInstanceHint != "" {
		// While typing a path, the tail is what matters — the segment being
		// completed. Cut from the left instead of the right.
		subtitle = truncateLeft(subtitle, remainingWidth)
	} else if remainingWidth < 0 {
		subtitle = ""
	} else if remainingWidth < subtitleWidth {
		if remainingWidth < 3 {
			subtitle = ""
		} else {
			// We know the remainingWidth is at least 4 and the subtitle is longer than that, so this is safe.
			subtitle = runewidth.Truncate(subtitle, remainingWidth-3, "...")
		}
	}
	remainingWidth -= runewidth.StringWidth(subtitle)

	// Add spaces to fill the remaining width.
	spaces := ""
	if remainingWidth > 0 {
		spaces = strings.Repeat(" ", remainingWidth)
	}

	dirLine := fmt.Sprintf("%s %s-%s%s%s", strings.Repeat(" ", len(prefix)), dirIcon, subtitle, spaces, diff)

	// join title and subtitle
	text := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		descS.Render(dirLine),
	)

	return text
}

// quotaBadge renders the 5-hour usage plus the time left until it resets.
// Empty when there is no fresh reading.
func (l *List) quotaBadge() string {
	if l.quota == nil {
		return ""
	}
	text := fmt.Sprintf("⏳ %d%%", l.quota.Percent)
	if left := time.Until(l.quota.ResetsAt); left > 0 {
		left = left.Round(time.Minute)
		text += fmt.Sprintf(" %d:%02d", int(left.Hours()), int(left.Minutes())%60)
	}
	style := quotaStyle
	if l.quota.Percent >= 80 {
		style = quotaWarnStyle
	}
	return style.Render(text + " ")
}

// groupHeader returns the project header to draw above an item, and whether it
// should be drawn at all. There is nothing to separate when every session lives
// in the same directory, and a session still being created has no directory yet.
func (l *List) groupHeader(item *session.Instance, current string) (string, bool) {
	if len(l.repos) < 2 || !item.Started() {
		return "", false
	}
	name := item.DirName()
	if name == current {
		return "", false
	}
	return name, true
}

func (l *List) String() string {
	const titleText = " Sessões "
	const autoYesText = " auto-yes "

	// Write the title.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	var badges []string
	if quota := l.quotaBadge(); quota != "" {
		badges = append(badges, quota)
	}
	if l.autoyes {
		badges = append(badges, autoYesStyle.Render(autoYesText))
	}
	if len(badges) == 0 {
		b.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		right := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom,
			lipgloss.JoinHorizontal(lipgloss.Bottom, badges...))
		b.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, right))
	}

	b.WriteString("\n")
	b.WriteString("\n")

	// Render the list, breaking it into projects. The header repeats whenever
	// the directory changes, so reordering sessions by hand keeps working
	// instead of being fought by the grouping.
	group := ""
	for i, item := range l.items {
		if header, ok := l.groupHeader(item, group); ok {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(groupHeaderStyle.Render(header))
			b.WriteString("\n")
			group = header
		}
		b.WriteString(l.renderer.Render(item, i+1, i == l.selectedIdx, len(l.repos) > 1))
		if i != len(l.items)-1 {
			b.WriteString("\n\n")
		}
	}
	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, b.String())
}

// Down selects the next item in the list.
func (l *List) Down() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx < len(l.items)-1 {
		l.selectedIdx++
	} else {
		l.selectedIdx = 0
	}
}

// Kill selects the next item in the list.
func (l *List) Kill() {
	if len(l.items) == 0 {
		return
	}
	targetInstance := l.items[l.selectedIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// If you delete the last one in the list, select the previous one.
	if l.selectedIdx == len(l.items)-1 {
		defer l.Up()
	}

	// Unregister the directory name.
	l.rmRepo(targetInstance.DirName())

	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
}

func (l *List) Attach() (chan struct{}, error) {
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// SelectNextAttention selects the next session waiting on the developer,
// wrapping around the list. It reports whether it found one: with a dozen
// agents running, hunting the badge with the arrow keys is the slow part of
// the job.
//
// Blocked sessions come first, wherever they are in the list. An agent stopped
// on a question makes no progress until it is answered, while an answer waiting
// to be read costs nothing to leave sitting.
func (l *List) SelectNextAttention() bool {
	blocked := func(i *session.Instance) bool { return i.NeedsApproval }
	answered := func(i *session.Instance) bool {
		return i.NeedsAttention && i.Status == session.Ready
	}
	return l.selectNext(blocked) || l.selectNext(answered)
}

// selectNext moves the selection to the next item after the current one that
// matches, wrapping around. It leaves the selection alone when nothing matches.
func (l *List) selectNext(match func(*session.Instance) bool) bool {
	for offset := 1; offset <= len(l.items); offset++ {
		idx := (l.selectedIdx + offset) % len(l.items)
		if match(l.items[idx]) {
			l.selectedIdx = idx
			return true
		}
	}
	return false
}

// Up selects the prev item in the list.
func (l *List) Up() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx > 0 {
		l.selectedIdx--
	} else {
		l.selectedIdx = len(l.items) - 1
	}
}

func (l *List) addRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		l.repos[repo] = 0
	}
	l.repos[repo]++
}

func (l *List) rmRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		log.ErrorLog.Printf("repo %s not found", repo)
		return
	}
	l.repos[repo]--
	if l.repos[repo] == 0 {
		delete(l.repos, repo)
	}
}

// AddInstance adds a new instance to the list. It returns a finalizer function that should be called when the instance
// is started. If the instance was restored from storage or is paused, you can call the finalizer immediately.
// When creating a new one and entering the name, you want to call the finalizer once the name is done.
func (l *List) AddInstance(instance *session.Instance) (finalize func()) {
	l.items = append(l.items, instance)
	// The finalizer registers the directory name once the instance is started.
	return func() {
		repoName := instance.DirName()

		l.addRepo(repoName)
	}
}

// GetSelectedInstance returns the currently selected instance
func (l *List) GetSelectedInstance() *session.Instance {
	if len(l.items) == 0 {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SetSelectedInstance sets the selected index. Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the list.
func (l *List) SelectInstance(target *session.Instance) {
	for i, inst := range l.items {
		if inst == target {
			l.SetSelectedInstance(i)
			return
		}
	}
}

// MoveUp swaps the selected instance with the one above it.
func (l *List) MoveUp() bool {
	if l.selectedIdx <= 0 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx-1] = l.items[l.selectedIdx-1], l.items[l.selectedIdx]
	l.selectedIdx--
	return true
}

// MoveDown swaps the selected instance with the one below it.
func (l *List) MoveDown() bool {
	if l.selectedIdx >= len(l.items)-1 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx+1] = l.items[l.selectedIdx+1], l.items[l.selectedIdx]
	l.selectedIdx++
	return true
}

// GetInstances returns all instances in the list
func (l *List) GetInstances() []*session.Instance {
	return l.items
}

// truncateLeft keeps the end of s, dropping the front with a leading ellipsis
// when it does not fit in width columns.
func truncateLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	runes := []rune(s)
	for i := range runes {
		tail := string(runes[i:])
		if runewidth.StringWidth(tail)+3 <= width {
			return "..." + tail
		}
	}
	return "..."
}
