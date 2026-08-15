/**
 * Per-provider rate-limit snapshot used for display.
 *
 * The concrete definitions live in internal/limitscore, one layer below both
 * internal/limits and every provider package, since provider adapters must
 * construct ProviderLimits without importing internal/limits (see
 * internal/limitscore/types.go). These aliases keep every existing call site
 * in this package unqualified.
 */
package limits

import "github.com/senna-lang/herdr-agent-usage/internal/limitscore"

type RunOutEstimate = limitscore.RunOutEstimate

type LimitWindow = limitscore.LimitWindow

type ProviderLimits = limitscore.ProviderLimits

type ProviderPaneActivity = limitscore.ProviderPaneActivity
