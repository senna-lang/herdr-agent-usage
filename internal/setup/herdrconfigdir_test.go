package setup

import (
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/testpath"
)

func TestHerdrConfigDirFor(t *testing.T) {
	xdg := testpath.Abs("/xdg")
	appData := testpath.Abs("/Users/u/AppData/Roaming")
	for _, tt := range []struct {
		name string
		env  map[string]string
		goos string
		want string
	}{
		{name: "xdg wins on unix", env: map[string]string{"XDG_CONFIG_HOME": xdg}, goos: "linux", want: filepath.Join(xdg, "herdr")},
		{name: "xdg wins on windows", env: map[string]string{"XDG_CONFIG_HOME": xdg, "APPDATA": appData}, goos: "windows", want: filepath.Join(xdg, "herdr")},
		{name: "windows uses APPDATA", env: map[string]string{"APPDATA": appData}, goos: "windows", want: filepath.Join(appData, "herdr")},
		{name: "unix ignores APPDATA", env: map[string]string{"APPDATA": appData}, goos: "darwin", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := herdrConfigDirFor(tt.env, tt.goos)
			if tt.want == "" {
				// The ~/.config default depends on the real home directory.
				if filepath.Base(got) != "herdr" || filepath.Base(filepath.Dir(got)) != ".config" {
					t.Fatalf("got %q, want ~/.config/herdr", got)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePluginConfigDir_EnvOverrideWins(t *testing.T) {
	dir := testpath.Abs("/plugin/config")
	got := ResolvePluginConfigDir(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": dir, "APPDATA": testpath.Abs("/appdata")})
	if got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}
