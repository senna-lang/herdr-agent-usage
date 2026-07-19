/**
 * z.ai coding-plan quota collection.
 *
 * Authentication and endpoint selection mirror CodexBar's provider:
 * Z_AI_API_KEY supplies the bearer token, Z_AI_QUOTA_URL overrides the full
 * endpoint, and Z_AI_API_HOST overrides only the API host. Credential-bearing
 * requests are sent only to HTTPS endpoints.
 */
package limits

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	zaiGlobalHost = "https://api.z.ai"
	zaiChinaHost  = "https://open.bigmodel.cn"
	zaiQuotaPath  = "/api/monitor/usage/quota/limit"
)

// CollectZaiLimitsOptions injects environment and HTTP dependencies for tests.
type CollectZaiLimitsOptions struct {
	Environment map[string]string
	HTTPClient  *http.Client
}

type zaiQuotaResponse struct {
	Code    int           `json:"code"`
	Message *string       `json:"msg"`
	Data    *zaiQuotaData `json:"data"`
	Success bool          `json:"success"`
}

type zaiQuotaData struct {
	Limits      []zaiLimitRaw `json:"limits"`
	PlanName    *string       `json:"planName"`
	Plan        *string       `json:"plan"`
	PlanType    *string       `json:"plan_type"`
	PackageName *string       `json:"packageName"`
}

type zaiLimitRaw struct {
	Type          string   `json:"type"`
	Unit          int      `json:"unit"`
	Number        int      `json:"number"`
	Usage         *float64 `json:"usage"`
	CurrentValue  *float64 `json:"currentValue"`
	Remaining     *float64 `json:"remaining"`
	Percentage    float64  `json:"percentage"`
	NextResetTime *int64   `json:"nextResetTime"`
}

// CollectZaiLimits fetches personal or team coding-plan quota windows.
func CollectZaiLimits(_ *string, nowMs int64, opts CollectZaiLimitsOptions) ProviderLimits {
	result := ProviderLimits{
		ProviderID:  "zai",
		Label:       "Z.ai",
		Source:      "z.ai quota API",
		FetchedAtMs: nowMs,
	}
	env := opts.Environment
	if env == nil {
		env = environmentMap()
	}
	apiKey := strings.TrimSpace(env["Z_AI_API_KEY"])
	if apiKey == "" {
		result.Note = strPtr("set Z_AI_API_KEY to fetch coding-plan limits")
		return result
	}

	endpoint, err := resolveZaiQuotaURL(env)
	if err != nil {
		result.Note = strPtr(err.Error())
		return result
	}
	if strings.EqualFold(strings.TrimSpace(env["Z_AI_USAGE_SCOPE"]), "team") {
		org := strings.TrimSpace(env["Z_AI_BIGMODEL_ORGANIZATION"])
		project := strings.TrimSpace(env["Z_AI_BIGMODEL_PROJECT"])
		if org == "" || project == "" {
			result.Note = strPtr("team usage requires Z_AI_BIGMODEL_ORGANIZATION and Z_AI_BIGMODEL_PROJECT")
			return result
		}
		query := endpoint.Query()
		query.Set("type", "2")
		endpoint.RawQuery = query.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		result.Note = strPtr("z.ai request: " + err.Error())
		return result
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	if strings.EqualFold(strings.TrimSpace(env["Z_AI_USAGE_SCOPE"]), "team") {
		req.Header.Set("Bigmodel-Organization", strings.TrimSpace(env["Z_AI_BIGMODEL_ORGANIZATION"]))
		req.Header.Set("Bigmodel-Project", strings.TrimSpace(env["Z_AI_BIGMODEL_PROJECT"]))
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		result.Note = strPtr("z.ai quota request failed: " + err.Error())
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		result.Note = strPtr("z.ai quota response: " + err.Error())
		return result
	}
	if resp.StatusCode != http.StatusOK {
		result.Note = strPtr(fmt.Sprintf("z.ai quota API HTTP %d", resp.StatusCode))
		return result
	}
	parsed, err := parseZaiQuota(body)
	if err != nil {
		result.Note = strPtr(err.Error())
		return result
	}
	result.Primary, result.Secondary, result.Tertiary = parsed.windows[0], parsed.windows[1], parsed.windows[2]
	result.PlanType = parsed.plan
	if result.Primary == nil {
		result.Note = strPtr("z.ai quota API returned no supported limits")
	}
	return result
}

type parsedZaiQuota struct {
	windows [3]*LimitWindow
	plan    *string
}

func parseZaiQuota(data []byte) (*parsedZaiQuota, error) {
	var response zaiQuotaResponse
	if len(data) == 0 {
		return nil, fmt.Errorf("z.ai quota API returned an empty response")
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parse z.ai quota response: %w", err)
	}
	if !response.Success || response.Code != http.StatusOK {
		message := fmt.Sprintf("z.ai quota API returned code %d", response.Code)
		if response.Message != nil && strings.TrimSpace(*response.Message) != "" {
			message = strings.TrimSpace(*response.Message)
		}
		return nil, fmt.Errorf("%s", message)
	}
	if response.Data == nil {
		return nil, fmt.Errorf("z.ai quota API response is missing data")
	}

	var windows []*LimitWindow
	for _, raw := range response.Data.Limits {
		window := zaiWindow(raw)
		if window != nil {
			windows = append(windows, window)
		}
	}
	sort.SliceStable(windows, func(i, j int) bool {
		return windowMinutesForSort(windows[i]) < windowMinutesForSort(windows[j])
	})
	if len(windows) > 3 {
		windows = windows[:3]
	}
	parsed := &parsedZaiQuota{plan: firstNonEmpty(
		response.Data.PlanName,
		response.Data.Plan,
		response.Data.PlanType,
		response.Data.PackageName,
	)}
	copy(parsed.windows[:], windows)
	return parsed, nil
}

func zaiWindow(raw zaiLimitRaw) *LimitWindow {
	if raw.Type != "TOKENS_LIMIT" && raw.Type != "TIME_LIMIT" {
		return nil
	}
	minutes := zaiWindowMinutes(raw)
	if minutes == 0 {
		return nil
	}
	used := raw.Percentage
	if raw.Usage != nil && *raw.Usage > 0 {
		var usedRaw *float64
		if raw.Remaining != nil {
			value := *raw.Usage - *raw.Remaining
			if raw.CurrentValue != nil && *raw.CurrentValue > value {
				value = *raw.CurrentValue
			}
			usedRaw = &value
		} else if raw.CurrentValue != nil {
			usedRaw = raw.CurrentValue
		}
		if usedRaw != nil {
			used = (*usedRaw / *raw.Usage) * 100
		}
	}
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	window := &LimitWindow{UsedPercentage: used, WindowMinutes: &minutes}
	if raw.NextResetTime != nil {
		seconds := *raw.NextResetTime
		if seconds > 10_000_000_000 {
			seconds /= 1000
		}
		window.ResetsAt = &seconds
	}
	return window
}

func zaiWindowMinutes(raw zaiLimitRaw) int {
	// z.ai reports the MCP monthly allowance as TIME_LIMIT/1 minute.
	if raw.Type == "TIME_LIMIT" && raw.Unit == 5 && raw.Number == 1 {
		return 30 * 24 * 60
	}
	if raw.Number <= 0 {
		return 0
	}
	switch raw.Unit {
	case 1:
		return raw.Number * 24 * 60
	case 3:
		return raw.Number * 60
	case 5:
		return raw.Number
	case 6:
		return raw.Number * 7 * 24 * 60
	default:
		return 0
	}
}

func windowMinutesForSort(window *LimitWindow) int {
	if window == nil || window.WindowMinutes == nil {
		return int(^uint(0) >> 1)
	}
	return *window.WindowMinutes
}

func firstNonEmpty(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			cleaned := strings.TrimSpace(*value)
			return &cleaned
		}
	}
	return nil
}

func resolveZaiQuotaURL(env map[string]string) (*url.URL, error) {
	raw := strings.TrimSpace(env["Z_AI_QUOTA_URL"])
	if raw == "" {
		host := strings.TrimSpace(env["Z_AI_API_HOST"])
		if host == "" {
			if strings.EqualFold(strings.TrimSpace(env["Z_AI_API_REGION"]), "bigmodel-cn") {
				host = zaiChinaHost
			} else {
				host = zaiGlobalHost
			}
		}
		raw = strings.TrimRight(host, "/")
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
			return nil, fmt.Errorf("Z_AI_API_HOST must be an HTTPS URL or bare host")
		}
		if parsed.Path == "" || parsed.Path == "/" {
			parsed.Path = zaiQuotaPath
		}
		return parsed, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
		return nil, fmt.Errorf("Z_AI_QUOTA_URL must be an HTTPS URL or bare host")
	}
	return parsed, nil
}

func environmentMap() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}
