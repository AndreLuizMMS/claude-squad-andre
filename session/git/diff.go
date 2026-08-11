package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// DiffStats holds statistics about the changes in a diff
type DiffStats struct {
	// Content is the full diff content
	Content string
	// Added is the number of added lines
	Added int
	// Removed is the number of removed lines
	Removed int
	// Error holds any error that occurred during diff computation
	// This allows propagating setup errors (like missing base commit) without breaking the flow
	Error error
}

func (d *DiffStats) IsEmpty() bool {
	return d.Added == 0 && d.Removed == 0 && d.Content == ""
}

// ErrNoDiffBase is returned when a directory has no version history to compare
// against. It is not a failure — sessions work on plain directories too.
var ErrNoDiffBase = errors.New("no comparison base available (directory is not versioned)")

// DirectDiff returns the uncommitted changes present in dir right now, without
// touching the index: it never runs `git add -N`, because the directory belongs
// to the developer and the coordinator must not act on versioning. Untracked
// files are therefore not included.
//
// Note the changes shown are not exclusive to the agent — any edit in the
// directory appears here.
func DirectDiff(dir string, numstatOnly bool) *DiffStats {
	stats := &DiffStats{}

	if !IsGitRepo(dir) {
		stats.Error = ErrNoDiffBase
		return stats
	}
	// An empty repository has no HEAD to compare against.
	if err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		stats.Error = ErrNoDiffBase
		return stats
	}

	args := []string{"-C", dir, "--no-pager", "diff", "HEAD"}
	if numstatOnly {
		args = append(args, "--numstat")
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		stats.Error = fmt.Errorf("failed to diff %s: %w", dir, err)
		return stats
	}
	content := string(out)

	if numstatOnly {
		stats.Added, stats.Removed = parseNumstat(content)
		return stats
	}

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			stats.Added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			stats.Removed++
		}
	}
	stats.Content = content
	return stats
}

// parseNumstat sums the added/removed columns from `git diff --numstat` output.
// Each line is formatted as <added>\t<removed>\t<path>. Binary files report
// "-\t-\t<path>" and are ignored for line totals.
func parseNumstat(out string) (added int, removed int) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			continue
		}
		a, aerr := strconv.Atoi(fields[0])
		r, rerr := strconv.Atoi(fields[1])
		if aerr != nil || rerr != nil {
			continue
		}
		added += a
		removed += r
	}
	return added, removed
}
