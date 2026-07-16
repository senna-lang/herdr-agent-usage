/**
 * Tests for DecideBucket.
 */
package ratelimit

import "testing"

const resetsAt int64 = 1_800_000_000

func TestDecideBucket_Enters50(t *testing.T) {
	result := DecideBucket(WindowInput{UsedPercentage: 55, ResetsAt: resetsAt}, nil)
	if result.BucketToNotify == nil || *result.BucketToNotify != Bucket50 {
		t.Fatalf("bucketToNotify = %#v", result.BucketToNotify)
	}
	if result.NewState.NotifiedBucket == nil || *result.NewState.NotifiedBucket != Bucket50 {
		t.Fatalf("notifiedBucket = %#v", result.NewState.NotifiedBucket)
	}
}

func TestDecideBucket_NotifiesMostSevereOnly(t *testing.T) {
	result := DecideBucket(WindowInput{UsedPercentage: 92, ResetsAt: resetsAt}, nil)
	if result.BucketToNotify == nil || *result.BucketToNotify != Bucket10 {
		t.Fatalf("bucketToNotify = %#v, want 10", result.BucketToNotify)
	}
}

func TestDecideBucket_Boundary50(t *testing.T) {
	result := DecideBucket(WindowInput{UsedPercentage: 50, ResetsAt: resetsAt}, nil)
	if result.BucketToNotify == nil || *result.BucketToNotify != Bucket50 {
		t.Fatalf("bucketToNotify = %#v", result.BucketToNotify)
	}
}

func TestDecideBucket_EscalatesFrom50(t *testing.T) {
	b := Bucket50
	prev := &WindowState{ResetsAt: resetsAt, NotifiedBucket: &b, FailedNotifyAttempts: 0}
	result := DecideBucket(WindowInput{UsedPercentage: 92, ResetsAt: resetsAt}, prev)
	if result.BucketToNotify == nil || *result.BucketToNotify != Bucket10 {
		t.Fatalf("bucketToNotify = %#v, want 10", result.BucketToNotify)
	}
}

func TestDecideBucket_NoRenotifySameSeverity(t *testing.T) {
	b := Bucket20
	prev := &WindowState{ResetsAt: resetsAt, NotifiedBucket: &b, FailedNotifyAttempts: 0}
	result := DecideBucket(WindowInput{UsedPercentage: 82, ResetsAt: resetsAt}, prev)
	if result.BucketToNotify != nil {
		t.Fatalf("bucketToNotify = %#v, want nil", result.BucketToNotify)
	}
	if result.NewState.NotifiedBucket == nil || *result.NewState.NotifiedBucket != Bucket20 {
		t.Fatalf("notifiedBucket = %#v", result.NewState.NotifiedBucket)
	}
}

func TestDecideBucket_NewWindowResets(t *testing.T) {
	b := Bucket5
	prev := &WindowState{ResetsAt: resetsAt, NotifiedBucket: &b, FailedNotifyAttempts: 3}
	newResetsAt := resetsAt + 18_000
	result := DecideBucket(WindowInput{UsedPercentage: 55, ResetsAt: newResetsAt}, prev)
	if result.BucketToNotify == nil || *result.BucketToNotify != Bucket50 {
		t.Fatalf("bucketToNotify = %#v", result.BucketToNotify)
	}
	if result.NewState.ResetsAt != newResetsAt {
		t.Fatalf("resetsAt = %d", result.NewState.ResetsAt)
	}
	if result.NewState.FailedNotifyAttempts != 0 {
		t.Fatalf("failedNotifyAttempts = %d", result.NewState.FailedNotifyAttempts)
	}
}

func TestDecideBucket_ImprovedRemaining(t *testing.T) {
	b := Bucket20
	prev := &WindowState{ResetsAt: resetsAt, NotifiedBucket: &b, FailedNotifyAttempts: 0}
	result := DecideBucket(WindowInput{UsedPercentage: 60, ResetsAt: resetsAt}, prev)
	if result.BucketToNotify != nil {
		t.Fatalf("bucketToNotify = %#v, want nil", result.BucketToNotify)
	}
	if result.NewState.NotifiedBucket == nil || *result.NewState.NotifiedBucket != Bucket20 {
		t.Fatalf("notifiedBucket = %#v", result.NewState.NotifiedBucket)
	}
}

func TestDecideBucket_NoThreshold(t *testing.T) {
	result := DecideBucket(WindowInput{UsedPercentage: 30, ResetsAt: resetsAt}, nil)
	if result.BucketToNotify != nil {
		t.Fatalf("bucketToNotify = %#v, want nil", result.BucketToNotify)
	}
	if result.NewState.NotifiedBucket != nil {
		t.Fatalf("notifiedBucket = %#v, want nil", result.NewState.NotifiedBucket)
	}
}
