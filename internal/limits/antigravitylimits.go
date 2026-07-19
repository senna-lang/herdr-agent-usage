/**
 * Antigravity quota collection from the local language-server API.
 *
 * The probe only talks to 127.0.0.1. It discovers Antigravity/agy processes,
 * obtains their listening ports, and calls the same internal endpoints used by
 * CodexBar. Self-signed TLS is accepted only for this fixed loopback target.
 */
package limits

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	antigravityQuotaSummaryPath = "/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary"
	antigravityUserStatusPath   = "/exa.language_server_pb.LanguageServerService/GetUserStatus"
	antigravityModelConfigPath  = "/exa.language_server_pb.LanguageServerService/GetCommandModelConfigs"
)

// AntigravityProcess is a local process that may expose quota endpoints.
type AntigravityProcess struct {
	PID                      int
	CSRFToken                string
	RequiresCSRF             bool
	ExtensionPort            int
	ExtensionServerCSRFToken string
}

// CollectAntigravityLimitsOptions injects process and network dependencies.
type CollectAntigravityLimitsOptions struct {
	ProcessList    func() (string, error)
	ListeningPorts func(pid int) ([]int, error)
	HTTPClient     *http.Client
}

// CollectAntigravityLimits probes a running Antigravity app, IDE, or agy CLI.
func CollectAntigravityLimits(_ *string, nowMs int64, opts CollectAntigravityLimitsOptions) ProviderLimits {
	result := ProviderLimits{
		ProviderID:  "antigravity",
		Label:       "Antigravity",
		Source:      "Antigravity local API",
		FetchedAtMs: nowMs,
	}
	processList := opts.ProcessList
	if processList == nil {
		processList = antigravityProcessList
	}
	output, err := processList()
	if err != nil {
		result.Note = strPtr("Antigravity process detection failed: " + err.Error())
		return result
	}
	processes := parseAntigravityProcesses(output)
	if len(processes) == 0 {
		result.Note = strPtr("start Antigravity or agy to fetch local limits")
		return result
	}
	listeningPorts := opts.ListeningPorts
	if listeningPorts == nil {
		listeningPorts = antigravityListeningPorts
	}
	client := opts.HTTPClient
	if client == nil {
		client = antigravityHTTPClient()
	}

	var lastErr error
	for _, process := range processes {
		ports, portErr := listeningPorts(process.PID)
		if portErr != nil || len(ports) == 0 {
			if portErr != nil {
				lastErr = portErr
			}
			continue
		}
		parsed, fetchErr := fetchAntigravityQuota(client, antigravityEndpoints(process, ports))
		if fetchErr != nil {
			lastErr = fetchErr
			continue
		}
		result.Primary = parsed.primary
		result.Secondary = parsed.secondary
		result.Tertiary = parsed.tertiary
		result.PlanType = parsed.plan
		result.Note = parsed.note
		return result
	}
	if lastErr != nil {
		result.Note = strPtr("Antigravity local quota unavailable: " + lastErr.Error())
	} else {
		result.Note = strPtr("Antigravity is running but exposes no listening quota port")
	}
	return result
}

func antigravityProcessList() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

var antigravityFlagPattern = regexp.MustCompile(`(?i)(--[a-z_]+)(?:=|\s+)([^\s]+)`)

func parseAntigravityProcesses(output string) []AntigravityProcess {
	var processes []AntigravityProcess
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		lower := strings.ToLower(command)
		isCLI := antigravityCLICommand(lower)
		isServer := antigravityLanguageServerCommand(lower)
		if !isCLI && !isServer {
			continue
		}
		flags := make(map[string]string)
		for _, match := range antigravityFlagPattern.FindAllStringSubmatch(command, -1) {
			flags[strings.ToLower(match[1])] = match[2]
		}
		csrf := flags["--csrf_token"]
		if isServer && csrf == "" {
			continue
		}
		extensionPort, _ := strconv.Atoi(flags["--extension_server_port"])
		processes = append(processes, AntigravityProcess{
			PID:                      pid,
			CSRFToken:                csrf,
			RequiresCSRF:             !isCLI,
			ExtensionPort:            extensionPort,
			ExtensionServerCSRFToken: flags["--extension_server_csrf_token"],
		})
	}
	return processes
}

func antigravityCLICommand(lower string) bool {
	for _, marker := range []string{"/agy ", "/agy", " antigravity-cli", "/antigravity-cli", " antigravity_cli", "/antigravity_cli"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.HasPrefix(lower, "agy ") || lower == "agy"
}

func antigravityLanguageServerCommand(lower string) bool {
	server := strings.Contains(lower, "language_server") || strings.Contains(lower, "language-server")
	marker := strings.Contains(lower, "antigravity")
	return server && marker
}

var antigravityListenPortPattern = regexp.MustCompile(`:(\d+)\s+\(LISTEN\)`)

func antigravityListeningPorts(pid int) ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	output, err := exec.CommandContext(
		ctx,
		"lsof",
		"-nP",
		"-iTCP",
		"-sTCP:LISTEN",
		"-a",
		"-p",
		strconv.Itoa(pid),
	).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("lsof pid %d: %w", pid, err)
	}
	seen := make(map[int]bool)
	var ports []int
	for _, match := range antigravityListenPortPattern.FindAllStringSubmatch(string(output), -1) {
		port, err := strconv.Atoi(match[1])
		if err == nil && !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}
	sort.Ints(ports)
	return ports, nil
}

type antigravityEndpoint struct {
	Scheme    string
	Port      int
	CSRFToken string
	UseCSRF   bool
}

func antigravityEndpoints(process AntigravityProcess, ports []int) []antigravityEndpoint {
	var endpoints []antigravityEndpoint
	seen := make(map[string]bool)
	appendEndpoint := func(endpoint antigravityEndpoint) {
		key := fmt.Sprintf("%s:%d:%t:%s", endpoint.Scheme, endpoint.Port, endpoint.UseCSRF, endpoint.CSRFToken)
		if !seen[key] {
			seen[key] = true
			endpoints = append(endpoints, endpoint)
		}
	}
	for _, port := range ports {
		appendEndpoint(antigravityEndpoint{"https", port, process.CSRFToken, process.RequiresCSRF})
		appendEndpoint(antigravityEndpoint{"http", port, process.CSRFToken, process.RequiresCSRF})
	}
	if process.ExtensionPort > 0 {
		token := process.ExtensionServerCSRFToken
		if token == "" {
			token = process.CSRFToken
		}
		appendEndpoint(antigravityEndpoint{"http", process.ExtensionPort, token, true})
	}
	return endpoints
}

func antigravityHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true} // #nosec G402 -- fixed 127.0.0.1 target only
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

type parsedAntigravityQuota struct {
	primary   *LimitWindow
	secondary *LimitWindow
	tertiary  *LimitWindow
	plan      *string
	note      *string
}

func fetchAntigravityQuota(client *http.Client, endpoints []antigravityEndpoint) (*parsedAntigravityQuota, error) {
	var lastErr error
	for _, endpoint := range endpoints {
		body, err := antigravityRequest(client, endpoint, antigravityQuotaSummaryPath, map[string]any{"forceRefresh": true})
		if err == nil {
			parsed, parseErr := parseAntigravityQuotaSummary(body)
			if parseErr == nil && parsed.primary != nil {
				identityBody, identityErr := antigravityRequest(client, endpoint, antigravityUserStatusPath, antigravityMetadataBody())
				if identityErr == nil {
					applyAntigravityIdentity(parsed, identityBody)
				}
				return parsed, nil
			}
			lastErr = parseErr
		} else {
			lastErr = err
		}
		for _, fallback := range []struct {
			path string
			body map[string]any
		}{
			{antigravityUserStatusPath, antigravityMetadataBody()},
			{antigravityModelConfigPath, antigravityMetadataBody()},
		} {
			body, err = antigravityRequest(client, endpoint, fallback.path, fallback.body)
			if err != nil {
				lastErr = err
				continue
			}
			parsed, parseErr := parseAntigravityLegacyQuota(body)
			if parseErr == nil && parsed.primary != nil {
				return parsed, nil
			}
			lastErr = parseErr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no local endpoints")
	}
	return nil, lastErr
}

func antigravityRequest(client *http.Client, endpoint antigravityEndpoint, path string, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	requestURL := fmt.Sprintf("%s://127.0.0.1:%d%s", endpoint.Scheme, endpoint.Port, path)
	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if endpoint.UseCSRF {
		req.Header.Set("X-Codeium-Csrf-Token", endpoint.CSRFToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return responseBody, nil
}

func antigravityMetadataBody() map[string]any {
	return map[string]any{"metadata": map[string]any{
		"ideName": "antigravity", "extensionName": "antigravity", "ideVersion": "unknown", "locale": "en",
	}}
}

type antigravityQuotaSummaryResponse struct {
	Code     any                      `json:"code"`
	Response *antigravityQuotaPayload `json:"response"`
	Summary  *antigravityQuotaPayload `json:"summary"`
	Groups   []antigravityQuotaGroup  `json:"groups"`
}

type antigravityQuotaPayload struct {
	Groups []antigravityQuotaGroup `json:"groups"`
}

type antigravityQuotaGroup struct {
	DisplayName string                   `json:"displayName"`
	Buckets     []antigravityQuotaBucket `json:"buckets"`
}

type antigravityQuotaBucket struct {
	BucketID          string   `json:"bucketId"`
	DisplayName       string   `json:"displayName"`
	Description       string   `json:"description"`
	RemainingFraction *float64 `json:"remainingFraction"`
	Remaining         *struct {
		RemainingFraction *float64 `json:"remainingFraction"`
		Case              string   `json:"case"`
		Value             *float64 `json:"value"`
	} `json:"remaining"`
	ResetTime string `json:"resetTime"`
	Disabled  bool   `json:"disabled"`
}

func parseAntigravityQuotaSummary(data []byte) (*parsedAntigravityQuota, error) {
	var response antigravityQuotaSummaryResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parse quota summary: %w", err)
	}
	payload := response.Response
	if payload == nil {
		payload = response.Summary
	}
	groups := response.Groups
	if payload != nil {
		groups = payload.Groups
	}
	best := make(map[int]*LimitWindow)
	for _, group := range groups {
		for _, bucket := range group.Buckets {
			remaining := bucket.RemainingFraction
			if remaining == nil && bucket.Remaining != nil {
				remaining = bucket.Remaining.RemainingFraction
				if remaining == nil && bucket.Remaining.Case == "remainingFraction" {
					remaining = bucket.Remaining.Value
				}
			}
			if bucket.Disabled || remaining == nil {
				continue
			}
			minutes := antigravityWindowMinutes(bucket.BucketID + " " + bucket.DisplayName + " " + bucket.Description)
			if minutes == 0 {
				continue
			}
			used := 100 - (*remaining * 100)
			if used < 0 {
				used = 0
			}
			if used > 100 {
				used = 100
			}
			window := &LimitWindow{UsedPercentage: used, WindowMinutes: &minutes, ResetsAt: parseAntigravityReset(bucket.ResetTime)}
			if current := best[minutes]; current == nil || window.UsedPercentage > current.UsedPercentage {
				best[minutes] = window
			}
		}
	}
	minutes := make([]int, 0, len(best))
	for value := range best {
		minutes = append(minutes, value)
	}
	sort.Ints(minutes)
	parsed := &parsedAntigravityQuota{}
	ordered := []*LimitWindow{nil, nil, nil}
	for i, value := range minutes {
		if i == len(ordered) {
			break
		}
		ordered[i] = best[value]
	}
	parsed.primary, parsed.secondary, parsed.tertiary = ordered[0], ordered[1], ordered[2]
	if parsed.primary == nil {
		return nil, fmt.Errorf("quota summary has no supported buckets")
	}
	parsed.note = strPtr("most constrained quota across Gemini and Claude + GPT")
	return parsed, nil
}

func antigravityWindowMinutes(text string) int {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "week") || strings.Contains(lower, "7d") || strings.Contains(lower, "weekly") {
		return 7 * 24 * 60
	}
	if strings.Contains(lower, "5h") || strings.Contains(lower, "5 h") || strings.Contains(lower, "5-hour") || strings.Contains(lower, "5 hour") || strings.Contains(lower, "session") {
		return 5 * 60
	}
	if strings.Contains(lower, "month") || strings.Contains(lower, "30d") {
		return 30 * 24 * 60
	}
	return 0
}

func parseAntigravityReset(raw string) *int64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil {
		if number > 10_000_000_000 {
			number /= 1000
		}
		return &number
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			seconds := parsed.Unix()
			return &seconds
		}
	}
	return nil
}

type antigravityLegacyResponse struct {
	UserStatus *struct {
		Email    *string `json:"email"`
		UserTier *struct {
			Name *string `json:"name"`
		} `json:"userTier"`
		PlanStatus *struct {
			PlanInfo *struct {
				PlanName        *string `json:"planName"`
				PlanDisplayName *string `json:"planDisplayName"`
				DisplayName     *string `json:"displayName"`
			} `json:"planInfo"`
		} `json:"planStatus"`
		CascadeModelConfigData *struct {
			ClientModelConfigs []antigravityModelConfig `json:"clientModelConfigs"`
		} `json:"cascadeModelConfigData"`
	} `json:"userStatus"`
	ClientModelConfigs []antigravityModelConfig `json:"clientModelConfigs"`
}

type antigravityModelConfig struct {
	Label        string `json:"label"`
	ModelOrAlias struct {
		Model string `json:"model"`
	} `json:"modelOrAlias"`
	QuotaInfo *struct {
		RemainingFraction *float64 `json:"remainingFraction"`
		ResetTime         string   `json:"resetTime"`
	} `json:"quotaInfo"`
}

func parseAntigravityLegacyQuota(data []byte) (*parsedAntigravityQuota, error) {
	var response antigravityLegacyResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parse legacy quota: %w", err)
	}
	models := response.ClientModelConfigs
	parsed := antigravityIdentity(response)
	if response.UserStatus != nil {
		if response.UserStatus.CascadeModelConfigData != nil {
			models = response.UserStatus.CascadeModelConfigData.ClientModelConfigs
		}
	}
	var best *LimitWindow
	for _, model := range models {
		if model.QuotaInfo == nil || model.QuotaInfo.RemainingFraction == nil {
			continue
		}
		minutes := 5 * 60
		used := 100 - (*model.QuotaInfo.RemainingFraction * 100)
		window := &LimitWindow{UsedPercentage: used, WindowMinutes: &minutes, ResetsAt: parseAntigravityReset(model.QuotaInfo.ResetTime)}
		if best == nil || window.UsedPercentage > best.UsedPercentage {
			best = window
		}
	}
	parsed.primary = best
	if best == nil {
		return nil, fmt.Errorf("legacy response has no usable model quota")
	}
	if parsed.note == nil {
		parsed.note = strPtr("most constrained local model quota")
	}
	return parsed, nil
}

func applyAntigravityIdentity(parsed *parsedAntigravityQuota, data []byte) {
	var response antigravityLegacyResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return
	}
	identity := antigravityIdentity(response)
	parsed.plan = identity.plan
	if identity.note != nil {
		parsed.note = identity.note
	}
}

func antigravityIdentity(response antigravityLegacyResponse) *parsedAntigravityQuota {
	parsed := &parsedAntigravityQuota{}
	if response.UserStatus == nil {
		return parsed
	}
	if response.UserStatus.UserTier != nil {
		parsed.plan = firstNonEmpty(response.UserStatus.UserTier.Name)
	}
	if parsed.plan == nil && response.UserStatus.PlanStatus != nil && response.UserStatus.PlanStatus.PlanInfo != nil {
		info := response.UserStatus.PlanStatus.PlanInfo
		parsed.plan = firstNonEmpty(info.PlanDisplayName, info.DisplayName, info.PlanName)
	}
	if response.UserStatus.Email != nil && strings.TrimSpace(*response.UserStatus.Email) != "" {
		parsed.note = strPtr("account " + strings.TrimSpace(*response.UserStatus.Email))
	}
	return parsed
}
