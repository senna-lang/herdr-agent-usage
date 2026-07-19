/**
 * Tests for Herdr toast config inspect/append
 */
package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectToastConfig_Missing(t *testing.T) {
	status := InspectToastConfig("[theme]\nname = \"x\"\n")
	if status.Kind != "missing" {
		t.Fatalf("got %+v", status)
	}
}

func TestInspectToastConfig_Delivery(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "double quoted", raw: "[ui.toast]\ndelivery = \"herdr\"\n", want: "herdr"},
		{name: "single quoted with comment", raw: "[ui.toast]\ndelivery = 'system' # desktop notifications\n", want: "system"},
		{name: "recommended snippet", raw: ToastConfigSnippet(), want: "herdr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := InspectToastConfig(tt.raw)
			if status.Kind != "present" || status.Delivery == nil || *status.Delivery != tt.want {
				t.Fatalf("got %+v, want delivery=%q", status, tt.want)
			}
		})
	}
}

func TestAppendToastConfigIfMissing_Writes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[theme]\nname = \"tokyo-night\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := AppendToastConfigIfMissing(path)
	if !r.Wrote {
		t.Fatalf("expected write: %+v", r)
	}
	text, _ := os.ReadFile(path)
	s := string(text)
	if !strings.Contains(s, "[theme]") || !strings.Contains(s, "[ui.toast]") || !strings.Contains(s, `delivery = "herdr"`) {
		t.Fatalf("got %s", s)
	}
}

func TestAppendToastConfigIfMissing_Preserves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[ui.toast]\ndelivery = \"system\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	r := AppendToastConfigIfMissing(path)
	if r.Wrote {
		t.Fatal("should not write")
	}
	text, _ := os.ReadFile(path)
	if string(text) != original {
		t.Fatalf("mutated: %q", text)
	}
}

func TestToastConfigSnippet(t *testing.T) {
	s := ToastConfigSnippet()
	if !strings.Contains(s, "[ui.toast]") || !strings.Contains(s, "delivery") {
		t.Fatalf("%q", s)
	}
}
