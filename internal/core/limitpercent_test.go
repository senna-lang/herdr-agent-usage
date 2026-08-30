/**
 * Tests for quota-percentage presentation direction.
 */
package core

import "testing"

func TestParseLimitPercent(t *testing.T) {
	cases := []struct {
		raw  string
		want LimitPercent
	}{
		{"", LimitPercentRemaining},
		{"remaining", LimitPercentRemaining},
		{" used ", LimitPercentUsed},
		{"USED", LimitPercentUsed},
		{"burned", LimitPercentRemaining},
	}
	for _, c := range cases {
		if got := ParseLimitPercent(c.raw); got != c.want {
			t.Fatalf("ParseLimitPercent(%q)=%q want %q", c.raw, got, c.want)
		}
	}
}

func TestLimitPercentDisplayAndFill(t *testing.T) {
	if LimitPercentRemaining.DisplayPercent(72) != 72 {
		t.Fatal("remaining keeps the headroom number")
	}
	if LimitPercentUsed.DisplayPercent(72) != 28 {
		t.Fatal("used inverts remaining 72 to 28")
	}
	if LimitPercentUsed.BarFill(72) != 28 {
		t.Fatal("bar fill follows the displayed used number")
	}
	if LimitPercentRemaining.BarFill(72) != 72 {
		t.Fatal("remaining bar fill stays on headroom")
	}
	if (LimitPercent("")).DisplayPercent(90) != 90 {
		t.Fatal("zero value is remaining")
	}
}

func TestLimitPercentInvertRemainingBucket(t *testing.T) {
	if got := LimitPercentRemaining.InvertRemainingBucket("20"); got != 20 {
		t.Fatalf("remaining bucket display=%d", got)
	}
	if got := LimitPercentUsed.InvertRemainingBucket("20"); got != 80 {
		t.Fatalf("used inverts remaining 20 to %d, want 80", got)
	}
	if got := LimitPercentUsed.InvertRemainingBucket("50"); got != 50 {
		t.Fatalf("50 remaining is 50 used, got %d", got)
	}
}
