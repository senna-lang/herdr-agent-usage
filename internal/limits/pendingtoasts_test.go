package limits

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
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

// A Claude entry that shares an account with another dir is skipped, so the
// same crossing is not toasted once per dir.
func TestCollectClaudeToasts_SkipsEntriesSharingAnAccount(t *testing.T) {
	isolatePendingToastState(t)
	resets := notifyResetsAt
	profiles := ResolvedClaudeProfiles()
	providers := []ProviderLimits{
		{ProviderID: "claude-work", ProfileLabel: "work", SameAccountAs: "claude", Primary: &LimitWindow{UsedPercentage: 85, ResetsAt: &resets}},
	}

	if got := collectClaudeToasts(providers, profiles, notifyNowMs, nil, core.LimitPercentRemaining); len(got) != 0 {
		t.Fatalf("toasts for a shared-account entry: %+v", got)
	}

	providers[0].SameAccountAs = ""
	if got := collectClaudeToasts(providers, profiles, notifyNowMs, nil, core.LimitPercentRemaining); len(got) != 1 {
		t.Fatalf("toasts for the owning entry = %+v, want one", got)
	}
}

func TestToastBucketAndLevel(t *testing.T) {
	for _, tc := range []struct {
		bucket    ratelimit.Bucket
		wantN     int
		wantLevel ToastLevel
	}{
		{"50", 50, ToastWarning},
		{"20", 20, ToastWarning},
		{"10", 10, ToastError},
		{"5", 5, ToastError},
		{"bogus", 0, ToastWarning},
	} {
		n, level := toastBucketAndLevel(tc.bucket)
		if n != tc.wantN || level != tc.wantLevel {
			t.Errorf("toastBucketAndLevel(%q) = %d, %s; want %d, %s", tc.bucket, n, level, tc.wantN, tc.wantLevel)
		}
	}
}
