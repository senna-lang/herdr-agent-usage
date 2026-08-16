/**
 * Facade that aggregates per-provider limits for the panel.
 *
 * Default collectors use local files/DBs. Overrides remain injectable for tests.
 */
package limits

import (
	"path/filepath"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
)

// LimitsCollector fetches one provider's rate-limit snapshot.
type LimitsCollector func(cwd *string, nowMs int64) ProviderLimits

// ClaudeProfileCollector is one configured Claude profile's collector, keyed by
// that profile's own provider id/label so multiple accounts collect and display
// independently instead of sharing the single literal "claude" id.
type ClaudeProfileCollector struct {
	ID        string
	Label     string
	Collector LimitsCollector
}

// CodexProfileCollector is one configured Codex profile's collector. Same
// shape as ClaudeProfileCollector: each account collects and displays under
// its own provider id instead of sharing the single literal "codex" id.
type CodexProfileCollector = ClaudeProfileCollector

// GrokProfileCollector is one configured Grok profile's collector.
type GrokProfileCollector = ClaudeProfileCollector

// OpenCodeProfileCollector is one configured OpenCode profile's collector.
type OpenCodeProfileCollector = ClaudeProfileCollector

// CollectOptions configures CollectAllProviderLimits.
type CollectOptions struct {
	// Each profile family is collected in configuration order. Empty profile
	// slices synthesize their literal default collector for direct test callers.
	Claude   []ClaudeProfileCollector
	Codex    []CodexProfileCollector
	OpenCode []OpenCodeProfileCollector
	Grok     []GrokProfileCollector
	// Attach activity after collection (injectable for tests).
	Attach func(providers []ProviderLimits, nowMs int64) []ProviderLimits
	// Only restricts collection to these provider ids (nil = all providers).
	// Filtered providers are skipped entirely: their collectors never run.
	Only map[string]bool
}

// DefaultCollectOptions wires production local collectors (no network), one
// Claude/Codex collector per configured profile.
func DefaultCollectOptions() CollectOptions {
	profiles := ResolvedClaudeProfiles()
	multiProfile := len(profiles) > 1
	claudeCollectors := make([]ClaudeProfileCollector, len(profiles))
	for i, profile := range profiles {
		claudeCollectors[i] = ClaudeProfileCollector{
			ID:    profile.ID,
			Label: profile.Label,
			Collector: func(_ *string, nowMs int64) ProviderLimits {
				pl := CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{
					StatusLineCachePath: profile.LimitsCache,
					ClaudeJSONPath:      profile.JSONPath,
				})
				pl.ProviderID = profile.ID
				pl.Label = profile.Label
				// When 2+ accounts are configured, every row nests under one
				// shared "Claude" group in the panel, labeled by its real
				// logged-in email rather than the profile's own label — so
				// the account behind each row is always verifiable.
				return applyProfileGrouping(pl, profile, multiProfile)
			},
		}
	}

	codexProfiles := ResolvedCodexProfiles()
	multiCodex := len(codexProfiles) > 1
	codexCollectors := make([]CodexProfileCollector, len(codexProfiles))
	for i, profile := range codexProfiles {
		codexCollectors[i] = CodexProfileCollector{
			ID:    profile.ID,
			Label: profile.Label,
			Collector: func(_ *string, nowMs int64) ProviderLimits {
				pl := CollectCodexLimitsIn(profile.Home, profile.ID, profile.Label, nowMs)
				return applyCodexProfileGrouping(pl, profile, multiCodex)
			},
		}
	}

	grokProfiles := ResolvedGrokProfiles()
	multiGrok := len(grokProfiles) > 1
	grokCollectors := make([]GrokProfileCollector, len(grokProfiles))
	for i, profile := range grokProfiles {
		grokCollectors[i] = GrokProfileCollector{
			ID:    profile.ID,
			Label: profile.Label,
			Collector: func(_ *string, nowMs int64) ProviderLimits {
				authPath := ""
				if !profile.Implicit {
					authPath = filepath.Join(profile.Home, "auth.json")
				}
				pl := CollectGrokLimits(nowMs, CollectGrokLimitsOptions{AuthPath: authPath})
				pl.ProviderID = profile.ID
				pl.Label = profile.Label
				return applyGrokProfileGrouping(pl, profile, multiGrok)
			},
		}
	}

	openCodeProfiles := ResolvedOpenCodeProfiles()
	multiOpenCode := len(openCodeProfiles) > 1
	openCodeCollectors := make([]OpenCodeProfileCollector, len(openCodeProfiles))
	for i, profile := range openCodeProfiles {
		openCodeCollectors[i] = OpenCodeProfileCollector{
			ID:    profile.ID,
			Label: profile.Label,
			Collector: func(_ *string, nowMs int64) ProviderLimits {
				dbPath := ""
				if !profile.Implicit {
					dbPath = opencode.ResolveOpenCodeDBPathIn(profile.DataDir)
				}
				pl := CollectOpenCodeLimits(nowMs, dbPath)
				pl.ProviderID = profile.ID
				pl.Label = profile.Label
				return applyOpenCodeProfileGrouping(pl, profile, multiOpenCode)
			},
		}
	}

	return CollectOptions{
		Claude:   claudeCollectors,
		Codex:    codexCollectors,
		Grok:     grokCollectors,
		OpenCode: openCodeCollectors,
	}
}

// singleCollectorQuotaSpecs pairs each still-single quota-owning provider
// id/label with the CollectOptions field carrying its collector. Claude and
// Codex are omitted because they expand to one collector per configured
// profile. This list's id set is checked against
// providers.IDsWithCapability(CapOwnsSubscriptionQuota) minus those profile
// families by TestSingleCollectorQuotaSpecs_MatchCapabilityRegistrations, so a
// newly registered quota-owning provider that isn't wired here fails that
// test instead of silently never appearing in the panel.
var singleCollectorQuotaSpecs = []struct {
	id, label string
	field     func(CollectOptions) LimitsCollector
}{}

// CollectAllProviderLimits runs collectors in display order: each configured
// Claude profile (config order) -> Codex -> OpenCode -> Grok, then attaches
// pane activity when configured. Providers excluded by opts.Only are skipped
// (collectors never run). Pass DefaultCollectOptions() for production local
// collectors.
func CollectAllProviderLimits(cwd *string, nowMs int64, opts CollectOptions) []ProviderLimits {
	collect := func(collector LimitsCollector, id, label string) ProviderLimits {
		if collector != nil {
			return collector(cwd, nowMs)
		}
		return ProviderLimits{
			ProviderID:  id,
			Label:       label,
			Source:      "stub",
			FetchedAtMs: nowMs,
			Note:        strPtr("limits collector not configured"),
		}
	}

	claudeSpecs := opts.Claude
	if len(claudeSpecs) == 0 {
		claudeSpecs = []ClaudeProfileCollector{{ID: "claude", Label: "Claude"}}
	}
	codexSpecs := opts.Codex
	if len(codexSpecs) == 0 {
		codexSpecs = []CodexProfileCollector{{ID: "codex", Label: "Codex"}}
	}
	openCodeSpecs := opts.OpenCode
	if len(openCodeSpecs) == 0 {
		openCodeSpecs = []OpenCodeProfileCollector{{ID: "opencode", Label: "OpenCode"}}
	}
	grokSpecs := opts.Grok
	if len(grokSpecs) == 0 {
		grokSpecs = []GrokProfileCollector{{ID: "grok", Label: "Grok"}}
	}

	base := make([]ProviderLimits, 0, len(claudeSpecs)+len(codexSpecs)+len(openCodeSpecs)+len(grokSpecs)+len(singleCollectorQuotaSpecs))
	for _, spec := range claudeSpecs {
		if opts.Only == nil || opts.Only[spec.ID] {
			base = append(base, collect(spec.Collector, spec.ID, spec.Label))
		}
	}
	for _, spec := range codexSpecs {
		if opts.Only == nil || opts.Only[spec.ID] {
			base = append(base, collect(spec.Collector, spec.ID, spec.Label))
		}
	}
	for _, spec := range openCodeSpecs {
		if opts.Only == nil || opts.Only[spec.ID] {
			base = append(base, collect(spec.Collector, spec.ID, spec.Label))
		}
	}
	for _, spec := range grokSpecs {
		if opts.Only == nil || opts.Only[spec.ID] {
			base = append(base, collect(spec.Collector, spec.ID, spec.Label))
		}
	}
	for _, spec := range singleCollectorQuotaSpecs {
		if opts.Only == nil || opts.Only[spec.id] {
			base = append(base, collect(spec.field(opts), spec.id, spec.label))
		}
	}

	if opts.Attach != nil {
		return opts.Attach(base, nowMs)
	}
	return base
}

func strPtr(s string) *string { return &s }
