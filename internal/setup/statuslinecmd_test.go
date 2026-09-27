package setup

import "testing"

func TestStatusLineCommandFor(t *testing.T) {
	for _, tt := range []struct {
		goos string
		want string
	}{
		{goos: "linux", want: "bash /plugin/bin/run-statusline.sh"},
		{goos: "darwin", want: "bash /plugin/bin/run-statusline.sh"},
		{goos: "windows", want: "/plugin/bin/usagebar.exe statusline"},
	} {
		t.Run(tt.goos, func(t *testing.T) {
			if got := statusLineCommandFor("/plugin", "run-statusline.sh", "statusline", tt.goos); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
