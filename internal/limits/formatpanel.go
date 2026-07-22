/**
 * Renders a list of ProviderLimits as text for a plugin pane.
 *
 * The remaining amount is shown as a bar gauge. Herdr's plugin pane clips
 * the top lines when the viewport hugs the bottom, so we render in three
 * tiers based on the pane's rows budget:
 *   1. rich      : header + 2 bars + extras (pane/note)
 *   2. rich-slim : header + 2 bars (no extras)
 *   3. compact   : 1 line per provider (inline mini bar)
 */
package limits

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/senna-lang/herdr-agent-usage/internal/bar"
)

// PanelLayout gathers external layout dependencies (color/width/height).
type PanelLayout struct {
	Columns int
	Rows    int
	Color   bool
	// EmptyMessage replaces the default "(no usage data yet)" text when the
	// provider list is empty (e.g. "(no agent panes open)" for active-only mode).
	EmptyMessage string
}

var defaultLayout = PanelLayout{Columns: 44, Rows: 9999, Color: false}

const (
	ruleChar       = "─"
	minBar         = 8
	maxBar         = 22
	richMinColumns = 32
	gutter         = " "
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func remainingOf(used float64) int {
	return int(math.Max(0, math.Min(100, math.Round(100-used))))
}

func formatResetIn(ms int64) string {
	if ms <= 0 {
		return "soon"
	}
	totalMin := ms / 60_000
	days := totalMin / (60 * 24)
	hours := (totalMin % (60 * 24)) / 60
	mins := totalMin % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func windowTag(w *LimitWindow, fallback string) string {
	if w == nil || w.WindowMinutes == nil {
		return fallback
	}
	return minutesTag(*w.WindowMinutes)
}

func minutesTag(mins int) string {
	if mins <= 360 {
		return "5h"
	}
	if mins == 24*60 {
		return "24h"
	}
	if mins >= 9000 && mins < 20*24*60 {
		return "7d"
	}
	if mins >= 20*24*60 {
		return "30d"
	}
	return strconv.Itoa(mins) + "m"
}

func shareText(sharePercent float64) string {
	if sharePercent == math.Trunc(sharePercent) {
		return strconv.FormatFloat(sharePercent, 'f', 0, 64)
	}
	return strconv.FormatFloat(sharePercent, 'f', 1, 64)
}

func barWidth(columns int) int {
	reserved := 32
	w := columns - reserved
	if w < minBar {
		return minBar
	}
	if w > maxBar {
		return maxBar
	}
	return w
}

func plainWidth(s string) int {
	return utf8.RuneCountInString(ansiRe.ReplaceAllString(s, ""))
}

func ruleWidth(columns int) int {
	w := columns - 2
	if w < 16 {
		return 16
	}
	if w > 60 {
		return 60
	}
	return w
}

func padEnd(s string, n int) string {
	for utf8.RuneCountInString(s) < n {
		s += " "
	}
	return s
}

func windowLine(w *LimitWindow, tag string, layout PanelLayout, nowMs int64) string {
	tagCol := padEnd(tag, 3)
	if w == nil {
		track := bar.Dim(strings.Repeat("░", barWidth(layout.Columns)), layout.Color)
		return "  " + tagCol + "  " + track + "   " + bar.Dim("—", layout.Color)
	}
	rem := remainingOf(w.UsedPercentage)
	tone := bar.ToneForRemaining(float64(rem))
	barStr := bar.Colorize(bar.RenderBar(float64(rem), barWidth(layout.Columns)), tone, layout.Color)
	pct := bar.Colorize(fmt.Sprintf("%3d%% left", rem), tone, layout.Color)
	line := "  " + tagCol + "  " + barStr + "   " + pct
	if w.ResetsAt != nil && *w.ResetsAt > 0 {
		hint := formatResetIn(*w.ResetsAt*1000 - nowMs)
		tail := "    " + hint
		if plainWidth(line)+utf8.RuneCountInString(tail) <= layout.Columns-1 {
			line += "    " + bar.Dim(hint, layout.Color)
		}
	}
	return line
}

func runOutLine(w *LimitWindow, layout PanelLayout) string {
	if w == nil || w.RunOut == nil || !w.RunOut.EmptyBeforeReset {
		return ""
	}
	dur := formatShortDuration(w.RunOut.MinutesToEmpty)
	return "      " + bar.Colorize("⚠ empty in ~"+dur, bar.ToneLow, layout.Color)
}

func formatShortDuration(minutes float64) string {
	totalMin := int(math.Max(0, math.Round(minutes)))
	if totalMin < 1 {
		return "<1m"
	}
	days := totalMin / (60 * 24)
	hours := (totalMin % (60 * 24)) / 60
	mins := totalMin % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func inlineWindow(w *LimitWindow, tag string, layout PanelLayout) string {
	if w == nil {
		return tag + " " + bar.Dim("—", layout.Color)
	}
	rem := remainingOf(w.UsedPercentage)
	tone := bar.ToneForRemaining(float64(rem))
	barStr := bar.Colorize(bar.RenderBar(float64(rem), 5), tone, layout.Color)
	pct := bar.Colorize(fmt.Sprintf("%d%%", rem), tone, layout.Color)
	warn := ""
	if w.RunOut != nil && w.RunOut.EmptyBeforeReset {
		warn = bar.Colorize("⚠", bar.ToneLow, layout.Color)
	}
	return tag + " " + barStr + " " + pct + warn
}

func paneActivityLine(activity ProviderPaneActivity, layout PanelLayout) string {
	if len(activity.Panes) == 0 {
		return ""
	}
	tag := minutesTag(activity.WindowMinutes)
	labelText := tag + " share"
	prefix := 1 + 2 + len(labelText) + 2
	budget := int(math.Max(8, float64(layout.Columns)-float64(prefix)-3))
	var parts []string
	used := 0
	shown := 0
	for _, pane := range activity.Panes {
		piece := pane.Label + " " + shareText(pane.SharePercent) + "%"
		add := 0
		if len(parts) > 0 {
			add = 3
		}
		add += len(piece)
		if used+add > budget && shown > 0 {
			break
		}
		parts = append(parts, piece)
		used += add
		shown++
	}
	overflow := len(activity.Panes) - shown
	tail := ""
	if overflow > 0 {
		tail = fmt.Sprintf(" +%d", overflow)
	}
	label := bar.Dim(labelText, layout.Color)
	return "  " + label + "  " + strings.Join(parts, " · ") + tail
}

// noteLine renders p.Note (stale-cache warnings, account identity, spend
// summaries, …), truncated to fit the pane width. Empty when there is no
// note — callers only append it when non-empty.
func noteLine(note *string, layout PanelLayout) string {
	if note == nil || *note == "" {
		return ""
	}
	text := *note
	budget := max(layout.Columns-3, 8) // "  " prefix + 1 margin
	if utf8.RuneCountInString(text) > budget {
		runes := []rune(text)
		text = string(runes[:budget-1]) + "…"
	}
	return "  " + bar.Dim(text, layout.Color)
}

func providerHeader(p ProviderLimits, layout PanelLayout) string {
	name := bar.Bold(p.Label, layout.Color)
	if p.PlanType != nil {
		return name + " " + bar.Dim("·", layout.Color) + " " + *p.PlanType
	}
	return name
}

// apiWindowLine renders one rolling usage row. Tokens are the base metric —
// every harness reports them — and USD is an optional trailing column, since
// only OpenCode prices its messages: "  24h  332k     $0.02".
func apiWindowLine(w APIUsageWindow, withCost bool, layout PanelLayout) string {
	tagCol := padEnd(minutesTag(w.WindowMinutes), 3)
	tokens := formatCompactTokens(w.Tokens)
	if !withCost {
		return "  " + tagCol + "  " + bar.Dim(tokens, layout.Color)
	}
	return "  " + tagCol + "  " + bar.Dim(padEnd(tokens, 7), layout.Color) + "  " + formatCompactCost(w.CostUSD)
}

// apiModelsLine renders the per-model token breakdown, dropping models that
// do not fit and reporting the remainder as "+N" — same budgeting as the
// share row. Tokens rather than USD: the per-model amounts are small enough
// that cost rounding collapses distinct models to the same "$0.02", and the
// window rows above already carry the spend.
func apiModelsLine(models []APIModelUsage, layout PanelLayout) string {
	if len(models) == 0 {
		return ""
	}
	labelText := "models"
	prefix := 1 + 2 + len(labelText) + 2
	budget := int(math.Max(8, float64(layout.Columns)-float64(prefix)-3))
	var parts []string
	used, shown := 0, 0
	for _, m := range models {
		piece := m.ModelID + " " + formatCompactTokens(m.Tokens)
		// Measure display width, not bytes: model ids are ASCII today but the
		// byte length would silently truncate anything wider.
		add := plainWidth(piece)
		if len(parts) > 0 {
			add += 3
		}
		if used+add > budget && shown > 0 {
			break
		}
		parts = append(parts, piece)
		used += add
		shown++
	}
	tail := ""
	if overflow := len(models) - shown; overflow > 0 {
		tail = fmt.Sprintf(" +%d", overflow)
	}
	return "  " + bar.Dim(labelText, layout.Color) + "  " + strings.Join(parts, " · ") + tail
}

// apiProviderHeader renders "DeepSeek · API".
func apiProviderHeader(p APIProviderUsage, layout PanelLayout) string {
	return bar.Bold(p.Label, layout.Color) + " " + bar.Dim("·", layout.Color) + " API"
}

// apiRichBlock is the full pay-as-you-go block: header, rolling windows,
// model breakdown, and pane share.
func apiRichBlock(p APIProviderUsage, layout PanelLayout, withExtras bool) []string {
	lines := []string{apiProviderHeader(p, layout)}
	for _, w := range p.Windows {
		lines = append(lines, apiWindowLine(w, p.HasCost, layout))
	}
	if !withExtras {
		return lines
	}
	if modelsLine := apiModelsLine(p.Models, layout); modelsLine != "" {
		lines = append(lines, modelsLine)
	}
	if p.PaneActivity != nil {
		if paneLine := paneActivityLine(*p.PaneActivity, layout); paneLine != "" {
			lines = append(lines, paneLine)
		}
	}
	return lines
}

// apiCompactLine collapses a block to one line for the tightest tier,
// keeping tokens as the metric so every backend reads the same way.
func apiCompactLine(p APIProviderUsage, layout PanelLayout) string {
	line := bar.Bold(p.Label, layout.Color)
	width := plainWidth(line) + 1
	for _, w := range p.Windows {
		seg := "  " + minutesTag(w.WindowMinutes) + " " + formatCompactTokens(w.Tokens)
		if width+plainWidth(seg) > layout.Columns {
			break
		}
		line += seg
		width += plainWidth(seg)
	}
	return line
}

func richBlock(p ProviderLimits, layout PanelLayout, withExtras bool, nowMs int64) []string {
	lines := []string{providerHeader(p, layout)}
	primaryTag := windowTag(p.Primary, "5h")
	secondaryTag := windowTag(p.Secondary, "7d")
	tertiaryTag := windowTag(p.Tertiary, "30d")

	pushWindow := func(w *LimitWindow, tag string) {
		lines = append(lines, windowLine(w, tag, layout, nowMs))
		if warn := runOutLine(w, layout); warn != "" {
			lines = append(lines, warn)
		}
	}

	hasAny := p.Primary != nil || p.Secondary != nil || p.Tertiary != nil
	if !hasAny {
		pushWindow(nil, primaryTag)
		pushWindow(nil, secondaryTag)
	} else {
		if p.Primary != nil {
			pushWindow(p.Primary, primaryTag)
		}
		if p.Secondary != nil {
			pushWindow(p.Secondary, secondaryTag)
		}
		if p.Tertiary != nil {
			pushWindow(p.Tertiary, tertiaryTag)
		}
	}
	if withExtras && p.PaneActivity != nil {
		if paneLine := paneActivityLine(*p.PaneActivity, layout); paneLine != "" {
			lines = append(lines, paneLine)
		}
	}
	if withExtras {
		if note := noteLine(p.Note, layout); note != "" {
			lines = append(lines, note)
		}
	}
	return lines
}

func compactLine(p ProviderLimits, layout PanelLayout) string {
	name := bar.Bold(p.Label, layout.Color)
	var windows []string
	if p.Primary != nil {
		windows = append(windows, inlineWindow(p.Primary, windowTag(p.Primary, "5h"), layout))
	}
	if p.Secondary != nil {
		windows = append(windows, inlineWindow(p.Secondary, windowTag(p.Secondary, "7d"), layout))
	}
	if p.Tertiary != nil {
		windows = append(windows, inlineWindow(p.Tertiary, windowTag(p.Tertiary, "30d"), layout))
	}

	line := name
	width := plainWidth(name) + 1
	for _, win := range windows {
		seg := "  " + win
		if width+plainWidth(seg) > layout.Columns {
			break
		}
		line += seg
		width += plainWidth(seg)
	}
	return line
}

// FormatProviderBlock renders one provider (default = rich, with extras).
func FormatProviderBlock(p ProviderLimits, layout PanelLayout, nowMs int64) string {
	if layout.Columns == 0 {
		layout = defaultLayout
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}
	return strings.Join(richBlock(p, layout, true, nowMs), "\n")
}

// FormatLimitsPanel renders subscription providers only. Prefer
// FormatUsagePanel when pay-as-you-go blocks may be present.
func FormatLimitsPanel(providers []ProviderLimits, nowMs int64, layout PanelLayout) string {
	return FormatUsagePanel(providers, nil, nowMs, layout)
}

// FormatUsagePanel renders the full panel — subscription blocks first, then
// pay-as-you-go spend blocks — stepping down rich -> rich-slim -> compact.
// Both kinds share one row budget so a short pane degrades uniformly.
func FormatUsagePanel(providers []ProviderLimits, apiUsage []APIProviderUsage, nowMs int64, layout PanelLayout) string {
	if layout.Columns == 0 && layout.Rows == 0 {
		layout = defaultLayout
	}
	if layout.Rows == 0 {
		layout.Rows = defaultLayout.Rows
	}
	if layout.Columns == 0 {
		layout.Columns = defaultLayout.Columns
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}

	timeStr := time.UnixMilli(nowMs).Local().Format("15:04")
	rule := strings.Repeat(ruleChar, ruleWidth(layout.Columns))
	footerText := fittingFooter(timeStr, layout.Columns-1)
	footer := bar.Dim(footerText, layout.Color)

	if len(providers) == 0 && len(apiUsage) == 0 {
		empty := layout.EmptyMessage
		if empty == "" {
			empty = "(no usage data yet)"
		}
		return strings.Join(indent([]string{"", empty, "", rule, footer, ""}), "\n")
	}

	blocks := make([]panelBlock, 0, len(providers)+len(apiUsage))
	blocks = append(blocks, buildProviderBlocks(providers, layout, nowMs)...)
	for _, p := range apiUsage {
		blocks = append(blocks, apiBlock(p, layout))
	}

	chrome := 5
	bodyBudget := int(math.Max(1, float64(layout.Rows)-float64(chrome)))
	body := renderBody(blocks, layout, bodyBudget)
	return strings.Join(indent([]string{"", body, "", rule, footer, ""}), "\n")
}

func fittingFooter(timeStr string, width int) string {
	variants := []string{
		"Updated " + timeStr + "  ·  q quit  ·  r refresh",
		timeStr + "  ·  q quit  ·  r refresh",
		timeStr + " · q · r",
		"q · r",
	}
	for _, v := range variants {
		if utf8.RuneCountInString(v) <= width {
			return v
		}
	}
	return variants[len(variants)-1]
}

func indent(lines []string) []string {
	var out []string
	for _, block := range lines {
		for _, line := range strings.Split(block, "\n") {
			if len(line) > 0 {
				out = append(out, gutter+line)
			} else {
				out = append(out, line)
			}
		}
	}
	return out
}

// panelBlock renders one provider entry at a given detail tier, so
// subscription and pay-as-you-go blocks share the same row budget.
type panelBlock struct {
	rich     func() []string
	richSlim func() []string
	compact  func() []string
}

func subscriptionBlock(p ProviderLimits, layout PanelLayout, nowMs int64) panelBlock {
	return panelBlock{
		rich:     func() []string { return richBlock(p, layout, true, nowMs) },
		richSlim: func() []string { return richBlock(p, layout, false, nowMs) },
		compact:  func() []string { return []string{compactLine(p, layout)} },
	}
}

// buildProviderBlocks turns each contiguous run of 2+ providers sharing the
// same non-empty GroupLabel (e.g. multiple configured Claude accounts) into
// one nested groupedSubscriptionBlock; every other provider keeps its normal
// standalone subscriptionBlock. Groups are expected to be contiguous, which
// holds for how CollectAllProviderLimits orders configured Claude profiles.
func buildProviderBlocks(providers []ProviderLimits, layout PanelLayout, nowMs int64) []panelBlock {
	blocks := make([]panelBlock, 0, len(providers))
	for i := 0; i < len(providers); {
		label := providers[i].GroupLabel
		j := i + 1
		if label != "" {
			for j < len(providers) && providers[j].GroupLabel == label {
				j++
			}
		}
		if label != "" && j-i >= 2 {
			blocks = append(blocks, groupedSubscriptionBlock(label, providers[i:j], layout, nowMs))
		} else {
			blocks = append(blocks, subscriptionBlock(providers[i], layout, nowMs))
			j = i + 1
		}
		i = j
	}
	return blocks
}

// groupedSubscriptionBlock renders a shared heading (e.g. "Claude") followed
// by each member indented two spaces, using AccountLabel in place of Label so
// the members stay distinguishable under the one heading.
func groupedSubscriptionBlock(label string, members []ProviderLimits, layout PanelLayout, nowMs int64) panelBlock {
	inner := layout
	inner.Columns = max(layout.Columns-2, richMinColumns)

	memberWithAccountLabel := func(m ProviderLimits) ProviderLimits {
		if m.AccountLabel != "" {
			m.Label = m.AccountLabel
		}
		return m
	}

	render := func(withExtras bool) []string {
		lines := []string{bar.Bold(label, layout.Color)}
		for i, m := range members {
			if i > 0 {
				lines = append(lines, "")
			}
			sub := richBlock(memberWithAccountLabel(m), inner, withExtras, nowMs)
			lines = append(lines, indentLines(sub, "  ")...)
		}
		return lines
	}

	return panelBlock{
		rich:     func() []string { return render(true) },
		richSlim: func() []string { return render(false) },
		compact: func() []string {
			lines := []string{bar.Bold(label, layout.Color)}
			for _, m := range members {
				lines = append(lines, "  "+compactLine(memberWithAccountLabel(m), inner))
			}
			return lines
		},
	}
}

func indentLines(lines []string, prefix string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			out[i] = l
			continue
		}
		out[i] = prefix + l
	}
	return out
}

func apiBlock(p APIProviderUsage, layout PanelLayout) panelBlock {
	return panelBlock{
		rich:     func() []string { return apiRichBlock(p, layout, true) },
		richSlim: func() []string { return apiRichBlock(p, layout, false) },
		compact:  func() []string { return []string{apiCompactLine(p, layout)} },
	}
}

func renderBody(blocks []panelBlock, layout PanelLayout, bodyBudget int) string {
	type tier func(panelBlock) []string
	var tiers []tier
	if layout.Columns >= richMinColumns {
		tiers = append(tiers,
			func(b panelBlock) []string { return b.rich() },
			func(b panelBlock) []string { return b.richSlim() },
		)
	}
	tiers = append(tiers, func(b panelBlock) []string { return b.compact() })

	for ti, render := range tiers {
		rendered := make([][]string, len(blocks))
		allSingle := true
		for i, b := range blocks {
			rendered[i] = render(b)
			if len(rendered[i]) != 1 {
				allSingle = false
			}
		}
		spacer := 1
		if allSingle {
			spacer = 0
		}
		total := 0
		for _, r := range rendered {
			total += len(r)
		}
		total += int(math.Max(0, float64(len(rendered)-1))) * spacer
		last := ti == len(tiers)-1
		if total <= bodyBudget || last {
			parts := make([]string, len(rendered))
			for i, r := range rendered {
				parts[i] = strings.Join(r, "\n")
			}
			if spacer == 1 {
				return strings.Join(parts, "\n\n")
			}
			return strings.Join(parts, "\n")
		}
	}
	parts := make([]string, len(blocks))
	for i, b := range blocks {
		parts[i] = strings.Join(b.compact(), "\n")
	}
	return strings.Join(parts, "\n")
}
