/**
 * Pure extractors for OMP session jsonl lines.
 */
package omp

import (
	"encoding/json"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

type assistantUsage struct {
	Input        *float64 `json:"input"`
	Output       *float64 `json:"output"`
	CacheRead    *float64 `json:"cacheRead"`
	CacheWrite   *float64 `json:"cacheWrite"`
	CacheWrite1h *float64 `json:"cacheWrite1h"`
	TotalTokens  *float64 `json:"totalTokens"`
	Cost         *struct {
		Total *float64 `json:"total"`
	} `json:"cost"`
}

type assistantMessage struct {
	Role            string          `json:"role"`
	Provider        string          `json:"provider"`
	Model           string          `json:"model"`
	StopReason      string          `json:"stopReason"`
	Timestamp       int64           `json:"timestamp"`
	Usage           *assistantUsage `json:"usage"`
	ContextSnapshot *struct {
		PromptTokens *float64 `json:"promptTokens"`
	} `json:"contextSnapshot"`
}

type assistantLine struct {
	Type     string            `json:"type"`
	ID       string            `json:"id"`
	ParentID *string           `json:"parentId"`
	Message  *assistantMessage `json:"message"`
}

func parseAssistantLine(raw string) *assistantLine {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, `"role"`) {
		return nil
	}
	var parsed assistantLine
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	if parsed.Message == nil || parsed.Message.Role != "assistant" {
		return nil
	}
	return &parsed
}

func intOrZero(n *float64) int {
	if n == nil {
		return 0
	}
	return int(*n)
}

func floatOrZero(n *float64) float64 {
	if n == nil {
		return 0
	}
	return *n
}

func totalTokensOf(usage *assistantUsage) int {
	if usage == nil {
		return 0
	}
	if total := intOrZero(usage.TotalTokens); total > 0 {
		return total
	}
	return int(floatOrZero(usage.Input) + floatOrZero(usage.Output) +
		floatOrZero(usage.CacheRead) + floatOrZero(usage.CacheWrite))
}

func sessionUsageFromMessage(msg *assistantMessage) *SessionUsage {
	if msg == nil || msg.Role != "assistant" || msg.StopReason == "aborted" || msg.StopReason == "error" {
		return nil
	}
	contextTokens := 0
	if msg.ContextSnapshot != nil {
		contextTokens = intOrZero(msg.ContextSnapshot.PromptTokens)
	}
	totalTokens := totalTokensOf(msg.Usage)
	cost := 0.0
	if msg.Usage != nil && msg.Usage.Cost != nil {
		cost = floatOrZero(msg.Usage.Cost.Total)
	}
	if contextTokens <= 0 {
		contextTokens = totalTokens
	}
	if contextTokens <= 0 {
		return nil
	}
	return &SessionUsage{
		Provider:      msg.Provider,
		Model:         msg.Model,
		ContextTokens: contextTokens,
		TotalTokens:   totalTokens,
		CostUSD:       cost,
	}
}

func extractLatestUsageLinear(lines []string) *SessionUsage {
	var found *SessionUsage
	acc := cacheAccum{}
	for i := len(lines) - 1; i >= 0; i-- {
		var entry assistantLine
		if json.Unmarshal([]byte(lines[i]), &entry) != nil {
			continue
		}
		if entry.Type == "compaction" {
			if found != nil {
				break
			}
			return nil
		}
		if usage := sessionUsageFromMessage(entry.Message); usage != nil {
			if found == nil {
				found = usage
			}
			acc.add(entry.Message)
		}
	}
	return acc.apply(found)
}

func extractLatestBackendLinear(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		var entry assistantLine
		if json.Unmarshal([]byte(lines[i]), &entry) != nil {
			continue
		}
		// Compaction does not change which backend the session is on; keep scanning.
		if entry.Type == "compaction" {
			continue
		}
		if provider := backendFromMessage(entry.Message); provider != "" {
			return provider
		}
	}
	return ""
}

func backendFromMessage(msg *assistantMessage) string {
	// Match sessionUsageFromMessage's failed-row filter so billing/backend labels
	// and the context meter agree on which assistant tip is active.
	if msg == nil || msg.Role != "assistant" || msg.StopReason == "aborted" || msg.StopReason == "error" {
		return ""
	}
	return strings.TrimSpace(msg.Provider)
}

// indexTreeEntries maps id -> entry for parentId walks. leafID is the most
// recently appended id-bearing row (the persisted active leaf).
func indexTreeEntries(lines []string) (entries map[string]assistantLine, leafID string) {
	entries = make(map[string]assistantLine)
	for _, line := range lines {
		var entry assistantLine
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Type == "session" || entry.ID == "" {
			continue
		}
		entries[entry.ID] = entry
		leafID = entry.ID
	}
	return entries, leafID
}

// walkActiveBranch visits entries from the active leaf toward the root.
// missingAncestor is true when a parent id is absent from the indexed tail
// (truncated read); callers should fall back to linear scans then.
func walkActiveBranch(entries map[string]assistantLine, leafID string, visit func(assistantLine) (stop bool)) (missingAncestor bool) {
	visited := map[string]bool{}
	for leafID != "" && !visited[leafID] {
		visited[leafID] = true
		entry, ok := entries[leafID]
		if !ok {
			return true
		}
		if visit(entry) {
			return false
		}
		if entry.ParentID == nil {
			return false
		}
		leafID = *entry.ParentID
	}
	return false
}

// ExtractLatestUsageFromLines follows Pi's parentId tree from the most recently
// appended entry (the persisted active leaf) and returns the latest valid
// assistant usage on that branch. A compaction after the last assistant makes
// occupancy unknown until Pi records its next response, matching Pi's footer.
// Old OMP/Pi files without tree ids fall back to reverse line order.
func ExtractLatestUsageFromLines(lines []string) *SessionUsage {
	entries, leafID := indexTreeEntries(lines)
	if leafID == "" {
		return extractLatestUsageLinear(lines)
	}

	var found *SessionUsage
	acc := cacheAccum{}
	missing := walkActiveBranch(entries, leafID, func(entry assistantLine) bool {
		if entry.Type == "compaction" {
			return true
		}
		if usage := sessionUsageFromMessage(entry.Message); usage != nil {
			if found == nil {
				found = usage
			}
			acc.add(entry.Message)
		}
		return false
	})
	if missing {
		return extractLatestUsageLinear(lines)
	}
	return acc.apply(found)
}

// ExtractLatestBackendFromLines returns the provider id of the latest valid
// assistant on the active parentId branch (same tree tip and failed-row skip as
// ExtractLatestUsageFromLines). Compaction is not a backend boundary — walk
// continues so subscription/billing routing still resolves after compact.
// Files without tree ids fall back to reverse line order with the same filters.
func ExtractLatestBackendFromLines(lines []string) string {
	entries, leafID := indexTreeEntries(lines)
	if leafID == "" {
		return extractLatestBackendLinear(lines)
	}

	found := ""
	missing := walkActiveBranch(entries, leafID, func(entry assistantLine) bool {
		if entry.Type == "compaction" {
			return false
		}
		if provider := backendFromMessage(entry.Message); provider != "" {
			found = provider
			return true
		}
		return false
	})
	if missing {
		return extractLatestBackendLinear(lines)
	}
	return found
}

// SumUsageFromLines sums assistant turn totals across all backends in file
// order. Unlike ExtractLatestUsageFromLines, this is a burn total: it does not
// follow parentId branches and does not drop aborted/error rows, because those
// turns still consumed tokens. When startMs/endMs > 0, only events with
// timestamps inside [startMs, endMs] are counted. Timestamps of 0 are always
// included when a window is unset (startMs==0 && endMs==0), and skipped when a
// window is active.
func SumUsageFromLines(lines []string, startMs, endMs int64) (tokens float64, costUSD float64) {
	return SumUsageForProviderFromLines(lines, "", startMs, endMs)
}

// SumUsageForProviderFromLines sums assistant turn totals for one backend in
// file order (empty provider = all backends). Same burn semantics as
// SumUsageFromLines: no tree filtering and no aborted/error skip; only
// totalTokens-or-component aggregation differs from the pre-tree extractor.
func SumUsageForProviderFromLines(lines []string, provider string, startMs, endMs int64) (tokens float64, costUSD float64) {
	provider = strings.TrimSpace(provider)
	windowed := startMs > 0 || endMs > 0
	for _, line := range lines {
		parsed := parseAssistantLine(line)
		if parsed == nil || parsed.Message == nil || parsed.Message.Usage == nil {
			continue
		}
		msg := parsed.Message
		if provider != "" && strings.TrimSpace(msg.Provider) != provider {
			continue
		}
		if windowed {
			ts := msg.Timestamp
			if ts <= 0 {
				continue
			}
			if startMs > 0 && ts < startMs {
				continue
			}
			if endMs > 0 && ts > endMs {
				continue
			}
		}
		tokens += float64(totalTokensOf(msg.Usage))
		if msg.Usage.Cost != nil {
			costUSD += floatOrZero(msg.Usage.Cost.Total)
		}
	}
	return tokens, costUSD
}

type cacheAccum struct {
	fresh, read, write int
	ttlSeconds         *int64
	lastActivity       *int64
	anthropicActive    bool
}

func (a *cacheAccum) add(msg *assistantMessage) {
	if msg == nil || msg.Usage == nil {
		return
	}
	// OMP records uncached input separately from cache reads and writes; keep
	// all three so the shared reuse ratio has the same denominator semantics.
	fresh := intOrZero(msg.Usage.Input)
	cacheRead := intOrZero(msg.Usage.CacheRead)
	cacheWrite := intOrZero(msg.Usage.CacheWrite)
	cacheWrite1h := intOrZero(msg.Usage.CacheWrite1h)
	a.fresh += fresh
	a.read += cacheRead
	a.write += cacheWrite
	if !strings.EqualFold(msg.Provider, "anthropic") || (cacheRead == 0 && cacheWrite == 0) {
		return
	}
	a.anthropicActive = true
	activity := unixSecondsFromMs(msg.Timestamp)
	if activity <= 0 {
		return
	}
	if a.lastActivity == nil {
		a.lastActivity = &activity
		ttl := int64(5 * 60)
		if cacheWrite1h > 0 {
			ttl = 60 * 60
		}
		a.ttlSeconds = &ttl
		return
	}
	if cacheWrite1h > 0 && a.ttlSeconds != nil && *a.ttlSeconds < 3600 {
		ttl := int64(60 * 60)
		a.ttlSeconds = &ttl
	}
}

func (a cacheAccum) apply(usage *SessionUsage) *SessionUsage {
	if usage == nil {
		return nil
	}
	cache := core.CacheFromTokenCounts(a.fresh, a.read, a.write)
	if cache == nil {
		return usage
	}
	if a.anthropicActive && strings.EqualFold(usage.Provider, "anthropic") {
		cache.TTLSeconds = a.ttlSeconds
		cache.LastActivityUnix = a.lastActivity
	}
	usage.Cache = cache
	return usage
}

func unixSecondsFromMs(ts int64) int64 {
	if ts <= 0 {
		return 0
	}
	if ts > 1_000_000_000_000 {
		return ts / 1000
	}
	return ts
}
