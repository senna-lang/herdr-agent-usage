package core

import "testing"

func TestFormatSidebarCountdown(t *testing.T) {
	cases := []struct {
		seconds int64
		want    string
	}{
		{0, ""},
		{-5, ""},
		{30, "1m"},
		{1800, "30m"},
		{3600, "1h"},
		{11640, "3h"},
		{86400, "1d"},
		{259200, "3d"},
		{1036800, "12d"},
	}
	for _, tc := range cases {
		if got := FormatSidebarCountdown(tc.seconds); got != tc.want {
			t.Errorf("FormatSidebarCountdown(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
