package session

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func writeQuotaFile(t *testing.T, body string, age time.Duration) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "squad-quota.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestReadQuota(t *testing.T) {
	resets := time.Now().Add(90 * time.Minute).Unix()
	writeQuotaFile(t, `{"used_percentage":42.7,"resets_at":`+strconv.FormatInt(resets, 10)+`}`, time.Minute)

	q := ReadQuota()
	if q == nil {
		t.Fatal("expected a quota reading")
	}
	if q.Percent != 43 {
		t.Errorf("percent = %d, want 43", q.Percent)
	}
	if q.ResetsAt.Unix() != resets {
		t.Errorf("resetsAt = %v, want %v", q.ResetsAt.Unix(), resets)
	}
}

func TestReadQuotaStale(t *testing.T) {
	writeQuotaFile(t, `{"used_percentage":42,"resets_at":0}`, time.Hour)
	if q := ReadQuota(); q != nil {
		t.Errorf("stale file should read as no quota, got %+v", q)
	}
}

func TestReadQuotaMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if q := ReadQuota(); q != nil {
		t.Errorf("missing file should read as no quota, got %+v", q)
	}
}
