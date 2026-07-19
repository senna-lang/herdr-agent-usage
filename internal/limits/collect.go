/**
 * Facade that aggregates per-provider limits for the panel.
 *
 * Default collectors use local files/DBs. Overrides remain injectable for tests.
 */
package limits

// LimitsCollector fetches one provider's rate-limit snapshot.
type LimitsCollector func(cwd *string, nowMs int64) ProviderLimits

// CollectOptions configures CollectAllProviderLimits.
type CollectOptions struct {
	Claude      LimitsCollector
	Codex       LimitsCollector
	Antigravity LimitsCollector
	Zai         LimitsCollector
	OpenCode    LimitsCollector
	Grok        LimitsCollector
	// Attach activity after collection (injectable for tests).
	Attach func(providers []ProviderLimits, nowMs int64) []ProviderLimits
	// Only restricts collection to these provider ids (nil = all providers).
	// Filtered providers are skipped entirely: their collectors never run.
	Only map[string]bool
}

// DefaultCollectOptions wires production local collectors (no network).
func DefaultCollectOptions() CollectOptions {
	return CollectOptions{
		Claude: func(_ *string, nowMs int64) ProviderLimits {
			return CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{})
		},
		Codex: CollectCodexLimits,
		Antigravity: func(_ *string, nowMs int64) ProviderLimits {
			return CollectAntigravityLimits(nil, nowMs, CollectAntigravityLimitsOptions{})
		},
		Zai: func(_ *string, nowMs int64) ProviderLimits {
			return CollectZaiLimits(nil, nowMs, CollectZaiLimitsOptions{})
		},
		OpenCode: func(_ *string, nowMs int64) ProviderLimits {
			return CollectOpenCodeLimits(nowMs, "")
		},
		Grok: func(_ *string, nowMs int64) ProviderLimits {
			return CollectGrokLimits(nowMs, CollectGrokLimitsOptions{})
		},
	}
}

// CollectAllProviderLimits runs collectors in display order:
// Claude -> Codex -> Antigravity -> Z.ai -> OpenCode -> Grok, then attaches
// pane activity when configured.
// Providers excluded by opts.Only are skipped (collectors never run).
// Pass DefaultCollectOptions() for production local collectors.
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
	specs := []struct {
		collector LimitsCollector
		id, label string
	}{
		{opts.Claude, "claude", "Claude"},
		{opts.Codex, "codex", "Codex"},
		{opts.Antigravity, "antigravity", "Antigravity"},
		{opts.Zai, "zai", "Z.ai"},
		{opts.OpenCode, "opencode", "OpenCode"},
		{opts.Grok, "grok", "Grok"},
	}
	base := make([]ProviderLimits, 0, len(specs))
	for _, s := range specs {
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
