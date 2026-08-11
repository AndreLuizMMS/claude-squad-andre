package ui

import (
	"claude-squad/session"
	"strings"
	"testing"
	"time"
)

func TestQuotaBadgeInHeader(t *testing.T) {
	l := NewList(nil, false)
	l.SetSize(80, 20)
	if strings.Contains(l.String(), "⏳") {
		t.Fatal("no quota should render no badge")
	}
	l.SetQuota(&session.Quota{Percent: 43, ResetsAt: time.Now().Add(97 * time.Minute)})
	out := l.String()
	if !strings.Contains(out, "⏳ 43% 1:37") {
		t.Errorf("header missing quota badge:\n%q", out)
	}
}
