/**
 * UsageProvider for the Antigravity CLI (`agy`).
 *
 * Antigravity exposes context occupancy and weekly quota only through its
 * statusLine, the same delivery mechanism Cursor uses; see the package
 * comment on snapshot.go for why no on-disk usage file is read directly. It
 * is registered as CapOwnsSubscriptionQuota: unlike Cursor, its statusLine
 * payload includes account-wide quota windows (see
 * internal/limits/antigravitylimits.go), so it owns a subscription quota in
 * addition to context.
 */
package antigravity

import (
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the Antigravity CLI UsageProvider.
var Provider = provider.FuncProvider{
	ID:   "agy",
	Func: resolveAntigravityUsage,
}

func nowUnixMs() int64 { return time.Now().UnixMilli() }
