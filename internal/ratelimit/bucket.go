/**
 * Pure function that decides the bucket (50/20/10/5%) from the remaining
 * rate-limit percentage.
 */
package ratelimit

var bucketRemainingThreshold = map[Bucket]float64{
	Bucket50: 50,
	Bucket20: 20,
	Bucket10: 10,
	Bucket5:  5,
}

func remainingPercentageOf(usedPercentage float64) float64 {
	return 100 - usedPercentage
}

// worstBucketFor returns the most severe bucket that remaining has entered, or nil.
func worstBucketFor(remainingPercentage float64) *Bucket {
	var worst *Bucket
	for i := range BucketOrder {
		b := BucketOrder[i]
		if remainingPercentage <= bucketRemainingThreshold[b] {
			worst = &b
		}
	}
	return worst
}

func severityRankOf(bucket *Bucket) int {
	if bucket == nil {
		return -1
	}
	for i, b := range BucketOrder {
		if b == *bucket {
			return i
		}
	}
	return -1
}

// DecideBucket decides the next state and which bucket (if any) should trigger a notification.
//
// - When resetsAt differs from the previous reading, treat it as a new window and reset notified state.
// - When multiple buckets are crossed in a single step, notify only for the most severe one.
// - When the remaining percentage has improved (not worsened), do not notify.
func DecideBucket(input WindowInput, previous *WindowState) BucketDecision {
	isNewWindow := previous == nil || previous.ResetsAt != input.ResetsAt
	var notifiedBucket *Bucket
	if !isNewWindow && previous != nil {
		notifiedBucket = previous.NotifiedBucket
	}

	remaining := remainingPercentageOf(input.UsedPercentage)
	currentWorst := worstBucketFor(remaining)

	shouldNotify := currentWorst != nil && severityRankOf(currentWorst) > severityRankOf(notifiedBucket)

	var newNotified *Bucket
	if shouldNotify {
		newNotified = currentWorst
	} else {
		newNotified = notifiedBucket
	}

	var bucketToNotify *Bucket
	if shouldNotify {
		bucketToNotify = currentWorst
	}

	return BucketDecision{
		NewState: WindowState{
			ResetsAt:             input.ResetsAt,
			NotifiedBucket:       newNotified,
			FailedNotifyAttempts: 0,
		},
		BucketToNotify: bucketToNotify,
	}
}
