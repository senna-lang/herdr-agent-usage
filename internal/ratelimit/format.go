/**
 * Builds the text of a rate-limit notification.
 */
package ratelimit

import (
	"fmt"
	"strconv"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// WindowKey is a Claude rate-limit window label key.
type WindowKey string

const (
	WindowFiveHour WindowKey = "fiveHour"
	WindowSevenDay WindowKey = "sevenDay"
)

var windowLabel = map[WindowKey]string{
	WindowFiveHour: "Session limit",
	WindowSevenDay: "Weekly limit",
}

// NotificationText is a toast title + body.
type NotificationText struct {
	Title string
	Body  string
}

func formatDuration(remainingMs int64) string {
	if remainingMs <= 0 {
		return "0m"
	}
	totalMinutes := remainingMs / 60_000
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	minutes := totalMinutes % 60
	if days > 0 {
		return strconv.FormatInt(days, 10) + "d " + strconv.FormatInt(hours, 10) + "h"
	}
	if hours > 0 {
		return strconv.FormatInt(hours, 10) + "h " + strconv.FormatInt(minutes, 10) + "m"
	}
	return strconv.FormatInt(minutes, 10) + "m"
}

// FormatNotificationBody builds Claude window notification text.
// remainingBucket is the configured remaining-percent threshold. percent
// only changes the rendered number and word; firing still uses remaining.
func FormatNotificationBody(
	windowKey WindowKey,
	remainingBucket Bucket,
	resetsAtEpochSeconds int64,
	nowMs int64,
	percent core.LimitPercent,
) NotificationText {
	remainingMs := resetsAtEpochSeconds*1000 - nowMs
	return NotificationText{
		Title: windowLabel[windowKey],
		Body:  formatLimitBucketBody(remainingBucket, remainingMs, percent),
	}
}

// FormatProviderPrimaryNotification formats an alert for a non-Claude provider's shortest window.
func FormatProviderPrimaryNotification(
	providerLabel string,
	remainingBucket Bucket,
	resetsAtEpochSeconds int64,
	nowMs int64,
	percent core.LimitPercent,
) NotificationText {
	remainingMs := resetsAtEpochSeconds*1000 - nowMs
	return NotificationText{
		Title: providerLabel + " limit",
		Body:  formatLimitBucketBody(remainingBucket, remainingMs, percent),
	}
}

func formatLimitBucketBody(remainingBucket Bucket, remainingMs int64, percent core.LimitPercent) string {
	word := "remaining"
	if percent.Used() {
		word = "used"
	}
	return fmt.Sprintf("%d%% %s · resets in %s", percent.InvertRemainingBucket(string(remainingBucket)), word, formatDuration(remainingMs))
}
