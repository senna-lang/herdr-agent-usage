package limits

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkInUse(t *testing.T) {
	installed := t.TempDir()
	missing := filepath.Join(t.TempDir(), "never-installed")
	window := &LimitWindow{UsedPercentage: 10}
	cases := []struct {
		name string
		p    ProviderLimits
		want bool
	}{
		{"a configured profile with no data yet", ProviderLimits{Configured: true, NoReadingYet: true, Home: missing}, true},
		{"any reading", ProviderLimits{Primary: window}, true},
		{"installed but signed out", ProviderLimits{Action: "grok login", Home: installed}, true},
		{"installed, nothing recorded yet", ProviderLimits{NoReadingYet: true, Home: installed}, true},
		{"never installed, asks for a login", ProviderLimits{Action: "grok login", Home: missing}, false},
		{"never installed, nothing recorded", ProviderLimits{NoReadingYet: true, Home: missing}, false},
		{"no home known", ProviderLimits{NoReadingYet: true}, false},
		{"data present but no subscription use", ProviderLimits{Home: installed}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MarkInUse([]ProviderLimits{tc.p}, false)[0].InUse; got != tc.want {
				t.Fatalf("InUse = %v, want %v", got, tc.want)
			}
		})
	}
	// An explicit [ui] providers list shows exactly what it names.
	if !MarkInUse([]ProviderLimits{{Home: missing}}, true)[0].InUse {
		t.Fatal("explicit list must mark every collected entry in use")
	}
}

// [ui] providers restricts collection by family; other collectors never run.
func TestCollectAllProviderLimits_FamiliesFilter(t *testing.T) {
	ran := false
	got := CollectAllProviderLimits(nil, 100, CollectOptions{
		Families: map[string]bool{"codex": true},
		Claude: []ClaudeProfileCollector{{ID: "claude", Label: "Claude", Collector: func(_ *string, now int64) ProviderLimits {
			ran = true
			return ProviderLimits{ProviderID: "claude"}
		}}},
	})
	if ran || len(got) != 1 || got[0].ProviderID != "codex" || !got[0].InUse {
		t.Fatalf("got %+v (claude ran: %v), want only codex, in use", got, ran)
	}
}

func TestResolvedProviderFamilies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	if ResolvedProviderFamilies() != nil {
		t.Fatal("unset must mean every provider")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[ui]\nproviders = [\"Claude\", \" codex \", \"\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ResolvedProviderFamilies()
	if len(got) != 2 || !got["claude"] || !got["codex"] {
		t.Fatalf("families = %v", got)
	}
}
