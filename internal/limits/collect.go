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
	Claude   LimitsCollector
	Codex    LimitsCollector
	OpenCode LimitsCollector
	Grok     LimitsCollector
	// Attach activity after collection (injectable for tests).
	Attach func(providers []ProviderLimits, nowMs int64) []ProviderLimits
}

// DefaultCollectOptions wires production local collectors (no network).
func DefaultCollectOptions() CollectOptions {
	return CollectOptions{
		Claude: func(_ *string, nowMs int64) ProviderLimits {
			return CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{})
		},
		Codex: CollectCodexLimits,
		OpenCode: func(_ *string, nowMs int64) ProviderLimits {
			return CollectOpenCodeLimits(nowMs, "")
		},
		Grok: func(_ *string, nowMs int64) ProviderLimits {
			return CollectGrokLimits(nowMs, CollectGrokLimitsOptions{})
		},
	}
}

// CollectAllProviderLimits runs collectors in display order:
// Claude -> Codex -> OpenCode -> Grok, then attaches pane activity when configured.
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
	base := []ProviderLimits{
		collect(opts.Claude, "claude", "Claude"),
		collect(opts.Codex, "codex", "Codex"),
		collect(opts.OpenCode, "opencode", "OpenCode"),
		collect(opts.Grok, "grok", "Grok"),
	}
	if opts.Attach != nil {
		return opts.Attach(base, nowMs)
	}
	return base
}

func strPtr(s string) *string { return &s }
