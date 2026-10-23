package limits

import "testing"

const openCodeConsoleStatusFixture = `{
  "product": "go",
  "access": {
    "startsAt": "2026-09-29T21:21:42.000Z",
    "endsAt": "2026-10-29T21:21:42.000Z",
    "meters": {
      "fiveHour": {
        "startsAt": "2026-10-22T23:29:28.000Z",
        "resetsAt": "2026-10-23T04:29:28.000Z",
        "limitMicroCents": "1200000000",
        "usedMicroCents": "33765864"
      },
      "week": {
        "startsAt": "2026-10-19T00:00:00.000Z",
        "resetsAt": "2026-10-26T00:00:00.000Z",
        "limitMicroCents": "3000000000",
        "usedMicroCents": "90880793"
      },
      "month": {
        "resetsAt": "2026-10-29T21:21:42.000Z",
        "limitMicroCents": "6000000000",
        "usedMicroCents": "3770617981"
      }
    }
  }
}`

func TestParseOpenCodeConsoleStatus(t *testing.T) {
	// 2026-10-23T00:00:00Z
	nowMs := int64(1792713600000)
	snap := ParseOpenCodeConsoleStatus(openCodeConsoleStatusFixture, "org_test", nowMs)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.Source != "opencode.ai console" || snap.WorkspaceID != "org_test" {
		t.Fatalf("unexpected identity: %+v", snap)
	}
	if got, want := snap.Rolling.UsedPercentage, 2.813822; got < want-0.001 || got > want+0.001 {
		t.Errorf("rolling used = %v, want ~%v", got, want)
	}
	if got, want := snap.Rolling.ResetInSec, 4*3600+29*60+28; got != want {
		t.Errorf("rolling reset = %d, want %d", got, want)
	}
	if snap.Weekly == nil || snap.Monthly == nil {
		t.Fatalf("expected weekly and monthly windows, got %+v", snap)
	}
	if got, want := snap.Weekly.UsedPercentage, 3.029360; got < want-0.001 || got > want+0.001 {
		t.Errorf("weekly used = %v, want ~%v", got, want)
	}
	if got, want := snap.Monthly.UsedPercentage, 62.843633; got < want-0.001 || got > want+0.001 {
		t.Errorf("monthly used = %v, want ~%v", got, want)
	}
	if got, want := snap.Monthly.ResetInSec, 6*86400+21*3600+21*60+42; got != want {
		t.Errorf("monthly reset = %d, want %d", got, want)
	}
}

func TestParseOpenCodeConsoleStatusClampsAndSkips(t *testing.T) {
	nowMs := int64(1792713600000)

	over := `{"access":{"meters":{"fiveHour":{"limitMicroCents":"100","usedMicroCents":"250","resetsAt":""}}}}`
	snap := ParseOpenCodeConsoleStatus(over, "org", nowMs)
	if snap == nil {
		t.Fatal("expected snapshot for over-limit window")
	}
	if snap.Rolling.UsedPercentage != 100 {
		t.Errorf("used = %v, want clamp to 100", snap.Rolling.UsedPercentage)
	}
	if snap.Rolling.ResetInSec != 0 {
		t.Errorf("reset = %d, want 0 for empty resetsAt", snap.Rolling.ResetInSec)
	}
	if snap.Weekly != nil || snap.Monthly != nil {
		t.Errorf("expected absent windows to stay nil, got %+v / %+v", snap.Weekly, snap.Monthly)
	}

	zeroLimit := `{"access":{"meters":{"fiveHour":{"limitMicroCents":"0","usedMicroCents":"10"}}}}`
	if snap := ParseOpenCodeConsoleStatus(zeroLimit, "org", nowMs); snap != nil {
		t.Errorf("expected nil for zero limit, got %+v", snap)
	}

	if snap := ParseOpenCodeConsoleStatus("not json", "org", nowMs); snap != nil {
		t.Errorf("expected nil for malformed body, got %+v", snap)
	}

	past := `{"access":{"meters":{"fiveHour":{"limitMicroCents":"100","usedMicroCents":"1","resetsAt":"2026-01-01T00:00:00.000Z"}}}}`
	if snap := ParseOpenCodeConsoleStatus(past, "org", nowMs); snap == nil || snap.Rolling.ResetInSec != 0 {
		t.Errorf("expected 0 reset for a past resetsAt, got %+v", snap)
	}
}
