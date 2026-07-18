/**
 * Detects drift between our hardcoded Claude context-window map
 * (internal/claude/modelwindows.go) and LiteLLM's community-maintained
 * model_prices_and_context_window.json, which tracks new Anthropic models and
 * window changes day-0.
 *
 * The check drives the real resolver (claude.ContextWindowFor) rather than
 * comparing map keys directly, so it reuses the longest-match + [1m]
 * normalization logic and naturally dedups date-suffixed variants.
 *
 * Exit codes (consumed by .github/workflows/model-drift.yml):
 *   0  no drift — every tracked Anthropic model matches upstream
 *   1  the checker itself failed (fetch/parse error) — NOT a drift signal
 *   2  drift found — uncovered models and/or window mismatches
 */
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
)

const defaultLiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// ignoredModels are LiteLLM Anthropic entries we deliberately do not track:
// pre-4.5 models that Claude Code paid plans no longer default to. A new model
// NOT in this set that resolves to nil is reported as "possibly new" so a human
// decides whether to add it here or to modelwindows.go.
var ignoredModels = map[string]bool{
	"claude-3-7-sonnet-20250219": true,
	"claude-3-haiku-20240307":    true,
	"claude-3-opus-20240229":     true,
	"claude-4-opus-20250514":     true,
	"claude-4-sonnet-20250514":   true,
	"claude-opus-4-20250514":     true,
}

// intentionalOverrides are models where our domain (the window Claude Code
// effectively uses on paid plans) intentionally differs from LiteLLM's
// non-beta API default. Matched as a substring so bare and date-suffixed
// variants are both covered.
//
// UNCONFIRMED: claude-sonnet-4-5 — repo uses 1M, LiteLLM's default is 200k.
// Pending confirmation that Claude Code gives Sonnet 4.5 a 1M window on the
// target plans (via the context-1m beta header). If it does not, this is a
// live bug in modelwindows.go, not an override — remove this entry and fix
// the map.
var intentionalOverrides = []string{
	"claude-sonnet-4-5",
}

type modelEntry struct {
	Provider       string  `json:"litellm_provider"`
	MaxInputTokens flexInt `json:"max_input_tokens"`
}

// flexInt accepts the several shapes LiteLLM uses for token counts across its
// 1000+ entries: a JSON number (200000), a float (200000.0), or a stringified
// number ("2000000.0"). Unparseable values leave it unset so the entry is
// skipped rather than crashing the whole parse.
type flexInt struct {
	set bool
	val int
}

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	f.val, f.set = int(n), true
	return nil
}

func main() {
	url := os.Getenv("LITELLM_URL")
	if url == "" {
		url = defaultLiteLLMURL
	}

	entries, err := fetch(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "model-drift: checker failed: %v\n", err)
		os.Exit(1)
	}

	var mismatches, uncovered []string
	for _, name := range sortedKeys(entries) {
		e := entries[name]
		if e.Provider != "anthropic" || !e.MaxInputTokens.set {
			continue
		}
		if ignoredModels[name] || overridden(name) {
			continue
		}
		upstream := e.MaxInputTokens.val
		got := claude.ContextWindowFor(name)
		switch {
		case got == nil:
			uncovered = append(uncovered, fmt.Sprintf("- `%s` — LiteLLM context window %d, not tracked by `modelwindows.go`", name, upstream))
		case *got != upstream:
			mismatches = append(mismatches, fmt.Sprintf("- `%s` — repo says %d, LiteLLM says %d", name, *got, upstream))
		}
	}

	if len(mismatches) == 0 && len(uncovered) == 0 {
		fmt.Println("No model-window drift: all tracked Anthropic models match LiteLLM.")
		return
	}

	fmt.Print(report(mismatches, uncovered))
	os.Exit(2)
}

func fetch(url string) (map[string]modelEntry, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse LiteLLM JSON: %w", err)
	}
	// Decode each entry independently: LiteLLM ships a documentation stub
	// (sample_spec) and occasional oddly-typed sibling entries. Skipping a
	// malformed entry must not fail the whole check.
	entries := make(map[string]modelEntry, len(raw))
	for name, msg := range raw {
		var e modelEntry
		if err := json.Unmarshal(msg, &e); err != nil {
			continue
		}
		entries[name] = e
	}
	return entries, nil
}

func overridden(name string) bool {
	for _, o := range intentionalOverrides {
		if strings.Contains(name, o) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]modelEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func report(mismatches, uncovered []string) string {
	var b strings.Builder
	b.WriteString("## Claude model-window drift detected\n\n")
	b.WriteString("`scripts/modeldrift` compared `internal/claude/modelwindows.go` against ")
	b.WriteString("[LiteLLM's model map](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json) and found differences.\n\n")

	if len(mismatches) > 0 {
		b.WriteString("### Context-window mismatches\n\n")
		b.WriteString("The tracked value disagrees with LiteLLM. Either update the map, or (if Claude Code intentionally uses a different window on paid plans) add the model to `intentionalOverrides` in `scripts/modeldrift/main.go`.\n\n")
		b.WriteString(strings.Join(mismatches, "\n"))
		b.WriteString("\n\n")
	}
	if len(uncovered) > 0 {
		b.WriteString("### Untracked models\n\n")
		b.WriteString("LiteLLM knows these Anthropic models; `modelwindows.go` does not. Add them to the map, or to `ignoredModels` in `scripts/modeldrift/main.go` if they are irrelevant to Claude Code paid plans.\n\n")
		b.WriteString(strings.Join(uncovered, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString("---\n")
	b.WriteString("_Filed automatically by the weekly model-drift workflow. This issue updates in place until the drift is resolved._\n")
	return b.String()
}
