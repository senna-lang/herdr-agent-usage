/**
 * Aliases into internal/limitscore's account-keyed window pool (moved there
 * so provider adapters can reach it without importing internal/limits; see
 * internal/limitscore/windowpool.go for the implementation).
 */
package limits

import "github.com/senna-lang/herdr-agent-usage/internal/limitscore"

type AccountWindows = limitscore.AccountWindows

var limitIDSlotTables = limitscore.LimitIDSlotTables

var SelectAccountWindows = limitscore.SelectAccountWindows

var BorrowedProviderLimits = limitscore.BorrowedProviderLimits
