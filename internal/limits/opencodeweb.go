/**
 * Fetches official OpenCode Go usage from opencode.ai:
 *   1. workspaces server-fn → wrk_…
 *   2. GET /workspace/{id}/go HTML/JS → rolling/weekly/monthly usage
 *
 * OPENCODE_GO_COOKIE supplies the Cookie header. Without it, callers fall back to local SQLite.
 */
package limits

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OpenCodeWorkspacesServerID is the opencode.ai server-fn id for listing workspaces.
const OpenCodeWorkspacesServerID = "def39973159c7f0483d8793a822b8dbb10d067e12c65455fcb4608459ba0234f"

const openCodeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"

// ParseWorkspaceIDs extracts wrk_… ids from page text.
func ParseWorkspaceIDs(text string) []string {
	re := regexp.MustCompile(`wrk_[A-Za-z0-9]+`)
	found := re.FindAllString(text, -1)
	var uniq []string
	seen := map[string]struct{}{}
	for _, id := range found {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	return uniq
}

// ParsedOpenCodeGoUsage is rolling/weekly/monthly from the Go page.
type ParsedOpenCodeGoUsage struct {
	Rolling OpenCodeGoWebWindow
	Weekly  *OpenCodeGoWebWindow
	Monthly *OpenCodeGoWebWindow
}

// ParseOpenCodeGoUsagePage reads rolling/weekly/monthly usage from embedded JS.
func ParseOpenCodeGoUsagePage(text string) *ParsedOpenCodeGoUsage {
	rolling := extractUsageBlock(text, "rollingUsage")
	if rolling == nil {
		return nil
	}
	return &ParsedOpenCodeGoUsage{
		Rolling: *rolling,
		Weekly:  extractUsageBlock(text, "weeklyUsage"),
		Monthly: extractUsageBlock(text, "monthlyUsage"),
	}
}

func extractUsageBlock(text, name string) *OpenCodeGoWebWindow {
	pctRe := regexp.MustCompile(name + `[^}]{0,240}?usagePercent\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
	resetRe := regexp.MustCompile(name + `[^}]{0,240}?resetInSec\s*:\s*([0-9]+)`)
	pctMatch := pctRe.FindStringSubmatch(text)
	if pctMatch == nil {
		return nil
	}
	used, err := strconv.ParseFloat(pctMatch[1], 64)
	if err != nil {
		return nil
	}
	resetInSec := 0
	if m := resetRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			if n < 0 {
				n = 0
			}
			resetInSec = n
		}
	}
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	return &OpenCodeGoWebWindow{UsedPercentage: used, ResetInSec: resetInSec}
}

// OpenCodeCookieDomain is the domain whose browser session authenticates the
// usage pages.
const OpenCodeCookieDomain = "opencode.ai"

// ReadOpenCodeGoCookieHeader returns the Cookie header to send to opencode.ai:
// OPENCODE_GO_COOKIE when set, otherwise a session imported from a local
// Chromium-family browser profile. The env var stays authoritative so a manual
// header can always override (or stand in for) the browser import.
func ReadOpenCodeGoCookieHeader() string {
	if v := strings.TrimSpace(os.Getenv("OPENCODE_GO_COOKIE")); v != "" {
		return v
	}
	imported, ok := ImportBrowserCookieHeader(OpenCodeCookieDomain, time.Now().UnixMilli())
	if !ok {
		return ""
	}
	return imported.Header
}

func randomServerInstance() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "server-fn:" + hex.EncodeToString(b[:])
}

func fetchOpenCodeText(client *http.Client, url, cookie string, method string, body []byte, extra map[string]string) (string, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", openCodeUserAgent)
	req.Header.Set("Accept", "text/javascript, application/json;q=0.9, text/html;q=0.8, */*;q=0.7")
	req.Header.Set("Origin", "https://opencode.ai")
	req.Header.Set("Referer", "https://opencode.ai/")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(data), nil
}

// openCodeMicroCentsWindow converts one console meter into a window.
type openCodeConsoleMeter struct {
	ResetsAt        string `json:"resetsAt"`
	LimitMicroCents string `json:"limitMicroCents"`
	UsedMicroCents  string `json:"usedMicroCents"`
}

func consoleMeterWindow(m openCodeConsoleMeter, nowMs int64) *OpenCodeGoWebWindow {
	limit, err := strconv.ParseFloat(m.LimitMicroCents, 64)
	if err != nil || limit <= 0 {
		return nil
	}
	usedMicro, err := strconv.ParseFloat(m.UsedMicroCents, 64)
	if err != nil {
		return nil
	}
	used := usedMicro / limit * 100
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	resetInSec := 0
	if m.ResetsAt != "" {
		if ts, err := time.Parse(time.RFC3339, m.ResetsAt); err == nil {
			if d := ts.UnixMilli() - nowMs; d > 0 {
				resetInSec = int(d / 1000)
			}
		}
	}
	return &OpenCodeGoWebWindow{UsedPercentage: used, ResetInSec: resetInSec}
}

// fetchOpenCodeConsoleUsage reads the live console API
// (opencode.ai/console/api/go/status), which replaced the workspace page the
// legacy scraper below targets. Requires only the session cookie plus the
// account's first org id.
func fetchOpenCodeConsoleUsage(client *http.Client, cookie string, nowMs int64) *OpenCodeGoWebSnapshot {
	orgText, err := fetchOpenCodeText(client, "https://opencode.ai/console/api/orgs", cookie, http.MethodGet, nil, map[string]string{
		"Accept": "application/json",
	})
	if err != nil {
		return nil
	}
	var orgs []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(orgText), &orgs); err != nil || len(orgs) == 0 || orgs[0].ID == "" {
		return nil
	}
	orgID := orgs[0].ID

	statusText, err := fetchOpenCodeText(client, "https://opencode.ai/console/api/go/status", cookie, http.MethodGet, nil, map[string]string{
		"Accept":   "application/json",
		"X-Org-Id": orgID,
	})
	if err != nil {
		return nil
	}
	return ParseOpenCodeConsoleStatus(statusText, orgID, nowMs)
}

// ParseOpenCodeConsoleStatus turns one console go-status body into a snapshot.
// Micro-cent amounts arrive as JSON strings, and any window may be absent.
func ParseOpenCodeConsoleStatus(statusText, orgID string, nowMs int64) *OpenCodeGoWebSnapshot {
	var status struct {
		Access struct {
			Meters map[string]openCodeConsoleMeter `json:"meters"`
		} `json:"access"`
	}
	if err := json.Unmarshal([]byte(statusText), &status); err != nil {
		return nil
	}
	rolling := consoleMeterWindow(status.Access.Meters["fiveHour"], nowMs)
	if rolling == nil {
		return nil
	}
	return &OpenCodeGoWebSnapshot{
		Rolling:     *rolling,
		Weekly:      consoleMeterWindow(status.Access.Meters["week"], nowMs),
		Monthly:     consoleMeterWindow(status.Access.Meters["month"], nowMs),
		WorkspaceID: orgID,
		Source:      "opencode.ai console",
	}
}

// FetchOpenCodeGoWebUsage tries cookie-backed official usage from opencode.ai.
func FetchOpenCodeGoWebUsage(cookie string, nowMs int64) *OpenCodeGoWebSnapshot {
	_ = nowMs
	if cookie == "" {
		cookie = ReadOpenCodeGoCookieHeader()
	}
	if cookie == "" {
		return nil
	}
	client := &http.Client{Timeout: 20 * time.Second}

	if snap := fetchOpenCodeConsoleUsage(client, cookie, nowMs); snap != nil {
		return snap
	}

	workspacesURL := "https://opencode.ai/_server?id=" + OpenCodeWorkspacesServerID
	workspacesText, err := fetchOpenCodeText(client, workspacesURL, cookie, http.MethodGet, nil, map[string]string{
		"X-Server-Id":       OpenCodeWorkspacesServerID,
		"X-Server-Instance": randomServerInstance(),
	})
	if err != nil {
		return nil
	}
	lower := strings.ToLower(workspacesText)
	if strings.Contains(lower, "not associated with an account") ||
		strings.Contains(lower, `actor of type "public"`) {
		return nil
	}
	ids := ParseWorkspaceIDs(workspacesText)
	if len(ids) == 0 {
		postText, err := fetchOpenCodeText(client, "https://opencode.ai/_server", cookie, http.MethodPost, []byte("[]"), map[string]string{
			"Content-Type":      "application/json",
			"X-Server-Id":       OpenCodeWorkspacesServerID,
			"X-Server-Instance": randomServerInstance(),
		})
		if err == nil {
			ids = ParseWorkspaceIDs(postText)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	workspaceID := ids[0]
	page, err := fetchOpenCodeText(
		client,
		fmt.Sprintf("https://opencode.ai/workspace/%s/go", workspaceID),
		cookie,
		http.MethodGet,
		nil,
		map[string]string{
			"Accept":  "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Referer": fmt.Sprintf("https://opencode.ai/workspace/%s/go", workspaceID),
		},
	)
	if err != nil {
		return nil
	}
	parsed := ParseOpenCodeGoUsagePage(page)
	if parsed == nil {
		return nil
	}
	return &OpenCodeGoWebSnapshot{
		Rolling:     parsed.Rolling,
		Weekly:      parsed.Weekly,
		Monthly:     parsed.Monthly,
		WorkspaceID: workspaceID,
		Source:      "opencode.ai web",
	}
}
