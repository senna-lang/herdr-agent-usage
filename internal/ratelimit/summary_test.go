/**
 * Tests for FormatStatusLineSummary.
 */
package ratelimit

import "testing"

func TestFormatStatusLineSummary(t *testing.T) {
	got := FormatStatusLineSummary(&RateLimitsInput{
		FiveHour: &RateWindowInput{UsedPercentage: 89, ResetsAt: 100},
		SevenDay: &RateWindowInput{UsedPercentage: 44, ResetsAt: 200},
	})
	if got != "5h:11% 7d:56%" {
		t.Fatalf("%q", got)
	}

	got = FormatStatusLineSummary(&RateLimitsInput{
		SevenDay: &RateWindowInput{UsedPercentage: 43.99999999999999, ResetsAt: 200},
	})
	if got != "7d:56%" {
		t.Fatalf("%q", got)
	}

	got = FormatStatusLineSummary(&RateLimitsInput{
		FiveHour: &RateWindowInput{UsedPercentage: 20.3, ResetsAt: 100},
	})
	if got != "5h:80%" {
		t.Fatalf("%q", got)
	}

	got = FormatStatusLineSummary(&RateLimitsInput{
		FiveHour: &RateWindowInput{UsedPercentage: 10, ResetsAt: 100},
	})
	if got != "5h:90%" {
		t.Fatalf("%q", got)
	}

	if FormatStatusLineSummary(nil) != "" {
		t.Fatal("expected empty")
	}
}
