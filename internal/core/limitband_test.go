package core

import "testing"

func TestLimitBandFor(t *testing.T) {
	cases := []struct {
		remaining float64
		want      LimitBand
	}{
		{100, LimitHigh},
		{50, LimitHigh},
		{49.9, LimitMid},
		{25, LimitMid},
		{24.9, LimitLow},
		{0, LimitLow},
	}
	for _, tc := range cases {
		if got := LimitBandFor(tc.remaining); got != tc.want {
			t.Errorf("LimitBandFor(%v) = %q, want %q", tc.remaining, got, tc.want)
		}
	}
}

func TestLimitTokenNames(t *testing.T) {
	want := []string{"limit_high", "limit_mid", "limit_low"}
	if len(LimitTokenNames) != len(want) {
		t.Fatalf("got %v, want %v", LimitTokenNames, want)
	}
	for i := range want {
		if LimitTokenNames[i] != want[i] {
			t.Fatalf("got %v, want %v", LimitTokenNames, want)
		}
	}
}
