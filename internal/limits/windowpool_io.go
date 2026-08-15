/**
 * Aliases into internal/limitscore's account-window-pool I/O (moved there so
 * provider adapters can reach it without importing internal/limits; see
 * internal/limitscore/windowpool_io.go for the implementation).
 */
package limits

import "github.com/senna-lang/herdr-agent-usage/internal/limitscore"

var ObserveAccountWindows = limitscore.ObserveAccountWindows

var borrowWindows = limitscore.BorrowWindows
