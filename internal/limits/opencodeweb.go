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

// ReadOpenCodeGoCookieHeader returns OPENCODE_GO_COOKIE if set.
func ReadOpenCodeGoCookieHeader() string {
	return strings.TrimSpace(os.Getenv("OPENCODE_GO_COOKIE"))
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
