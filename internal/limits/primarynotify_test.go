/**
 * Tests for CheckProviderPrimaryLimits.
 */
package limits

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
)

var defaultClaudeProfiles = []claude.ClaudeProfile{{ID: "claude"}}

const (
	notifyNowMs    int64 = 1_800_000_000_000
	notifyResetsAt       = notifyNowMs/1000 + 2*60*60
)

func notifyTestProvider(overrides func(*ProviderLimits)) ProviderLimits {
	wm := 300
	r := notifyResetsAt
	p := ProviderLimits{
		ProviderID:  "codex",
		Label:       "Codex",
		Primary:     &LimitWindow{UsedPercentage: 55, ResetsAt: &r, WindowMinutes: &wm},
		Source:      "test",
		FetchedAtMs: notifyNowMs,
	}
	if overrides != nil {
		overrides(&p)
	}
	return p
}

func TestCheckProviderPrimaryLimits_Notifies(t *testing.T) {
	var notifications []string
	next := CheckProviderPrimaryLimits(
		[]ProviderLimits{notifyTestProvider(nil)},
		ProviderNotifyState{},
		notifyNowMs,
		func(title, body string) bool {
			notifications = append(notifications, title+": "+body)
			return true
		},
		defaultClaudeProfiles,
	)
	if len(notifications) != 1 || notifications[0] != "Codex limit: 50% remaining · resets in 2h 0m" {
		t.Fatalf("notifications=%v", notifications)
	}
	if next["codex"] == nil || next["codex"].NotifiedBucket == nil || *next["codex"].NotifiedBucket != ratelimit.Bucket50 {
		t.Fatalf("next=%+v", next["codex"])
	}
}

func TestCheckProviderPrimaryLimits_NoRenotify(t *testing.T) {
	b := ratelimit.Bucket50
	current := ProviderNotifyState{
		"codex": &ratelimit.WindowState{ResetsAt: notifyResetsAt, NotifiedBucket: &b, FailedNotifyAttempts: 0},
	}
	next := CheckProviderPrimaryLimits(
		[]ProviderLimits{notifyTestProvider(nil)},
		current,
		notifyNowMs,
		func(title, body string) bool {
			t.Fatal("should not notify")
			return false
		},
		defaultClaudeProfiles,
	)
	if next["codex"] == nil || next["codex"].NotifiedBucket == nil || *next["codex"].NotifiedBucket != ratelimit.Bucket50 {
		t.Fatalf("next=%+v", next["codex"])
	}
}

func TestCheckProviderPrimaryLimits_IgnoresClaudeAndSecondary(t *testing.T) {
	wm := 300
	r := notifyResetsAt
	claudeProvider := notifyTestProvider(func(p *ProviderLimits) {
		p.ProviderID = "claude"
		p.Label = "Claude"
		p.Primary = &LimitWindow{UsedPercentage: 99, ResetsAt: &r, WindowMinutes: &wm}
	})
	codexNoPrimary := notifyTestProvider(func(p *ProviderLimits) {
		p.Primary = nil
		sec := 10080
		p.Secondary = &LimitWindow{UsedPercentage: 99, ResetsAt: &r, WindowMinutes: &sec}
	})
	var notifications []string
	next := CheckProviderPrimaryLimits(
		[]ProviderLimits{claudeProvider, codexNoPrimary},
		ProviderNotifyState{},
		notifyNowMs,
		func(title, body string) bool {
			notifications = append(notifications, title)
			return true
		},
		defaultClaudeProfiles,
	)
	if len(notifications) != 0 {
		t.Fatalf("notifications=%v", notifications)
	}
	// codex without primary keeps previous (nil)
	if next["codex"] != nil {
		t.Fatalf("expected nil codex state, got %+v", next["codex"])
	}
}

func TestCheckProviderPrimaryLimits_IgnoresConfiguredSecondaryProfile(t *testing.T) {
	wm := 300
	r := notifyResetsAt
	secondary := notifyTestProvider(func(p *ProviderLimits) {
		p.ProviderID = "claude-secondary"
		p.Label = "Claude (secondary)"
		p.Primary = &LimitWindow{UsedPercentage: 99, ResetsAt: &r, WindowMinutes: &wm}
	})
	profiles := []claude.ClaudeProfile{{ID: "claude"}, {ID: "claude-secondary"}}
	var notifications []string
	CheckProviderPrimaryLimits(
		[]ProviderLimits{secondary},
		ProviderNotifyState{},
		notifyNowMs,
		func(title, body string) bool {
			notifications = append(notifications, title)
			return true
		},
		profiles,
	)
	if len(notifications) != 0 {
		t.Fatalf("claude-secondary must not double-notify through the generic loop: %v", notifications)
	}
}
