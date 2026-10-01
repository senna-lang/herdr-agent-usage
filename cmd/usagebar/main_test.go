// Command-level tests for usagebar notification configuration.
package main

import (
	"encoding/json"
	"io"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"os"
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
	"github.com/senna-lang/herdr-agent-usage/internal/setup"
)

// TestNotificationsEnabledHonorsPluginConfig ensures both notification entrypoints
// use the documented [notify].enabled switch.
func TestNotificationsEnabledHonorsPluginConfig(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[notify]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if notificationsEnabled(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": configDir}) {
		t.Fatal("notifications must be disabled by config")
	}
}

func TestUpdateNotificationHonorsPluginConfig(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[notify]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if updateNotification(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": configDir}) != nil {
		t.Fatal("update toast must be disabled by config")
	}
}

// TestStatusLineNotificationsDeduplicatesEveryTick reproduces issue #32's
// once-per-second statusLine calls after entering the 50% remaining bucket.
func TestStatusLineNotificationsDeduplicatesEveryTick(t *testing.T) {
	profile := claude.ClaudeProfile{StateDir: t.TempDir()}
	config := setup.PluginConfig{NotifyEnabled: true, RemainingThresholds: []int{50, 20, 10, 5}}
	payload := `{"rate_limits":{"five_hour":{"used_percentage":55,"resets_at":1800000000}}}`
	notifications := 0
	notify := ratelimit.ShowNotificationFn(func(_, _ string) bool {
		notifications++
		return true
	})

	for nowMs := int64(1_700_000_000_000); nowMs < 1_700_000_003_000; nowMs += 1_000 {
		runStatusLineNotifications(profile, payload, nowMs, config, notify)
	}

	if notifications != 1 {
		t.Fatalf("statusline sent %d notifications, want exactly one", notifications)
	}
	raw, err := os.ReadFile(filepath.Join(profile.StateDir, "rate-limit-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		FiveHour *struct {
			NotifiedBucket *string `json:"notifiedBucket"`
		} `json:"fiveHour"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.FiveHour == nil || state.FiveHour.NotifiedBucket == nil || *state.FiveHour.NotifiedBucket != "50" {
		t.Fatalf("notified bucket was not persisted: %s", raw)
	}
}

// TestStatusLineNotificationsDisabled reproduces issue #32's documented
// notify.enabled=false switch for the same threshold-crossing statusline input.
func TestStatusLineNotificationsDisabled(t *testing.T) {
	profile := claude.ClaudeProfile{StateDir: t.TempDir()}
	config := setup.PluginConfig{NotifyEnabled: false, RemainingThresholds: []int{50, 20, 10, 5}}
	notifications := 0

	runStatusLineNotifications(
		profile,
		`{"rate_limits":{"five_hour":{"used_percentage":55,"resets_at":1800000000}}}`,
		1_700_000_000_000,
		config,
		func(_, _ string) bool {
			notifications++
			return true
		},
	)

	if notifications != 0 {
		t.Fatalf("disabled notifications sent %d toasts", notifications)
	}
}

// A quota dashboard must not wait for Herdr or read pane activity transcripts.
// Legacy collection and active-provider filtering still query Herdr.
func TestCollectQuotasOnly(t *testing.T) {
	for _, tc := range []struct {
		name               string
		args               []string
		wantPaneQuery      bool
		herdrReply         string
		wantEmptyProviders bool
	}{
		{"all quotas", []string{"--all", "--quotas-only"}, false, "", false},
		{"active quotas", []string{"--quotas-only"}, true, "", false},
		{"legacy full collection", []string{"--all"}, true, "", false},
		{"active quotas with no panes", []string{"--quotas-only"}, true, `{"result":{"panes":[]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
			t.Setenv("HERDR_PLUGIN_CONFIG_DIR", t.TempDir())
			t.Setenv("USAGEBAR_HISTORY_PATH", filepath.Join(home, "history.json"))
			marker := filepath.Join(home, "herdr-called")
			t.Setenv("USAGEBAR_TEST_HERDR_LOG", marker)
			t.Setenv("USAGEBAR_TEST_HERDR_REPLY", tc.herdrReply)
			herdr := filepath.Join(home, "herdr")
			if err := os.WriteFile(herdr, []byte("#!/bin/sh\nprintf 'called\\n' >> \"$USAGEBAR_TEST_HERDR_LOG\"\nif [ -n \"$USAGEBAR_TEST_HERDR_REPLY\" ]; then printf '%s\\n' \"$USAGEBAR_TEST_HERDR_REPLY\"; exit 0; fi\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HERDR_BIN_PATH", herdr)
			cache := limits.ResolvedClaudeProfiles()[0].LimitsCache
			if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
				t.Fatal(err)
			}
			nowMs := time.Now().UnixMilli()
			raw, err := json.Marshal(map[string]any{
				"fetchedAtMs": nowMs,
				"fiveHour":    map[string]any{"usedPercentage": 32, "resetsAtMs": nowMs + 3_600_000},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cache, raw, 0o644); err != nil {
				t.Fatal(err)
			}

			output := captureCollectJSON(t, tc.args)
			var got struct {
				Providers []limits.ProviderLimits   `json:"providers"`
				APIUsage  []limits.APIProviderUsage `json:"apiUsage"`
			}
			if err := json.Unmarshal(output, &got); err != nil {
				t.Fatal(err)
			}
			foundQuota := false
			for _, provider := range got.Providers {
				if provider.Primary != nil && provider.Primary.UsedPercentage == 32 {
					foundQuota = true
				}
				if provider.PaneActivity != nil {
					t.Fatal("unexpected pane activity")
				}
			}
			if tc.wantEmptyProviders {
				if len(got.Providers) != 0 {
					t.Fatal("inactive providers were collected")
				}
			} else if !foundQuota {
				t.Fatalf("cached quota was lost: %s", output)
			}
			if len(got.APIUsage) != 0 {
				t.Fatal("unexpected API activity")
			}
			_, err = os.Stat(marker)
			if didQuery := err == nil; didQuery != tc.wantPaneQuery {
				t.Fatalf("queried Herdr=%v, want %v", didQuery, tc.wantPaneQuery)
			}
		})
	}
}

func captureCollectJSON(t *testing.T, args []string) []byte {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	defer func() { os.Stdout = original }()
	os.Stdout = writer
	runCollectJSON(args)
	writer.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
