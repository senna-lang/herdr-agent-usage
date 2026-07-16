/**
 * Tests for OpenCode Go web parsers.
 */
package limits

import "testing"

func TestParseWorkspaceIDs(t *testing.T) {
	got := ParseWorkspaceIDs(`foo wrk_abc123 bar wrk_abc123 wrk_xyz`)
	if len(got) != 2 || got[0] != "wrk_abc123" || got[1] != "wrk_xyz" {
		t.Fatalf("%v", got)
	}
}

func TestParseOpenCodeGoUsagePage(t *testing.T) {
	text := `rollingUsage:$R[33]={status:"ok",resetInSec:1805,usagePercent:42}
weeklyUsage:$R[34]={status:"ok",resetInSec:100000,usagePercent:17}
monthlyUsage:$R[35]={status:"ok",resetInSec:2000000,usagePercent:31}`
	got := ParseOpenCodeGoUsagePage(text)
	if got == nil || got.Rolling.UsedPercentage != 42 || got.Rolling.ResetInSec != 1805 {
		t.Fatalf("rolling %+v", got)
	}
	if got.Weekly == nil || got.Weekly.UsedPercentage != 17 {
		t.Fatalf("weekly %+v", got.Weekly)
	}
	if got.Monthly == nil || got.Monthly.UsedPercentage != 31 {
		t.Fatalf("monthly %+v", got.Monthly)
	}
	if ParseOpenCodeGoUsagePage("no usage here") != nil {
		t.Fatal("expected nil")
	}
}

func TestOpenCodeWorkspacesServerID(t *testing.T) {
	if OpenCodeWorkspacesServerID == "" {
		t.Fatal("server id empty")
	}
}
