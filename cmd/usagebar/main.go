// Command usagebar is the Herdr Agent Usage plugin binary.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
	"github.com/senna-lang/herdr-agent-usage/internal/setup"
	"github.com/senna-lang/herdr-agent-usage/internal/update"
	"golang.org/x/term"
)

// version is overridden at release time via -ldflags "-X main.version=vX.Y.Z".
var version = "0.1.0-dev"

func main() {
	limits.SetShowNotification(herdrcli.ShowNotification)

	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "version", "--version", "-V":
		fmt.Printf("usagebar %s\n", version)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	case "status", "update":
		// force when invoked as a plugin action (refresh)
		force := os.Getenv("HERDR_PLUGIN_ACTION_ID") != "" || hasFlag(args, "--force")
		update.RunUpdate(force)
	case "setup":
		writeToast := hasFlag(args, "--write-toast") || hasFlag(args, "--apply-toast")
		report := setup.RunSetup(setup.SetupOptions{WriteToast: writeToast})
		fmt.Print(strings.Join(report.Lines, "\n") + "\n")
	case "limits", "panel":
		if err := runLimitsPane(args); err != nil {
			fmt.Fprintf(os.Stderr, "usagebar limits: %v\n", err)
			os.Exit(1)
		}
	case "notify":
		runNotify()
	case "statusline":
		// Claude Code statusLine bridge (stdin JSON → cache + toasts + summary stdout)
		runStatusLine()
	case "collect":
		// debug: print JSON of collected limits once
		runCollectJSON(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `usagebar — Herdr Agent Usage (Go)

Usage:
  usagebar status|update [--force]   Update sidebar custom_status for HERDR_PANE_ID
  usagebar limits|panel              Interactive limits panel (q quit, r refresh)
                                     Shows providers with an open agent pane;
                                     --all shows every provider
  usagebar limits --once [--all]     Print panel once to stdout
  usagebar notify                    Check non-Claude primary rate-limit toasts
  usagebar statusline                Claude Code statusLine (stdin rate_limits)
  usagebar setup [--write-toast]     Seed plugin config / show snippets
  usagebar collect                   Debug: print collected limits as JSON
  usagebar version

`)
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func resolveCwd() *string {
	if ctxRaw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); ctxRaw != "" {
		var ctx struct {
			FocusedPaneCwd *string `json:"focused_pane_cwd"`
			WorkspaceCwd   *string `json:"workspace_cwd"`
		}
		if err := json.Unmarshal([]byte(ctxRaw), &ctx); err == nil {
			if ctx.FocusedPaneCwd != nil && *ctx.FocusedPaneCwd != "" {
				return ctx.FocusedPaneCwd
			}
			if ctx.WorkspaceCwd != nil && *ctx.WorkspaceCwd != "" {
				return ctx.WorkspaceCwd
			}
		}
	}
	if paneID := os.Getenv("HERDR_PANE_ID"); paneID != "" {
		pane := herdrcli.GetPaneInfo(paneID)
		if pane.ForegroundCwd != nil {
			return pane.ForegroundCwd
		}
		if pane.Cwd != nil {
			return pane.Cwd
		}
	}
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return nil
	}
	return &cwd
}

// openPaneSnapshots lists open agent panes; ok=false means the herdr pane
// query failed (unknown state), as opposed to a confirmed empty pane list.
func openPaneSnapshots() ([]limits.OpenPaneSnapshot, bool) {
	open, ok := herdrcli.ListOpenAgentPanesOK()
	snaps := make([]limits.OpenPaneSnapshot, 0, len(open))
	for _, p := range open {
		agent := ""
		if p.Agent != nil {
			agent = *p.Agent
		}
		label := agent
		if p.RowLabel != nil {
			label = *p.RowLabel
		}
		var sid *string
		if p.AgentSession != nil {
			sid = &p.AgentSession.Value
		}
		cwd := p.ForegroundCwd
		if cwd == nil {
			cwd = p.Cwd
		}
		snaps = append(snaps, limits.OpenPaneSnapshot{
			PaneID: p.PaneID, Agent: agent, Label: label,
			SessionID: sid, Cwd: cwd,
		})
	}
	return snaps, ok
}

// collectProviders gathers provider limits. activeOnly hides providers that
// have no open agent pane in Herdr (the limits pane default; --all overrides).
// When the pane query fails, all providers are shown (fail-open).
func collectProviders(nowMs int64, activeOnly bool) []limits.ProviderLimits {
	snaps, panesOK := openPaneSnapshots()
	opts := limits.DefaultCollectOptions()
	if activeOnly {
		opts.Only = limits.ActiveProviderFilter(snaps, panesOK)
	}
	opts.Attach = func(providers []limits.ProviderLimits, now int64) []limits.ProviderLimits {
		return limits.CollectAndAttachPaneActivity(providers, snaps, now)
	}
	base := limits.CollectAllProviderLimits(resolveCwd(), nowMs, opts)
	hist := limits.LoadUsageHistory()
	res := limits.EnrichRunOut(base, hist, nowMs, limits.DefaultRunOutOptions)
	limits.SaveUsageHistory(res.History)
	return res.Providers
}

func currentLayout() limits.PanelLayout {
	cols, rows := 44, 24
	// Prefer live TTY size (Herdr pane size), then COLUMNS/LINES.
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		if w > 0 {
			cols = w
		}
		if h > 0 {
			rows = h
		}
	} else {
		if v := os.Getenv("COLUMNS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cols = n
			}
		}
		if v := os.Getenv("LINES"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				rows = n
			}
		}
	}
	color := term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""
	return limits.PanelLayout{Columns: cols, Rows: rows, Color: color}
}

// paintFrame draws text on the alternate screen.
// After term.MakeRaw, \n alone is LF without CR (staircase wrap). Always use \r\n.
func paintFrame(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")
	_, _ = os.Stdout.WriteString("\x1b[H\x1b[2J\x1b[3J" + text + "\x1b[J\x1b[H")
}

func runLimitsPane(args []string) error {
	once := hasFlag(args, "--once")
	// Default: show only providers with an open agent pane; --all shows every provider.
	activeOnly := !hasFlag(args, "--all")
	layoutFor := func() limits.PanelLayout {
		layout := currentLayout()
		if activeOnly {
			layout.EmptyMessage = "(no agent panes open)"
		}
		return layout
	}
	if once || !term.IsTerminal(int(os.Stdout.Fd())) {
		nowMs := time.Now().UnixMilli()
		providers := collectProviders(nowMs, activeOnly)
		text := limits.FormatLimitsPanel(providers, nowMs, layoutFor())
		fmt.Print(text)
		if !strings.HasSuffix(text, "\n") {
			fmt.Println()
		}
		return nil
	}

	// Interactive alternate screen
	enter := "\x1b[?1049h\x1b[?25l"
	leave := "\x1b[?25h\x1b[?1049l"
	_, _ = os.Stdout.WriteString(enter)
	defer func() { _, _ = os.Stdout.WriteString(leave) }()

	paintFrame("loading…\n")

	// Cache last snapshot so resize can re-layout instantly without re-collecting.
	var (
		cacheMu         sync.Mutex
		cachedProviders []limits.ProviderLimits
		cachedNowMs     int64
	)

	paintCached := func() {
		cacheMu.Lock()
		providers := cachedProviders
		nowMs := cachedNowMs
		cacheMu.Unlock()
		if providers == nil {
			return
		}
		paintFrame(limits.FormatLimitsPanel(providers, nowMs, layoutFor()))
	}

	renderFull := func() {
		nowMs := time.Now().UnixMilli()
		providers := collectProviders(nowMs, activeOnly)
		cacheMu.Lock()
		cachedProviders = providers
		cachedNowMs = nowMs
		cacheMu.Unlock()
		paintFrame(limits.FormatLimitsPanel(providers, nowMs, layoutFor()))
	}
	renderFull()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// SIGWINCH: instant layout-only repaint (debounced full refresh after drag ends).
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	resizeFull := make(chan struct{}, 1)
	go func() {
		var debounce *time.Timer
		for range winch {
			// Immediate re-layout with cached data (snappy while dragging).
			paintCached()
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(120*time.Millisecond, func() {
				select {
				case resizeFull <- struct{}{}:
				default:
				}
			})
		}
	}()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		// No raw keys: keep auto-refreshing / resize-handling until killed.
		for {
			select {
			case <-ticker.C:
				renderFull()
			case <-resizeFull:
				renderFull()
			}
		}
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// stdin read in goroutine
	keys := make(chan byte, 8)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				keys <- buf[0]
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ticker.C:
			renderFull()
		case <-resizeFull:
			// After resize settles, refresh data once (still fast enough).
			renderFull()
		case ch := <-keys:
			switch ch {
			case 'q', 'Q', 3: // ctrl-c
				return nil
			case 'r', 'R':
				paintFrame("refreshing…\n")
				renderFull()
			}
		}
	}
}

func runNotify() {
	nowMs := time.Now().UnixMilli()
	providers := limits.CollectAllProviderLimits(resolveCwd(), nowMs, limits.DefaultCollectOptions())
	limits.NotifyProviderPrimaryLimits(providers, nowMs)
}

func runStatusLine() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[usagebar-rate] read stdin: %v\n", err)
		os.Exit(1)
	}
	stdinJSON := string(data)
	nowMs := time.Now().UnixMilli()
	if rateLimits := ratelimit.ParseRateLimits(stdinJSON); rateLimits != nil {
		// Persist rate_limits so the limits pane can show Claude windows.
		input := limits.RateLimitsInput{}
		if rateLimits.FiveHour != nil {
			input.FiveHour = &struct {
				UsedPercentage float64
				ResetsAt       int64
			}{rateLimits.FiveHour.UsedPercentage, rateLimits.FiveHour.ResetsAt}
		}
		if rateLimits.SevenDay != nil {
			input.SevenDay = &struct {
				UsedPercentage float64
				ResetsAt       int64
			}{rateLimits.SevenDay.UsedPercentage, rateLimits.SevenDay.ResetsAt}
		}
		if err := limits.WriteClaudeLimitsCache(input, nowMs, ""); err != nil {
			fmt.Fprintf(os.Stderr, "[usagebar-rate] cache write failed: %v\n", err)
		}
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[usagebar-rate] check failed: %v\n", r)
			}
		}()
		ratelimit.RunRateLimitCheck(stdinJSON, nowMs, herdrcli.ShowNotification)
	}()
	summary := ratelimit.FormatStatusLineSummary(ratelimit.ParseRateLimits(stdinJSON))
	if summary != "" {
		fmt.Println(summary)
	}
}

func runCollectJSON(args []string) {
	nowMs := time.Now().UnixMilli()
	providers := collectProviders(nowMs, !hasFlag(args, "--all"))
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(providers)
}
