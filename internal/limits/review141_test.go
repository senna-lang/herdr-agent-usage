/**
 * PR141 review regressions for same-account sharing and in-use evidence.
 *
 * These tests exercise the collectors and CollectAllProviderLimits. They do
 * not assert source text. Cache fixtures write the wire format directly so
 * provenance is checked independently of the writer:
 *
 *	statusLine cache: accountEmail + organizationUuid, beside fiveHour/fetchedAtMs
 *	.claude.json:     oauthAccount.emailAddress + oauthAccount.organizationUuid
 *
 * The join key is an opaque complete email+organizationUuid value. It must not
 * be the bare email. Identity and AccountLabel keep the email for display.
 * Untagged caches stay local. A prompt-cache-only rewrite must not drop or
 * relabel a stamped identity. Login metadata alone never proves sameness.
 */
package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

const review141Now int64 = 1_700_000_000_000

func review141QuietCollectors(t *testing.T) {
	t.Helper()
	t.Setenv("USAGEBAR_DISABLE_BROWSER_COOKIES", "1")
	t.Setenv("OPENCODE_GO_COOKIE", "")
	t.Setenv("OPENCODE_DB", filepath.Join(t.TempDir(), "missing.db"))
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("KILO_DATA_DIR", filepath.Join(t.TempDir(), "no-kilo"))
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DEVIN_DATA_DIR", filepath.Join(t.TempDir(), "no-devin"))
	t.Setenv("USAGEBAR_DEVIN_CACHE_PATH", filepath.Join(t.TempDir(), "devin-quota.json"))
	t.Setenv("USAGEBAR_KILO_CACHE_PATH", filepath.Join(t.TempDir(), "kilo-pass.json"))
	t.Setenv("USAGEBAR_OPENCODE_WEB_CACHE_PATH", filepath.Join(t.TempDir(), "opencode-go-web.json"))
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_CONFIG_JSON", filepath.Join(t.TempDir(), "missing-claude.json"))
	t.Setenv("USAGEBAR_CLAUDE_LIMITS_PATH", filepath.Join(t.TempDir(), "missing-cache.json"))
}

func review141WriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func review141Login(email, org string) map[string]any {
	oauth := map[string]any{"emailAddress": email}
	if org != "" {
		oauth["organizationUuid"] = org
	}
	return map[string]any{"oauthAccount": oauth}
}

func review141ProvenanceCache(email, org string, used float64, fetchedAtMs int64) map[string]any {
	payload := map[string]any{
		"fiveHour": map[string]any{
			"usedPercentage": used,
			"windowMinutes":  300,
			"resetsAt":       review141Now/1000 + 7200,
		},
		"fetchedAtMs": fetchedAtMs,
	}
	if email != "" {
		payload["accountEmail"] = email
	}
	if org != "" {
		payload["organizationUuid"] = org
	}
	return payload
}

func review141CachePath(configDir string) string {
	return filepath.Join(configDir, "herdr-usagebar", "claude-limits-latest.json")
}

func review141JSONPath(configDir string) string {
	return filepath.Join(configDir, ".claude.json")
}

// review141ConfigureClaude points two or more explicit profiles at temp dirs.
// claude_json_path is set explicitly so the ~/.claude sibling rule cannot
// attach a profile to another dir's login file.
func review141ConfigureClaude(t *testing.T, home string, profiles []struct{ id, label, dir string }) {
	t.Helper()
	plugin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", plugin)
	var b strings.Builder
	for _, p := range profiles {
		b.WriteString("[[claude.profiles]]\n")
		b.WriteString("id = \"" + p.id + "\"\n")
		b.WriteString("label = \"" + p.label + "\"\n")
		b.WriteString("config_dir = \"" + p.dir + "\"\n")
		b.WriteString("claude_json_path = \"" + review141JSONPath(p.dir) + "\"\n\n")
	}
	if err := os.WriteFile(filepath.Join(plugin, "config.toml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func review141Collect(now int64, only map[string]bool) []ProviderLimits {
	opts := DefaultCollectOptions()
	opts.Only = only
	return CollectAllProviderLimits(nil, now, opts)
}

func review141Entry(t *testing.T, entries []ProviderLimits, id string) ProviderLimits {
	t.Helper()
	for _, e := range entries {
		if e.ProviderID == id {
			return e
		}
	}
	t.Fatalf("missing %q", id)
	return ProviderLimits{}
}

func review141Used(p ProviderLimits) float64 {
	if p.Primary == nil {
		return -1
	}
	return p.Primary.UsedPercentage
}

func review141Sidebar(p ProviderLimits) string {
	return FormatSidebarLimit(p, review141Now, core.LimitPercentUsed)
}

func review141SameAccountOutside(entries []ProviderLimits) string {
	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.ProviderID] = true
	}
	for _, e := range entries {
		if e.SameAccountAs != "" && !ids[e.SameAccountAs] {
			return e.ProviderID + " -> " + e.SameAccountAs
		}
	}
	return ""
}

// TestReview141ProvenSameAccountShares is the positive control: two profiles
// with the same email and organizationUuid, and statusLine caches stamped
// with that identity, share the fresher windows. The join key is not the email.
func TestReview141ProvenSameAccountShares(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const (
		email = "a@x.com"
		org   = "11111111-1111-4111-8111-111111111111"
	)
	review141WriteJSON(t, review141JSONPath(personal), review141Login(email, org))
	review141WriteJSON(t, review141JSONPath(work), review141Login(email, org))
	review141WriteJSON(t, review141CachePath(personal), review141ProvenanceCache(email, org, 15, review141Now-60_000))
	review141WriteJSON(t, review141CachePath(work), review141ProvenanceCache(email, org, 80, review141Now-20*60_000))

	got := review141Collect(review141Now, nil)
	base := review141Entry(t, got, "claude")
	dedicated := review141Entry(t, got, "work")
	if review141Used(base) != 15 || review141Used(dedicated) != 15 {
		t.Errorf("used = %v/%v, want the fresher proven 15%% on both rows", review141Used(base), review141Used(dedicated))
	}
	if !strings.Contains(dedicated.Source, "via personal") {
		t.Errorf("work source = %q, want the personal reading", dedicated.Source)
	}
	if dedicated.SameAccountAs != "claude" {
		t.Errorf("SameAccountAs = %q, want claude", dedicated.SameAccountAs)
	}
	if base.Identity != email || dedicated.Identity != email || dedicated.AccountLabel != email {
		t.Errorf("email display = %q/%q/%q, want %q kept independent of the join key", base.Identity, dedicated.Identity, dedicated.AccountLabel, email)
	}
	if base.Account == "" || base.Account == email || base.Account == org || base.Account != dedicated.Account {
		t.Errorf("Account = %q/%q, want one opaque email+organizationUuid key, not the bare email", base.Account, dedicated.Account)
	}
}

// A dedicated dir with a complete login and no reading of its own still
// receives a proven same-account observation.
func TestReview141NoReadingReceivesProvenSameAccount(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const (
		email = "a@x.com"
		org   = "22222222-2222-4222-8222-222222222222"
	)
	review141WriteJSON(t, review141JSONPath(personal), review141Login(email, org))
	review141WriteJSON(t, review141JSONPath(work), review141Login(email, org))
	review141WriteJSON(t, review141CachePath(personal), review141ProvenanceCache(email, org, 40, review141Now-60_000))

	got := review141Collect(review141Now, nil)
	base := review141Entry(t, got, "claude")
	dedicated := review141Entry(t, got, "work")
	if review141Used(dedicated) != 40 || !strings.Contains(dedicated.Source, "via personal") {
		t.Errorf("work = used %v source %q, want the proven 40%% reading", review141Used(dedicated), dedicated.Source)
	}
	if base.Account == "" || base.Account == email || base.Account != dedicated.Account {
		t.Errorf("Account = %q/%q, want the opaque key on the no-reading row too", base.Account, dedicated.Account)
	}
}

// Native cachedUsageUtilization has no observation account. A current
// oauthAccount in the same file does not prove those windows, so a fresher
// native 92% must stay local and must not overwrite dedicated B's proven 10%.
func TestReview141NativeUtilizationDoesNotShareOverDedicated(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const (
		emailB = "b@x.com"
		orgB   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	)
	review141WriteJSON(t, review141JSONPath(personal), map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress":     emailB,
			"organizationUuid": orgB,
		},
		"cachedUsageUtilization": map[string]any{
			"fetchedAtMs": review141Now - 2*60_000,
			"utilization": map[string]any{
				"five_hour": map[string]any{
					"utilization": 92,
					"resets_at":   "2026-07-15T00:00:00.000Z",
				},
			},
		},
	})
	review141WriteJSON(t, review141JSONPath(work), review141Login(emailB, orgB))
	review141WriteJSON(t, review141CachePath(work), review141ProvenanceCache(emailB, orgB, 10, review141Now-30*60_000))

	got := review141Collect(review141Now, nil)
	base := review141Entry(t, got, "claude")
	dedicated := review141Entry(t, got, "work")
	if review141Used(base) != 92 {
		t.Errorf("base used = %v, native 92%% should still display locally", review141Used(base))
	}
	if base.Account != "" || base.SameAccountAs != "" {
		t.Errorf("base account/same = %q/%q, native utilization must stay unjoined", base.Account, base.SameAccountAs)
	}
	if review141Used(dedicated) != 10 || strings.Contains(dedicated.Source, "via ") || dedicated.SameAccountAs != "" {
		t.Errorf("work = used %v source %q same %q, want its own proven 10%%", review141Used(dedicated), dedicated.Source, dedicated.SameAccountAs)
	}
}

// Fresh statusLine windows written under A must not become B's data after the
// login file switches, and must not be copied onto B's dedicated reading.
func TestReview141LoginSwitchDoesNotGiveBAccountAWindows(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const (
		emailA = "a@x.com"
		orgA   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		emailB = "b@x.com"
		orgB   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	)
	// Cache written while personal was A, 2 minutes ago. Login is now B.
	review141WriteJSON(t, review141CachePath(personal), review141ProvenanceCache(emailA, orgA, 92, review141Now-2*60_000))
	review141WriteJSON(t, review141JSONPath(personal), review141Login(emailB, orgB))
	review141WriteJSON(t, review141JSONPath(work), review141Login(emailB, orgB))
	review141WriteJSON(t, review141CachePath(work), review141ProvenanceCache(emailB, orgB, 10, review141Now-30*60_000))

	got := review141Collect(review141Now, nil)
	base := review141Entry(t, got, "claude")
	dedicated := review141Entry(t, got, "work")
	if review141Used(base) == 92 || review141Used(dedicated) == 92 {
		t.Errorf("used = %v/%v, A's 92%% must not be current data for B", review141Used(base), review141Used(dedicated))
	}
	if review141Used(dedicated) != 10 {
		t.Errorf("work used = %v, want B's own 10%%", review141Used(dedicated))
	}
	if strings.Contains(dedicated.Source, "via ") {
		t.Errorf("work source = %q, B must not borrow the switched profile", dedicated.Source)
	}
	if dedicated.SameAccountAs != "" {
		ref := review141Entry(t, got, dedicated.SameAccountAs)
		if review141Used(ref) != 10 {
			t.Errorf("work deduped onto %s (used %v), want only a proven B row", dedicated.SameAccountAs, review141Used(ref))
		}
	}
	if base.Identity != emailB || dedicated.Identity != emailB {
		t.Errorf("email display = %q/%q, want %q preserved", base.Identity, dedicated.Identity, emailB)
	}
}

// The same email in two organizations is two accounts. Neither row may take
// the other's windows.
func TestReview141SameEmailDifferentOrgNeverShares(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const email = "same@x.com"
	const orgPersonal = "33333333-3333-4333-8333-333333333333"
	const orgWork = "44444444-4444-4444-8444-444444444444"
	review141WriteJSON(t, review141JSONPath(personal), review141Login(email, orgPersonal))
	review141WriteJSON(t, review141JSONPath(work), review141Login(email, orgWork))
	review141WriteJSON(t, review141CachePath(personal), review141ProvenanceCache(email, orgPersonal, 15, review141Now-20*60_000))
	review141WriteJSON(t, review141CachePath(work), review141ProvenanceCache(email, orgWork, 70, review141Now-60_000))

	got := review141Collect(review141Now, nil)
	base := review141Entry(t, got, "claude")
	dedicated := review141Entry(t, got, "work")
	if review141Used(base) != 15 || review141Used(dedicated) != 70 {
		t.Errorf("used = %v/%v, want 15 and 70 kept apart", review141Used(base), review141Used(dedicated))
	}
	if base.SameAccountAs != "" || dedicated.SameAccountAs != "" {
		t.Errorf("SameAccountAs = %q/%q, different organizations must not dedupe", base.SameAccountAs, dedicated.SameAccountAs)
	}
	if base.Account != "" && base.Account == dedicated.Account {
		t.Errorf("Account = %q shared across organizations", base.Account)
	}
}

// Email without an organization, or a cache the writer never stamped, is not
// proof of one account. Current email-only joins must not satisfy this.
func TestReview141MissingIdentityRefusesEmailSharing(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const email = "same@x.com"
	review141WriteJSON(t, review141JSONPath(personal), review141Login(email, ""))
	review141WriteJSON(t, review141JSONPath(work), review141Login(email, ""))
	review141WriteJSON(t, review141CachePath(personal), review141ProvenanceCache(email, "", 15, review141Now-20*60_000))
	review141WriteJSON(t, review141CachePath(work), review141ProvenanceCache(email, "", 70, review141Now-60_000))

	got := review141Collect(review141Now, nil)
	base := review141Entry(t, got, "claude")
	dedicated := review141Entry(t, got, "work")
	if review141Used(base) != 15 || review141Used(dedicated) != 70 || base.SameAccountAs != "" || dedicated.SameAccountAs != "" {
		t.Errorf("personal=%v %q work=%v %q, missing organizationUuid must refuse sharing", review141Used(base), base.SameAccountAs, review141Used(dedicated), dedicated.SameAccountAs)
	}
}

func TestReview141LegacyUntaggedCacheStaysLocal(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const (
		email = "same@x.com"
		org   = "55555555-5555-4555-8555-555555555555"
	)
	review141WriteJSON(t, review141JSONPath(personal), review141Login(email, org))
	review141WriteJSON(t, review141JSONPath(work), review141Login(email, org))
	// The current writer stamps no account. A later window write that has no
	// captured identity must leave that legacy shape untagged.
	if err := WriteClaudeLimitsCache(fiveHourInput(15), review141Now-20*60_000, review141CachePath(personal)); err != nil {
		t.Fatal(err)
	}
	if err := WriteClaudeLimitsCache(fiveHourInput(70), review141Now-60_000, review141CachePath(work)); err != nil {
		t.Fatal(err)
	}

	got := review141Collect(review141Now, nil)
	base := review141Entry(t, got, "claude")
	dedicated := review141Entry(t, got, "work")
	if review141Used(base) != 15 || review141Used(dedicated) != 70 || base.SameAccountAs != "" || dedicated.SameAccountAs != "" {
		t.Errorf("personal=%v %q work=%v %q, untagged caches must stay local", review141Used(base), base.SameAccountAs, review141Used(dedicated), dedicated.SameAccountAs)
	}
}

// A prompt-cache-only update may refresh the expiry. It must not drop or
// replace the account stamped by the earlier window write.
func TestReview141PromptOnlyCacheKeepsAccountProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-limits-latest.json")
	const (
		email = "a@x.com"
		org   = "66666666-6666-4666-8666-666666666666"
	)
	review141WriteJSON(t, path, review141ProvenanceCache(email, org, 42, 1_000))
	expiry := int64(1_700_003_600)
	wrote, err := WriteClaudeLimitsCacheGuarded(RateLimitsInput{
		PromptCachePresent:   true,
		PromptCacheExpiresAt: &expiry,
	}, 2_000, path)
	if err != nil || !wrote {
		t.Fatalf("prompt-only write: wrote=%v err=%v", wrote, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["accountEmail"] != email || parsed["organizationUuid"] != org {
		t.Errorf("provenance = %v/%v, prompt-only update relabeled or dropped the stamped account", parsed["accountEmail"], parsed["organizationUuid"])
	}
	five, _ := parsed["fiveHour"].(map[string]any)
	if five == nil || five["usedPercentage"] != 42.0 {
		t.Errorf("fiveHour = %v, prompt-only update must keep the stamped windows", parsed["fiveHour"])
	}
}

// A window write stamps the captured identity. A later window write without
// one clears that stamp instead of leaving the old account on new numbers.
func TestReview141WindowWriteStampsAndClearsProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-limits-latest.json")
	jsonPath := filepath.Join(t.TempDir(), ".claude.json")
	const (
		email = "a@x.com"
		org   = "99999999-9999-4999-8999-999999999999"
	)
	review141WriteJSON(t, jsonPath, review141Login(email, org))
	input := WithClaudeAccountProvenance(fiveHourInput(15), jsonPath)
	if input.AccountEmail != email || input.OrganizationUUID != org {
		t.Fatalf("provenance input = %q/%q", input.AccountEmail, input.OrganizationUUID)
	}
	if err := WriteClaudeLimitsCache(input, review141Now, path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stamped map[string]any
	if err := json.Unmarshal(raw, &stamped); err != nil {
		t.Fatal(err)
	}
	if stamped["accountEmail"] != email || stamped["organizationUuid"] != org {
		t.Fatalf("stamped cache = %v/%v", stamped["accountEmail"], stamped["organizationUuid"])
	}
	if err := WriteClaudeLimitsCache(fiveHourInput(20), review141Now+1_000, path); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cleared map[string]any
	if err := json.Unmarshal(raw, &cleared); err != nil {
		t.Fatal(err)
	}
	if _, ok := cleared["accountEmail"]; ok || cleared["organizationUuid"] != nil {
		t.Fatalf("unstamped window write kept provenance: %v", cleared)
	}
	five, _ := cleared["fiveHour"].(map[string]any)
	if five == nil || five["usedPercentage"] != 20.0 {
		t.Fatalf("fiveHour = %v, want the new window", cleared["fiveHour"])
	}
}

// Only={work} must observe the same shared work value as a full collect.
// SameAccountAs must not name a row the filter removed. Other families stay
// uncollected; a Families exclusion still skips Claude entirely.
func TestReview141OnlySharesBeforeFilter(t *testing.T) {
	const now = review141Now
	five := 300
	fresh := &LimitWindow{UsedPercentage: 15, WindowMinutes: &five}
	old := &LimitWindow{UsedPercentage: 80, WindowMinutes: &five}
	mid := &LimitWindow{UsedPercentage: 50, WindowMinutes: &five}
	reading := func(id, label string, window *LimitWindow, fetched int64, called *bool) ClaudeProfileCollector {
		return ClaudeProfileCollector{
			ID: id, Label: label,
			Collector: func(_ *string, _ int64) ProviderLimits {
				if called != nil {
					*called = true
				}
				return ProviderLimits{
					ProviderID: id, Label: label, ProfileLabel: label, Account: "proven-same",
					Primary: window, FetchedAtMs: fetched, Source: "claude statusLine cache",
				}
			},
		}
	}

	t.Run("work matches the full shared value", func(t *testing.T) {
		var personal, work, codex bool
		opts := CollectOptions{
			Claude: []ClaudeProfileCollector{
				reading("claude", "personal", fresh, now, &personal),
				reading("work", "work", old, now-30*60_000, &work),
			},
			Codex: []CodexProfileCollector{{
				ID: "codex", Label: "Codex",
				Collector: func(_ *string, _ int64) ProviderLimits {
					codex = true
					return ProviderLimits{ProviderID: "codex", Label: "Codex"}
				},
			}},
		}
		full := CollectAllProviderLimits(nil, now, opts)
		fullWork := review141Entry(t, full, "work")
		if review141Sidebar(fullWork) != "5h 15%" {
			t.Fatalf("full $limit = %q, want the shared 5h 15%% before testing Only", review141Sidebar(fullWork))
		}
		personal, work, codex = false, false, false
		opts.Only = map[string]bool{"work": true}
		filtered := CollectAllProviderLimits(nil, now, opts)
		if !personal {
			t.Errorf("Only={work} must collect the claude sibling before sharing")
		}
		if codex {
			t.Errorf("Only={work} collected codex, an unrelated family")
		}
		got := review141Entry(t, filtered, "work")
		if review141Sidebar(got) != review141Sidebar(fullWork) || review141Used(got) != 15 {
			t.Errorf("Only={work} $limit = %q used %v, want %q", review141Sidebar(got), review141Used(got), review141Sidebar(fullWork))
		}
		if outside := review141SameAccountOutside(filtered); outside != "" || got.SameAccountAs != "" {
			t.Errorf("SameAccountAs = %q outside %q", got.SameAccountAs, outside)
		}
	})

	t.Run("reanchors when the first row is filtered out", func(t *testing.T) {
		opts := CollectOptions{Claude: []ClaudeProfileCollector{
			reading("claude", "personal", fresh, now, nil),
			reading("work", "work", old, now-30*60_000, nil),
			reading("extra", "extra", mid, now-10*60_000, nil),
		}}
		opts.Only = map[string]bool{"work": true, "extra": true}
		got := CollectAllProviderLimits(nil, now, opts)
		if outside := review141SameAccountOutside(got); outside != "" {
			t.Errorf("SameAccountAs points outside the report: %s", outside)
		}
		work := review141Entry(t, got, "work")
		extra := review141Entry(t, got, "extra")
		if review141Used(work) != 15 || review141Used(extra) != 15 {
			t.Errorf("used = %v/%v, want the family-wide 15%% after the excluded anchor is dropped", review141Used(work), review141Used(extra))
		}
		if work.SameAccountAs != "" {
			t.Errorf("first retained row SameAccountAs = %q, want empty", work.SameAccountAs)
		}
		if extra.SameAccountAs != "work" {
			t.Errorf("extra SameAccountAs = %q, want the retained anchor work", extra.SameAccountAs)
		}
	})

	t.Run("families exclusion still skips claude", func(t *testing.T) {
		var personal bool
		opts := CollectOptions{
			Families: map[string]bool{"codex": true},
			Claude: []ClaudeProfileCollector{
				reading("claude", "personal", fresh, now, &personal),
			},
			Codex: []CodexProfileCollector{{
				ID: "codex", Label: "Codex",
				Collector: func(_ *string, n int64) ProviderLimits {
					return ProviderLimits{ProviderID: "codex", Label: "Codex", FetchedAtMs: n}
				},
			}},
		}
		got := CollectAllProviderLimits(nil, now, opts)
		if personal {
			t.Error("Families={codex} must not collect claude")
		}
		if len(got) != 1 || got[0].ProviderID != "codex" {
			t.Errorf("got %d entries, want only codex", len(got))
		}
	})
}

// The file-backed form of the Only bug: proven same-account profiles, then
// Only={work}, must show the same work limit as the full collect.
func TestReview141OnlyKnownAccountMatchesFull(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal},
		{"work", "work", work},
	})
	const (
		email = "a@x.com"
		org   = "77777777-7777-4777-8777-777777777777"
	)
	review141WriteJSON(t, review141JSONPath(personal), review141Login(email, org))
	review141WriteJSON(t, review141JSONPath(work), review141Login(email, org))
	review141WriteJSON(t, review141CachePath(personal), review141ProvenanceCache(email, org, 15, review141Now-60_000))
	review141WriteJSON(t, review141CachePath(work), review141ProvenanceCache(email, org, 80, review141Now-20*60_000))

	full := review141Collect(review141Now, nil)
	filtered := review141Collect(review141Now, map[string]bool{"work": true})
	fullWork := review141Entry(t, full, "work")
	onlyWork := review141Entry(t, filtered, "work")
	if review141Sidebar(fullWork) != "5h 15%" {
		t.Fatalf("full $limit = %q, want shared 5h 15%%", review141Sidebar(fullWork))
	}
	if review141Used(onlyWork) != review141Used(fullWork) || review141Sidebar(onlyWork) != review141Sidebar(fullWork) {
		t.Errorf("Only={work} $limit = %q used %v, full = %q used %v", review141Sidebar(onlyWork), review141Used(onlyWork), review141Sidebar(fullWork), review141Used(fullWork))
	}
	if outside := review141SameAccountOutside(filtered); outside != "" || onlyWork.SameAccountAs != "" {
		t.Errorf("Only={work} SameAccountAs = %q outside %q", onlyWork.SameAccountAs, outside)
	}
}

// A signed-in provider whose fetch fails, with its home on disk, stays in use
// and keeps the error. A missing login is not a better outcome than a failed one.
func TestReview141SignedInFetchFailureStaysInUse(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", t.TempDir())
	devinDir := filepath.Join(home, "devin-data")
	if err := os.MkdirAll(devinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// http is rejected before any dial. The credential is still a login, and
	// the collector records a fetch failure rather than needs-login.
	cred := "windsurf_api_key = \"test-key-not-a-secret\"\napi_server_url = \"http://127.0.0.1:1\"\n"
	if err := os.WriteFile(filepath.Join(devinDir, "credentials.toml"), []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVIN_DATA_DIR", devinDir)

	got := review141Collect(review141Now, nil)
	devin := review141Entry(t, got, "devin")
	if devin.Status() != StatusUnavailable || devin.Note == nil || *devin.Note == "" {
		t.Errorf("status = %q note = %v, want the fetch error visible", devin.Status(), devin.Note)
	}
	if devin.Home != devinDir {
		t.Errorf("Home = %q, want %q", devin.Home, devinDir)
	}
	if !devin.InUse {
		t.Error("signed-in fetch failure with an existing home must stay in use")
	}
}

// Plugin state under ~/.claude/herdr-usagebar is not a Claude install. The
// implicit profile must stay out of use when that directory is the only evidence.
func TestReview141PluginClaudeDirDoesNotImplyInUse(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", t.TempDir())
	pluginDir := filepath.Join(home, ".claude", "herdr-usagebar")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "usage-history.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_JSON", filepath.Join(home, ".claude.json"))
	t.Setenv("CLAUDE_PROJECTS_ROOT", filepath.Join(home, ".claude", "projects"))
	t.Setenv("USAGEBAR_STATE_DIR", pluginDir)
	t.Setenv("USAGEBAR_CLAUDE_LIMITS_PATH", filepath.Join(pluginDir, "claude-limits-latest.json"))

	got := review141Collect(review141Now, nil)
	claude := review141Entry(t, got, "claude")
	if claude.Status() != StatusNoData {
		t.Errorf("status = %q, want no-data rather than a failure", claude.Status())
	}
	if !strings.HasSuffix(claude.Home, ".claude") {
		t.Errorf("Home = %q, want the implicit config dir", claude.Home)
	}
	if claude.InUse {
		t.Error("plugin-created ~/.claude/herdr-usagebar alone must not make implicit Claude in use")
	}
}

// A real ~/.claude.json is install evidence even when ~/.claude does not exist
// yet. This is the complement of the herdr-usagebar-only case.
func TestReview141ImplicitClaudeJSONStaysInUse(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", t.TempDir())
	jsonPath := filepath.Join(home, ".claude.json")
	review141WriteJSON(t, jsonPath, review141Login("installed@example.com", "88888888-8888-4888-8888-888888888888"))
	t.Setenv("CLAUDE_CONFIG_JSON", jsonPath)
	t.Setenv("CLAUDE_PROJECTS_ROOT", filepath.Join(home, ".claude", "projects"))
	t.Setenv("USAGEBAR_CLAUDE_LIMITS_PATH", filepath.Join(home, ".claude", "herdr-usagebar", "claude-limits-latest.json"))

	got := review141Collect(review141Now, nil)
	claude := review141Entry(t, got, "claude")
	if !claude.InUse {
		t.Error("implicit Claude with ~/.claude.json must stay in use")
	}
}
