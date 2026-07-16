/**
 * plugin config seed / parse のテスト
 */
package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParsePluginConfigTOML_Defaults(t *testing.T) {
	cfg := ParsePluginConfigTOML(DefaultPluginConfigTOML(DefaultPluginConfig))
	if !cfg.NotifyEnabled {
		t.Fatal("expected enabled")
	}
	if !reflect.DeepEqual(cfg.RemainingThresholds, []int{50, 20, 10, 5}) {
		t.Fatalf("thresholds=%v", cfg.RemainingThresholds)
	}
}

func TestParsePluginConfigTOML_Custom(t *testing.T) {
	cfg := ParsePluginConfigTOML(`
[notify]
enabled = false
remaining_thresholds = [30, 10]
`)
	if cfg.NotifyEnabled {
		t.Fatal("expected disabled")
	}
	if !reflect.DeepEqual(cfg.RemainingThresholds, []int{30, 10}) {
		t.Fatalf("thresholds=%v", cfg.RemainingThresholds)
	}
}

func TestSeedPluginConfigIfMissing(t *testing.T) {
	dir := t.TempDir()
	if !SeedPluginConfigIfMissing(dir) {
		t.Fatal("first seed should write")
	}
	if SeedPluginConfigIfMissing(dir) {
		t.Fatal("second seed should not write")
	}
	text, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(text), "[notify]") {
		t.Fatal("missing [notify]")
	}
	if !LoadPluginConfig(dir).NotifyEnabled {
		t.Fatal("expected enabled")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
