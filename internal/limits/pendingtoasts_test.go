package limits

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// isolatePendingToastState points every notify state file and the profile
// config at temp dirs and configures two Claude accounts.
func isolatePendingToastState(t *testing.T) (workDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USAGEBAR_STATE_DIR", filepath.Join(home, "state"))
	t.Setenv("USAGEBAR_PROVIDER_NOTIFY_PATH", filepath.Join(home, "provider-state.json"))
	pluginConfigDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir)
	personal := filepath.Join(home, ".claude")
	workDir = filepath.Join(home, ".claude-work")
	toml := "[[claude.profiles]]\nid = \"claude\"\nlabel = \"personal\"\nconfig_dir = \"" + personal + "\"\n\n" +
		"[[claude.profiles]]\nid = \"claude-work\"\nlabel = \"work\"\nconfig_dir = \"" + workDir + "\"\n"
	if err := os.WriteFile(filepath.Join(pluginConfigDir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	return workDir
}

// Every due toast comes back once, titled with its account and levelled by
// its bucket, and is recorded so the next call (or herdr delivery) is quiet.
func TestCollectPendingToasts_ReturnsEachDueToastOnce(t *testing.T) {
	workDir := isolatePendingToastState(t)
	resets := notifyResetsAt
	providers := []ProviderLimits{
		{ProviderID: "claude-work", Label: "work", ProfileLabel: "work", Primary: &LimitWindow{UsedPercentage: 85, ResetsAt: &resets}},
		{ProviderID: "claude", Label: "personal", ProfileLabel: "personal", Secondary: &LimitWindow{UsedPercentage: 10, ResetsAt: &resets}},
		notifyTestProvider(func(p *ProviderLimits) {
			p.Primary.UsedPercentage = 92
			p.GroupLabel, p.ProfileLabel = "Codex", "personal"
		}),
	}

	got := CollectPendingToasts(providers, notifyNowMs, nil, core.LimitPercentRemaining)

	if got.Version != CollectReportVersion || len(got.Toasts) != 2 {
		t.Fatalf("report = %+v, want two toasts", got)
	}
	work, codex := got.Toasts[0], got.Toasts[1]
	if work.ProviderID != "claude-work" || work.Window != "primary" || work.Bucket != 20 || work.Level != ToastWarning || work.Title != "Session limit · work" {
		t.Fatalf("claude toast = %+v", work)
	}
	if codex.ProviderID != "codex" || codex.Bucket != 10 || codex.Level != ToastError || codex.Title != "Codex limit · personal" || codex.Body != "10% remaining · resets in 2h 0m" {
		t.Fatalf("codex toast = %+v", codex)
	}
	// The Claude decision used the profile's own state dir, the one the
	// statusLine bridge for that account reads.
	if _, err := os.Stat(filepath.Join(workDir, "herdr-usagebar", "rate-limit-state.json")); err != nil {
		t.Fatalf("work profile state not recorded: %v", err)
	}

	again := CollectPendingToasts(providers, notifyNowMs, nil, core.LimitPercentRemaining)
	if len(again.Toasts) != 0 {
		t.Fatalf("toasts repeated: %+v", again.Toasts)
	}
}

func TestCollectPendingToasts_NothingDueIsAnEmptyList(t *testing.T) {
	isolatePendingToastState(t)
	got := CollectPendingToasts([]ProviderLimits{notifyTestProvider(func(p *ProviderLimits) { p.Primary.UsedPercentage = 5 })}, notifyNowMs, nil, core.LimitPercentRemaining)
	if got.Toasts == nil || len(got.Toasts) != 0 {
		t.Fatalf("toasts = %#v, want an empty list", got.Toasts)
	}
}
