/**
 * Tests for the Grok gRPC-web billing parser.
 */
package limits

import (
	"encoding/hex"
	"testing"
	"time"
)

// Fixture captured from a real response (used≈55%, reset at path 1/5/1).
const liveHex = "00000000560a540d00005c4212001a00220c0882acd8d20610e0cdbd9f022a0c0882a1fdd20610e0cdbd9f023a0708021500005c42421e0802120c0882acd8d20610e0cdbd9f021a0c0882a1fdd20610e0cdbd9f02580162006801800000000f677270632d7374617475733a300d0a"

func TestGrpcWebDataFrames(t *testing.T) {
	data, err := hex.DecodeString(liveHex)
	if err != nil {
		t.Fatal(err)
	}
	frames := GrpcWebDataFrames(data)
	if len(frames) < 1 {
		t.Fatal("expected frames")
	}
	if len(frames[0]) != 86 {
		t.Fatalf("frame0 len=%d want 86", len(frames[0]))
	}
}

func TestParseGrokWebBillingResponse(t *testing.T) {
	data, err := hex.DecodeString(liveHex)
	if err != nil {
		t.Fatal(err)
	}
	nowMs := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).UnixMilli()
	snap := ParseGrokWebBillingResponse(data, nowMs)
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.UsedPercent != 55 {
		t.Fatalf("usedPercent=%v want 55", snap.UsedPercent)
	}
	if snap.ResetsAtEpochSeconds == nil || *snap.ResetsAtEpochSeconds != 1_784_631_426 {
		t.Fatalf("resetsAt=%v want 1784631426", snap.ResetsAtEpochSeconds)
	}
}

func TestLimitWindowFromWebBilling(t *testing.T) {
	reset := int64(1_784_631_426)
	nowMs := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).UnixMilli()
	w := LimitWindowFromWebBilling(GrokWebBillingSnapshot{UsedPercent: 55, ResetsAtEpochSeconds: &reset}, nowMs)
	if w == nil || w.UsedPercentage != 55 {
		t.Fatalf("%+v", w)
	}
	if w.WindowMinutes == nil || *w.WindowMinutes != 7*24*60 {
		t.Fatalf("windowMinutes=%v want 7d", w.WindowMinutes)
	}
	if w.ResetsAt == nil || *w.ResetsAt != reset {
		t.Fatalf("resetsAt=%v", w.ResetsAt)
	}
}
