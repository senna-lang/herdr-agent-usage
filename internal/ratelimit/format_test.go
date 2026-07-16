/**
 * Tests for FormatNotificationBody.
 */
package ratelimit

import "testing"

func TestFormatNotificationBody_Session(t *testing.T) {
	nowMs := int64(1_800_000_000_000)
	resetsAt := nowMs/1000 + 2*3600 + 15*60
	result := FormatNotificationBody(WindowFiveHour, Bucket20, resetsAt, nowMs)
	if result.Title != "Session limit" {
		t.Fatalf("title=%q", result.Title)
	}
	if result.Body != "20% remaining · resets in 2h 15m" {
		t.Fatalf("body=%q", result.Body)
	}
}

func TestFormatNotificationBody_Weekly(t *testing.T) {
	nowMs := int64(1_800_000_000_000)
	resetsAt := nowMs/1000 + 3*86400 + 4*3600
	result := FormatNotificationBody(WindowSevenDay, Bucket10, resetsAt, nowMs)
	if result.Title != "Weekly limit" {
		t.Fatalf("title=%q", result.Title)
	}
	if result.Body != "10% remaining · resets in 3d 4h" {
		t.Fatalf("body=%q", result.Body)
	}
}

func TestFormatNotificationBody_MinutesOnly(t *testing.T) {
	nowMs := int64(1_800_000_000_000)
	resetsAt := nowMs/1000 + 30*60
	result := FormatNotificationBody(WindowFiveHour, Bucket5, resetsAt, nowMs)
	if result.Body != "5% remaining · resets in 30m" {
		t.Fatalf("body=%q", result.Body)
	}
}

func TestFormatNotificationBody_PastReset(t *testing.T) {
	nowMs := int64(1_800_000_000_000)
	resetsAt := nowMs/1000 - 100
	result := FormatNotificationBody(WindowFiveHour, Bucket5, resetsAt, nowMs)
	if result.Body != "5% remaining · resets in 0m" {
		t.Fatalf("body=%q", result.Body)
	}
}
