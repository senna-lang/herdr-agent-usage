/**
 * Type definitions for the rate-limit notification feature.
 */
package ratelimit

// Bucket is a remaining-percentage threshold bucket.
type Bucket string

const (
	Bucket50 Bucket = "50"
	Bucket20 Bucket = "20"
	Bucket10 Bucket = "10"
	Bucket5  Bucket = "5"
)

// BucketOrder is most-severe last among crossed thresholds when iterating.
var BucketOrder = []Bucket{Bucket50, Bucket20, Bucket10, Bucket5}

// WindowState is the notification state for one rate-limit window.
type WindowState struct {
	ResetsAt             int64
	NotifiedBucket       *Bucket
	FailedNotifyAttempts int
}

// MaxFailedNotifyAttempts: once shown=false reaches this count we give up the toast.
const MaxFailedNotifyAttempts = 5

// NotifyState is Claude five-hour / seven-day window state.
type NotifyState struct {
	FiveHour *WindowState
	SevenDay *WindowState
}

// WindowInput is the current reading for a rate-limit window.
type WindowInput struct {
	UsedPercentage float64
	ResetsAt       int64
}

// BucketDecision is the next state plus optional notification bucket.
type BucketDecision struct {
	NewState       WindowState
	BucketToNotify *Bucket
}
