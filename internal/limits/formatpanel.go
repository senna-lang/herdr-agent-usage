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
	w := int(math.Floor(float64(columns))) - reserved
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
	w := int(math.Floor(float64(columns))) - 2
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
	budget := int(math.Max(8, math.Floor(float64(layout.Columns))-float64(prefix)-3))
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

func providerHeader(p ProviderLimits, layout PanelLayout) string {
	name := bar.Bold(p.Label, layout.Color)
	if p.PlanType != nil {
		return name + " " + bar.Dim("·", layout.Color) + " " + *p.PlanType
	}
	return name
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

// FormatLimitsPanel renders the full panel, stepping down rich -> rich-slim -> compact.
func FormatLimitsPanel(providers []ProviderLimits, nowMs int64, layout PanelLayout) string {
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

	if len(providers) == 0 {
		return strings.Join(indent([]string{"", "(no usage data yet)", "", rule, footer, ""}), "\n")
	}

	chrome := 5
	bodyBudget := int(math.Max(1, math.Floor(float64(layout.Rows))-float64(chrome)))
	body := renderBody(providers, layout, bodyBudget, nowMs)
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

func renderBody(providers []ProviderLimits, layout PanelLayout, bodyBudget int, nowMs int64) string {
	type renderer func(ProviderLimits) []string
	var tiers []renderer
	if layout.Columns >= richMinColumns {
		tiers = append(tiers,
			func(p ProviderLimits) []string { return richBlock(p, layout, true, nowMs) },
			func(p ProviderLimits) []string { return richBlock(p, layout, false, nowMs) },
		)
	}
	tiers = append(tiers, func(p ProviderLimits) []string { return []string{compactLine(p, layout)} })

	for ti, render := range tiers {
		blocks := make([][]string, len(providers))
		allSingle := true
		for i, p := range providers {
			blocks[i] = render(p)
			if len(blocks[i]) != 1 {
				allSingle = false
			}
		}
		spacer := 1
		if allSingle {
			spacer = 0
		}
		total := 0
		for _, b := range blocks {
			total += len(b)
		}
		total += int(math.Max(0, float64(len(blocks)-1))) * spacer
		last := ti == len(tiers)-1
		if total <= bodyBudget || last {
			parts := make([]string, len(blocks))
			for i, b := range blocks {
				parts[i] = strings.Join(b, "\n")
			}
			if spacer == 1 {
				return strings.Join(parts, "\n\n")
			}
			return strings.Join(parts, "\n")
		}
	}
	parts := make([]string, len(providers))
	for i, p := range providers {
		parts[i] = compactLine(p, layout)
	}
	return strings.Join(parts, "\n")
}
