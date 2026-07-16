/**
 * Herdr toast config inspect / append のテスト
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
	status := InspectToastConfig(`
[ui.toast]
delivery = "herdr"
`)
	if status.Kind != "present" || status.Delivery == nil || *status.Delivery != "herdr" {
		t.Fatalf("got %+v", status)
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
