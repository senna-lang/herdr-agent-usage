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
	"syscall"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
	"github.com/senna-lang/herdr-agent-usage/internal/setup"
	"github.com/senna-lang/herdr-agent-usage/internal/update"
	"github.com/senna-lang/herdr-agent-usage/internal/updatecheck"
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
	case "check-update":
		runUpdateCheck(args)
	case "statusline":
		// Claude Code statusLine bridge (stdin JSON → cache + toasts + summary stdout)
		runStatusLine()
	case "collect":
		// debug: print JSON of collected limits once
		runCollectJSON(args)
	case "opencode-check":
		// debug: report each stage of the OpenCode Go usage path
		runOpenCodeCheck()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `usagebar — Herdr Agent Usage (Go)

Usage:
  usagebar status|update [--force]   Update sidebar metadata tokens for HERDR_PANE_ID
  usagebar limits|panel              Interactive limits panel (q quit, r refresh)
                                     Shows providers with an open agent pane;
                                     --all shows every provider
  usagebar limits --once [--all]     Print panel once to stdout
  usagebar notify                    Check non-Claude primary rate-limit toasts
  usagebar check-update --current-version X.Y.Z [--force] [--quiet]
                                     Check GitHub Releases for a newer plugin version
  usagebar statusline                Claude Code statusLine (stdin rate_limits)
  usagebar setup [--write-toast]     Seed plugin config / show snippets
  usagebar collect                   Debug: print collected limits as JSON
  usagebar opencode-check            Debug: report the OpenCode Go usage path
                                     (browser session import → opencode.ai fetch)
  usagebar version

`)
}

func runUpdateCheck(args []string) {
	quiet := hasFlag(args, "--quiet")
	currentVersion := flagValue(args, "--current-version")
	if currentVersion == "" {
		currentVersion = version
	}
	result := updatecheck.Run(updatecheck.Options{
		CurrentVersion: currentVersion,
		StateDir:       setup.ResolvePluginConfigDir(environment()),
		Force:          hasFlag(args, "--force"),
		Notify:         updateNotification(environment()),
	})
	if quiet {
		return
	}
	if result.Err != nil {
		fmt.Fprintf(os.Stderr, "Agent Usage: could not check for updates: %v\n", result.Err)
		return
	}
	if !result.Checked {
		fmt.Println("Agent Usage: update check is not due yet.")
		return
	}
	if !result.Update {
		fmt.Printf("Agent Usage %s is up to date.\n", result.Current)
		return
	}
	fmt.Printf("Agent Usage update available: %s (installed %s)\n", result.Latest, result.Current)
	fmt.Printf("Release and update instructions: %s\n", result.ReleaseURL)
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func flagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func environment() map[string]string {
	env := map[string]string{}
	for _, entry := range os.Environ() {
		if i := strings.IndexByte(entry, '='); i >= 0 {
			env[entry[:i]] = entry[i+1:]
		}
	}
	return env
}

// notificationsEnabled returns the plugin-level switch for usage-limit toasts.
// Statusline rendering and provider notifications remain available when off.
func notificationsEnabled(env map[string]string) bool {
	config := setup.LoadPluginConfig(setup.ResolvePluginConfigDir(env))
	return config.NotifyEnabled
}

// updateNotification returns the update-toast callback only when plugin
// notifications are enabled.
func updateNotification(env map[string]string) updatecheck.NotifyFunc {
	if !notificationsEnabled(env) {
		return nil
	}
	return herdrcli.ShowNotification
}

// runStatusLineNotifications applies configured notification policy to one
// statusline render while the caller continues rendering its summary.
func runStatusLineNotifications(
	profile claude.ClaudeProfile,
	stdinJSON string,
	nowMs int64,
	config setup.PluginConfig,
	notify ratelimit.ShowNotificationFn,
) {
	if !config.NotifyEnabled {
		return
	}
	ratelimit.RunRateLimitCheckWithThresholdsIn(profile.StateDir, stdinJSON, nowMs, config.RemainingThresholds, notify)
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
		cwd := herdrcli.PaneSessionCwd(p.PaneInfo)
		snaps = append(snaps, limits.OpenPaneSnapshot{
			PaneID: p.PaneID, Agent: agent, Label: label,
			SessionID: sid, Cwd: cwd,
		})
	}
	return snaps, ok
}

// panelSnapshot is what one panel render needs: subscription providers plus
// pay-as-you-go spend blocks for the backends open panes are running.
type panelSnapshot struct {
	providers []limits.ProviderLimits
	apiUsage  []limits.APIProviderUsage
}

// collectPanel gathers everything the panel shows. activeOnly hides providers
// that have no open agent pane in Herdr (the panel default; --all overrides).
// When the pane query fails, all subscription providers are shown (fail-open).
func collectPanel(nowMs int64, activeOnly bool) panelSnapshot {
	snaps, panesOK := openPaneSnapshots()
	opts := limits.DefaultCollectOptions()
	if activeOnly {
		opts.Only = limits.ActiveProviderFilter(snaps, panesOK)
		// Subscription gate: hide providers whose open panes all run on
		// pay-as-you-go backends (--all bypasses both filters).
		billing := limits.BillingProviderFilter(snaps, panesOK, limits.DefaultBillingDeps())
		opts.Only = limits.IntersectFilters(opts.Only, billing)
	}
	opts.Attach = func(providers []limits.ProviderLimits, now int64) []limits.ProviderLimits {
		return limits.CollectAndAttachPaneActivity(providers, snaps, now)
	}
	base := limits.CollectAllProviderLimits(resolveCwd(), nowMs, opts)
	hist := limits.LoadUsageHistory()
	res := limits.EnrichRunOut(base, hist, nowMs, limits.DefaultRunOutOptions)
	limits.SaveUsageHistory(res.History)

	// Pay-as-you-go blocks have no quota to run out of, so they skip the
	// run-out enrichment entirely.
	return panelSnapshot{
		providers: res.Providers,
		apiUsage:  limits.CollectAPIProviderUsage(snaps, nowMs),
	}
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
		snap := collectPanel(nowMs, activeOnly)
		text := limits.FormatUsagePanel(snap.providers, snap.apiUsage, nowMs, layoutFor())
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
	// All painting (paintCached/renderFull) happens on this goroutine only: the
	// SIGWINCH handler forwards events through channels instead of writing to
	// stdout itself, so a resize repaint can never interleave escape sequences
	// with an in-progress full render.
	var (
		cachedSnap   panelSnapshot
		cachedLoaded bool
		cachedNowMs  int64
	)

	paintCached := func() {
		if !cachedLoaded {
			return
		}
		paintFrame(limits.FormatUsagePanel(cachedSnap.providers, cachedSnap.apiUsage, cachedNowMs, layoutFor()))
	}

	renderFull := func() {
		nowMs := time.Now().UnixMilli()
		cachedSnap = collectPanel(nowMs, activeOnly)
		cachedLoaded = true
		cachedNowMs = nowMs
		paintFrame(limits.FormatUsagePanel(cachedSnap.providers, cachedSnap.apiUsage, nowMs, layoutFor()))
	}
	renderFull()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// SIGWINCH: instant layout-only repaint (debounced full refresh after drag
	// ends). The goroutine only signals; painting stays on the main loop.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	resizeQuick := make(chan struct{}, 1)
	resizeFull := make(chan struct{}, 1)
	go func() {
		var debounce *time.Timer
		for range winch {
			// Ask the main loop for an immediate re-layout with cached data
			// (snappy while dragging).
			select {
			case resizeQuick <- struct{}{}:
			default:
			}
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
			case <-resizeQuick:
				paintCached()
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
		case <-resizeQuick:
			// Instant re-layout with cached data while the drag is ongoing.
			paintCached()
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
	env := environment()
	if !notificationsEnabled(env) {
		return
	}
	config := setup.LoadPluginConfig(setup.ResolvePluginConfigDir(env))
	nowMs := time.Now().UnixMilli()
	opts := limits.DefaultCollectOptions()
	// Never toast about subscription windows for pay-as-you-go setups.
	snaps, panesOK := openPaneSnapshots()
	opts.Only = limits.BillingProviderFilter(snaps, panesOK, limits.DefaultBillingDeps())
	providers := limits.CollectAllProviderLimits(resolveCwd(), nowMs, opts)
	limits.NotifyProviderPrimaryLimitsWithThresholds(providers, nowMs, config.RemainingThresholds)
}

func runStatusLine() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[usagebar-rate] read stdin: %v\n", err)
		os.Exit(1)
	}
	stdinJSON := string(data)
	nowMs := time.Now().UnixMilli()

	// Route this statusLine invocation to the profile matching its own
	// CLAUDE_CONFIG_DIR. The statusLine runs inside the Claude process, so the
	// env var identifies the account; when it is unset Claude is running the
	// default account and the profile for ~/.claude is the match.
	env := environment()
	profile, profiles, known := setup.ResolveActiveClaudeProfile(env)
	if !known {
		// Profiles are configured but none match this CLAUDE_CONFIG_DIR: skip all
		// writes and notifications rather than misattribute the account. Name the
		// mismatch on stderr — silently rendering correct numbers while never
		// caching them is otherwise invisible. Still print the summary so the
		// Claude status line renders.
		fmt.Fprintf(os.Stderr, "[usagebar-rate] no claude profile matches CLAUDE_CONFIG_DIR=%s; configured config_dirs: %s (skipping cache write and notifications)\n",
			describeConfigDir(env["CLAUDE_CONFIG_DIR"]), describeProfileDirs(profiles))
		printStatusLineSummary(stdinJSON)
		return
	}

	rateLimits := ratelimit.ParseRateLimits(stdinJSON)
	if rateLimits != nil {
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
		// Empty-payload guard lives in WriteClaudeLimitsCacheGuarded so `{}`
		// cannot clobber a valid cache.
		if _, err := limits.WriteClaudeLimitsCacheGuarded(input, nowMs, profile.LimitsCache); err != nil {
			fmt.Fprintf(os.Stderr, "[usagebar-rate] cache write failed: %v\n", err)
		}
	}

	// Prefix non-default profile toasts with the profile label so two accounts'
	// alerts are distinguishable.
	notify := herdrcli.ShowNotification
	if !claude.IsDefaultProfile(profile) {
		label := profile.Label
		inner := notify
		notify = func(title, body string) bool { return inner(label+": "+title, body) }
	}

	config := setup.LoadPluginConfig(setup.ResolvePluginConfigDir(env))
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[usagebar-rate] check failed: %v\n", r)
			}
		}()
		runStatusLineNotifications(profile, stdinJSON, nowMs, config, notify)
	}()
	printStatusLineSummary(stdinJSON)
}

func printStatusLineSummary(stdinJSON string) {
	summary := ratelimit.FormatStatusLineSummary(ratelimit.ParseRateLimits(stdinJSON))
	if summary != "" {
		fmt.Println(summary)
	}
}

// describeConfigDir renders CLAUDE_CONFIG_DIR for the profile-miss diagnostic,
// distinguishing "unset" (the default account) from an explicit value.
func describeConfigDir(configDir string) string {
	if configDir == "" {
		return "(unset, i.e. the default account)"
	}
	return configDir
}

// describeProfileDirs lists the configured profiles as id=config_dir pairs.
func describeProfileDirs(profiles []claude.ClaudeProfile) string {
	parts := make([]string, len(profiles))
	for i, p := range profiles {
		parts[i] = p.ID + "=" + p.ConfigDir
	}
	return strings.Join(parts, ", ")
}

// runOpenCodeCheck reports each stage of the OpenCode Go usage path so a
// failure can be attributed to the browser session or to the fetch, without
// printing any cookie value.
func runOpenCodeCheck() {
	nowMs := time.Now().UnixMilli()
	cookie := ""

	if env := strings.TrimSpace(os.Getenv("OPENCODE_GO_COOKIE")); env != "" {
		cookie = env
		fmt.Println("cookie: OPENCODE_GO_COOKIE is set (browser import skipped)")
	} else if strings.TrimSpace(os.Getenv("USAGEBAR_DISABLE_BROWSER_COOKIES")) != "" {
		// Distinguish "opted out" from "looked and found nothing".
		fmt.Println("cookie: browser import is disabled by USAGEBAR_DISABLE_BROWSER_COOKIES")
		fmt.Println("        unset it, or set OPENCODE_GO_COOKIE, to use official usage")
		return
	} else {
		imported, probes, ok := limits.ImportBrowserCookieHeaderWithProbes(limits.OpenCodeCookieDomain, nowMs)
		fmt.Printf("browser profiles probed: %d\n", len(probes))
		for _, p := range probes {
			switch {
			case p.Rows == 0:
				fmt.Printf("  - %s / %s: no %s cookies\n", p.Browser, p.Profile, limits.OpenCodeCookieDomain)
			case !p.KeychainOK:
				fmt.Printf("  - %s / %s: %d cookies, but the Keychain password was unavailable"+
					" (access denied, or the Safe Storage item is named differently)\n", p.Browser, p.Profile, p.Rows)
			case p.Decrypted == 0:
				fmt.Printf("  - %s / %s: %d cookies, none decrypted (unexpected encryption scheme)\n", p.Browser, p.Profile, p.Rows)
			default:
				fmt.Printf("  - %s / %s: %d cookies, %d decrypted\n", p.Browser, p.Profile, p.Rows, p.Decrypted)
			}
		}
		if !ok {
			fmt.Printf("cookie: no %s session imported\n", limits.OpenCodeCookieDomain)
			fmt.Println("        sign in to opencode.ai in Chrome/Arc, allow Keychain access when macOS asks,")
			fmt.Println("        or set OPENCODE_GO_COOKIE manually")
			return
		}
		cookie = imported.Header
		fmt.Printf("cookie: imported from %s / %s (%d cookies: %s)\n",
			imported.Browser, imported.Profile, len(imported.Names), strings.Join(imported.Names, ", "))
	}

	// Deliberately bypasses the collector's usage cache: this command exists to
	// exercise the live path, so it always makes the request.
	snap := limits.FetchOpenCodeGoWebUsage(cookie, nowMs)
	if snap == nil {
		fmt.Println("fetch: opencode.ai returned no usage (session expired, or the page changed)")
		return
	}
	fmt.Printf("fetch: workspace %s via %s\n", snap.WorkspaceID, snap.Source)
	pl := limits.ProviderLimitsFromWebSnapshot(*snap, nowMs)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(pl)
}

func runCollectJSON(args []string) {
	nowMs := time.Now().UnixMilli()
	snap := collectPanel(nowMs, !hasFlag(args, "--all"))
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(struct {
		Providers []limits.ProviderLimits   `json:"providers"`
		APIUsage  []limits.APIProviderUsage `json:"apiUsage,omitempty"`
	}{snap.providers, snap.apiUsage})
}
