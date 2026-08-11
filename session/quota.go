package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// quotaStaleAfter bounds how old the quota file may be before we stop showing
// it. Claude Code only reports the quota while a session is drawing its
// statusline, so a stale file means nothing has been running for a while and
// the number can no longer be trusted.
const quotaStaleAfter = 15 * time.Minute

// Quota is the account-wide 5-hour rate-limit usage Claude Code reports.
type Quota struct {
	// Percent is how much of the 5-hour window is used, 0-100.
	Percent int
	// ResetsAt is when the window rolls over. Zero when unknown.
	ResetsAt time.Time
}

// quotaFile is where the statusline hook drops the numbers. Claude Code hands
// the 5-hour quota to the statusline command and persists it nowhere else, so
// that hook is the only place it can be captured from outside a session.
func quotaFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "squad-quota.json"), nil
}

// ReadQuota returns the last 5-hour quota reading written by the statusline
// hook, or nil when it is missing or stale.
func ReadQuota() *Quota {
	path, err := quotaFile()
	if err != nil {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > quotaStaleAfter {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var payload struct {
		UsedPercentage float64 `json:"used_percentage"`
		ResetsAt       int64   `json:"resets_at"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}

	q := &Quota{Percent: int(payload.UsedPercentage + 0.5)}
	if q.Percent > 100 {
		q.Percent = 100
	}
	if payload.ResetsAt > 0 {
		q.ResetsAt = time.Unix(payload.ResetsAt, 0)
	}
	return q
}
