/**
 * Tests for ApplyNotifyResult.
 */
package ratelimit

import "testing"

func TestApplyNotifyResult_Shown(t *testing.T) {
	b50, b20 := Bucket50, Bucket20
	previous := &WindowState{ResetsAt: 100, NotifiedBucket: &b50, FailedNotifyAttempts: 2}
	candidate := WindowState{ResetsAt: 100, NotifiedBucket: &b20, FailedNotifyAttempts: 0}
	result := ApplyNotifyResult(previous, candidate, true)
	if result == nil || result.NotifiedBucket == nil || *result.NotifiedBucket != Bucket20 || result.FailedNotifyAttempts != 0 {
		t.Fatalf("got %+v", result)
	}
}

func TestApplyNotifyResult_NotShownKeepsPrevious(t *testing.T) {
	b50, b20 := Bucket50, Bucket20
	previous := &WindowState{ResetsAt: 100, NotifiedBucket: &b50, FailedNotifyAttempts: 0}
	candidate := WindowState{ResetsAt: 100, NotifiedBucket: &b20, FailedNotifyAttempts: 0}
	result := ApplyNotifyResult(previous, candidate, false)
	if result == nil || result.NotifiedBucket == nil || *result.NotifiedBucket != Bucket50 || result.FailedNotifyAttempts != 1 {
		t.Fatalf("got %+v", result)
	}
}

func TestApplyNotifyResult_NullPreviousNotShown(t *testing.T) {
	b50 := Bucket50
	candidate := WindowState{ResetsAt: 100, NotifiedBucket: &b50, FailedNotifyAttempts: 0}
	result := ApplyNotifyResult(nil, candidate, false)
	if result == nil || result.NotifiedBucket != nil || result.FailedNotifyAttempts != 1 || result.ResetsAt != 100 {
		t.Fatalf("got %+v", result)
	}
}

func TestApplyNotifyResult_GiveUp(t *testing.T) {
	b50, b20 := Bucket50, Bucket20
	previous := &WindowState{
		ResetsAt: 100, NotifiedBucket: &b50,
		FailedNotifyAttempts: MaxFailedNotifyAttempts - 1,
	}
	candidate := WindowState{ResetsAt: 100, NotifiedBucket: &b20, FailedNotifyAttempts: 0}
	result := ApplyNotifyResult(previous, candidate, false)
	if result == nil || result.NotifiedBucket == nil || *result.NotifiedBucket != Bucket20 || result.FailedNotifyAttempts != 0 {
		t.Fatalf("got %+v", result)
	}
}
