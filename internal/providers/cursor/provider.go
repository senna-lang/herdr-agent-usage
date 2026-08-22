/**
 * UsageProvider for the Cursor CLI.
 *
 * Cursor reports context occupancy only through its statusLine, and reports no
 * rate-limit windows locally at all, so this provider resolves context usage
 * and nothing else. It is registered as CapContextOnly: it owns no subscription
 * quota and routes to no other provider's collector.
 */
package cursor

import (
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the Cursor UsageProvider.
var Provider = provider.FuncProvider{
	ID:   "cursor",
	Func: resolveCursorUsage,
}

func nowUnixMs() int64 { return time.Now().UnixMilli() }
