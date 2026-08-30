/**
 * Fans a collected ProviderLimits snapshot out to open panes' $limit tokens.
 *
 * $limit is account state, so one collect updates every pane that bills the
 * same account. $context stays event-driven except the multi-profile limit
 * prefix that already lives on that row. Empty snapshots never blank a
 * last-known-good token. Pay-as-you-go burn is pane-session state and is
 * left to the event path.
 */
package update

import (
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
)

// LimitPublishPane is one open pane as the publisher sees it.
// Billing and profile identity are resolved by the caller so this type
// stays provider-neutral.
type LimitPublishPane struct {
	PaneID           string
	Resolved         bool
	BillingMode      limits.BillingMode
	LimitsProviderID string
	MultiProfile     bool
	AccountLabel     string
	Tokens           map[string]string
}

// LimitPublishTarget is one metadata write. ContextToken is nil when $context
// must not be touched.
type LimitPublishTarget struct {
	PaneID       string
	LimitToken   string
	ContextToken *string
}

// LimitPublishTargets decides $limit (and multi-profile $context prefix)
// writes from one collected snapshot. It does not I/O.
func LimitPublishTargets(providers []limits.ProviderLimits, panes []LimitPublishPane, nowMs int64, percent core.LimitPercent) []LimitPublishTarget {
	byID := make(map[string]limits.ProviderLimits, len(providers))
	for _, provider := range providers {
		byID[provider.ProviderID] = provider
	}
	var out []LimitPublishTarget
	for _, pane := range panes {
		if !pane.Resolved || pane.BillingMode == limits.BillingPayAsYouGo {
			continue
		}
		provider, ok := byID[pane.LimitsProviderID]
		if !ok {
			continue
		}
		limitText := limits.FormatSidebarLimit(provider, nowMs, percent)
		if limitText == "" {
			continue
		}
		target := LimitPublishTarget{PaneID: pane.PaneID, LimitToken: limitText}
		if pane.MultiProfile && pane.AccountLabel != "" {
			target.LimitToken = pane.AccountLabel
			ctx := replaceContextLimitPrefix(tokenValue(pane.Tokens, "context"), limitText)
			target.ContextToken = &ctx
		}
		out = append(out, target)
	}
	return out
}

func tokenValue(tokens map[string]string, name string) string {
	if tokens == nil {
		return ""
	}
	return tokens[name]
}

// replaceContextLimitPrefix updates the limit segment parked on $context for
// multi-profile panes without recomputing the context meter.
func replaceContextLimitPrefix(current, limitText string) string {
	if limitText == "" {
		return current
	}
	if current == "" {
		return limitText
	}
	if i := strings.Index(current, " · "); i >= 0 {
		return limitText + current[i:]
	}
	if strings.Contains(current, "⛁") || strings.Contains(current, "compacted") {
		return limitText + " · " + current
	}
	return limitText
}

func applyLimitPublishTargets(writer metadataTokenWriter, panes []LimitPublishPane, targets []LimitPublishTarget) {
	tokensByPane := make(map[string]map[string]string, len(panes))
	for _, pane := range panes {
		tokensByPane[pane.PaneID] = pane.Tokens
	}
	for _, target := range targets {
		current := tokensByPane[target.PaneID]
		writeMetadataTokenWith(writer, current, target.PaneID, "limit", target.LimitToken, false)
		if target.ContextToken != nil {
			writeMetadataTokenWith(writer, current, target.PaneID, "context", *target.ContextToken, false)
		}
	}
}

func sidebarAccountIdentity(
	agent, providerID string,
	claudeProfiles []claude.ClaudeProfile,
	codexProfiles []codex.CodexProfile,
	grokProfiles []grok.GrokProfile,
	openCodeProfiles []opencode.OpenCodeProfile,
) (multi bool, account string) {
	multi = (agent == "claude" && len(claudeProfiles) > 1) ||
		(agent == "codex" && len(codexProfiles) > 1) ||
		(agent == "grok" && len(grokProfiles) > 1) ||
		(agent == "opencode" && len(openCodeProfiles) > 1)
	if !multi {
		return false, ""
	}
	switch agent {
	case "claude":
		if profile, ok := findClaudeProfile(claudeProfiles, providerID); ok {
			return true, resolveSidebarAccountLabel(profile)
		}
	case "codex":
		if profile, ok := findCodexProfile(codexProfiles, providerID); ok {
			return true, profile.Label
		}
	case "grok":
		if profile, ok := findGrokProfile(grokProfiles, providerID); ok {
			return true, profile.Label
		}
	case "opencode":
		if profile, ok := findOpenCodeProfile(openCodeProfiles, providerID); ok {
			return true, profile.Label
		}
	}
	return true, ""
}

// PublishCollectedLimits writes $limit for every open subscription pane from
// an already-collected snapshot. A failed pane query is a no-op so a
// transient herdr error cannot blank rows.
func PublishCollectedLimits(providers []limits.ProviderLimits, nowMs int64) {
	PublishCollectedLimitsWith(herdrMetadataTokenWriter, herdrcli.GetPaneInfo, providers, nowMs)
}

func PublishCollectedLimitsWith(
	writer metadataTokenWriter,
	getPane func(string) herdrcli.PaneInfo,
	providers []limits.ProviderLimits,
	nowMs int64,
) {
	snaps, ok := ListOpenPaneSnapshots()
	if !ok {
		return
	}
	claudeProfiles := limits.ResolvedClaudeProfiles()
	codexProfiles := limits.ResolvedCodexProfiles()
	grokProfiles := limits.ResolvedGrokProfiles()
	openCodeProfiles := limits.ResolvedOpenCodeProfiles()
	resolve := limits.BuildHarnessPaneProviderResolver(claudeProfiles, codexProfiles, grokProfiles, openCodeProfiles)
	billing := limits.DefaultBillingDeps()
	panes := make([]LimitPublishPane, 0, len(snaps))
	for _, snap := range snaps {
		providerID, resolved := resolve(snap)
		info := getPane(snap.PaneID)
		pane := LimitPublishPane{
			PaneID:   snap.PaneID,
			Resolved: resolved,
			Tokens:   info.Tokens,
		}
		if resolved {
			pane.BillingMode = limits.PaneBillingMode(providerID, snap, billing)
			pane.LimitsProviderID = limits.SubscriptionLimitsProviderID(providerID, snap)
			pane.MultiProfile, pane.AccountLabel = sidebarAccountIdentity(
				snap.Agent, providerID, claudeProfiles, codexProfiles, grokProfiles, openCodeProfiles,
			)
		}
		panes = append(panes, pane)
	}
	applyLimitPublishTargets(writer, panes, LimitPublishTargets(providers, panes, nowMs, limits.ResolvedLimitPercent()))

}
