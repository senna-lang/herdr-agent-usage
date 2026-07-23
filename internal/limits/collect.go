/**
 * Facade that aggregates per-provider limits for the panel.
 *
 * Default collectors use local files/DBs. Overrides remain injectable for tests.
 */
package limits

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

// CollectOptions configures CollectAllProviderLimits.
type CollectOptions struct {
	// Claude is one entry per configured Claude profile, in config order. Empty
	// synthesizes a single "claude" stub spec, matching how the other
	// providers behave when left unconfigured (mainly relevant to tests that
	// build CollectOptions directly rather than via DefaultCollectOptions).
	Claude   []ClaudeProfileCollector
	Codex    LimitsCollector
	OpenCode LimitsCollector
	Grok     LimitsCollector
	// Attach activity after collection (injectable for tests).
	Attach func(providers []ProviderLimits, nowMs int64) []ProviderLimits
	// Only restricts collection to these provider ids (nil = all providers).
	// Filtered providers are skipped entirely: their collectors never run.
	Only map[string]bool
}

// DefaultCollectOptions wires production local collectors (no network), one
// Claude collector per configured profile (see ResolvedClaudeProfiles).
func DefaultCollectOptions() CollectOptions {
	profiles := ResolvedClaudeProfiles()
	multiProfile := len(profiles) > 1
	claudeCollectors := make([]ClaudeProfileCollector, len(profiles))
	for i, p := range profiles {
		claudeCollectors[i] = ClaudeProfileCollector{
			ID:    p.ID,
			Label: p.Label,
			Collector: func(_ *string, nowMs int64) ProviderLimits {
				pl := CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{
					StatusLineCachePath: p.LimitsCache,
					ClaudeJSONPath:      p.JSONPath,
				})
				pl.ProviderID = p.ID
				pl.Label = p.Label
				// When 2+ accounts are configured, every row nests under one
				// shared "Claude" group in the panel, labeled by its real
				// logged-in email rather than the profile's own label — so
				// the account behind each row is always verifiable.
				return applyProfileGrouping(pl, p, multiProfile)
			},
		}
	}
	return CollectOptions{
		Claude: claudeCollectors,
		Codex:  CollectCodexLimits,
		OpenCode: func(_ *string, nowMs int64) ProviderLimits {
			return CollectOpenCodeLimits(nowMs, "")
		},
		Grok: func(_ *string, nowMs int64) ProviderLimits {
			return CollectGrokLimits(nowMs, CollectGrokLimitsOptions{})
		},
	}
}

// CollectAllProviderLimits runs collectors in display order: each configured
// Claude profile (config order) -> Codex -> OpenCode -> Grok, then attaches
// pane activity when configured. Providers excluded by opts.Only are skipped
// (collectors never run). Pass DefaultCollectOptions() for production local
// collectors.
func CollectAllProviderLimits(cwd *string, nowMs int64, opts CollectOptions) []ProviderLimits {
	collect := func(c LimitsCollector, id, label string) ProviderLimits {
		if c != nil {
			return c(cwd, nowMs)
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

	base := make([]ProviderLimits, 0, len(claudeSpecs)+3)
	for _, s := range claudeSpecs {
		if opts.Only != nil && !opts.Only[s.ID] {
			continue
		}
		base = append(base, collect(s.Collector, s.ID, s.Label))
	}

	otherSpecs := []struct {
		collector LimitsCollector
		id, label string
	}{
		{opts.Codex, "codex", "Codex"},
		{opts.OpenCode, "opencode", "OpenCode"},
		{opts.Grok, "grok", "Grok"},
	}
	for _, s := range otherSpecs {
		if opts.Only != nil && !opts.Only[s.id] {
			continue
		}
		base = append(base, collect(s.collector, s.id, s.label))
	}

	if opts.Attach != nil {
		return opts.Attach(base, nowMs)
	}
	return base
}

func strPtr(s string) *string { return &s }
