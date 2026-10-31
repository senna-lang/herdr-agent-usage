/**
 * UsageProvider for Devin CLI.
 *
 * Herdr's Devin integration reports agent_session.kind=id where the value
 * is the session's own id (e.g. "bejeweled-whitefish"), which is the
 * primary key of cli/sessions.db, so resolution is an exact lookup with no
 * cwd fallback: two panes in one directory are distinct sessions.
 *
 * Devin sessions always bill against the account's plan quota — the CLI
 * offers no per-session backend choice — so the store records no billing
 * route to classify and this adapter does not implement
 * SessionBillingProvider; billingmode_io maps the provider id to
 * subscription directly. The context window size is left unset: no local
 * surface reports a model's limit, and inventing a denominator would draw
 * a percentage against a number that does not exist (see
 * docs/devin-contract.md). The cache row falls back to the newest
 * request's counters: session-cumulative cache requires a full scan of the
 * node forest, which is too expensive to run on every pane event.
 */
package devin

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

type usageProvider struct{}

// Provider is the Devin CLI UsageProvider.
var Provider usageProvider

func (usageProvider) AgentID() string { return "devin" }

func (usageProvider) ResolveUsage(input provider.UsageResolveInput) *core.ContextUsage {
	sessionID := provider.SessionID(input)
	if sessionID == nil {
		return nil
	}
	return ResolveUsageIn(ResolveDevinDataDir(), *sessionID)
}

// ResolveUsageIn reads one exact session from a Devin data directory.
func ResolveUsageIn(dataDir, sessionID string) *core.ContextUsage {
	if dataDir == "" || sessionID == "" {
		return nil
	}
	db := openSessionsDB(ResolveDevinSessionsDBPathIn(dataDir))
	if db == nil {
		return nil
	}
	defer db.Close()
	if !sessionExists(db, sessionID) {
		return nil
	}

	usage := &core.ContextUsage{}
	if latest, ok := readLatestMetrics(db, sessionID); ok {
		usage.ContextTokens = nonNegative(latest.ContextInput)
		usage.Cache = core.CacheFromTokenCounts(nonNegative(latest.Input), nonNegative(latest.CacheRead), nonNegative(latest.CacheCreate))
		if usage.Cache != nil && latest.CreatedAtSec > 0 {
			usage.Cache.LastActivityUnix = &latest.CreatedAtSec
		}
	} else {
		usage.ContextUnavailable = true
	}
	return usage
}

func nonNegative(n int) int {
	if n > 0 {
		return n
	}
	return 0
}

var _ provider.UsageProvider = usageProvider{}
