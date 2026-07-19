package limits

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseAntigravityProcesses(t *testing.T) {
	output := `
  101 /opt/Antigravity/language_server --app_data_dir antigravity --csrf_token abc --extension_server_port=9191 --extension_server_csrf_token ext
  202 /home/test/.local/bin/agy
  303 /other/language_server --csrf_token wrong
`
	got := parseAntigravityProcesses(output)
	if len(got) != 2 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	if got[0].PID != 101 || got[0].CSRFToken != "abc" || got[0].ExtensionPort != 9191 || got[0].ExtensionServerCSRFToken != "ext" {
		t.Fatalf("app=%+v", got[0])
	}
	if got[1].PID != 202 || got[1].RequiresCSRF {
		t.Fatalf("cli=%+v", got[1])
	}
}

func TestParseAntigravityQuotaSummaryChoosesMostConstrainedPerWindow(t *testing.T) {
	data := []byte(`{
  "response": {"groups": [
    {"displayName":"Gemini Models","buckets":[
      {"bucketId":"gemini-session","displayName":"5 hour limit","remaining":{"remainingFraction":0.8},"resetTime":"2030-01-01T00:00:00Z"},
      {"bucketId":"gemini-weekly","displayName":"Weekly limit","remaining":{"remainingFraction":0.6}}
    ]},
    {"displayName":"Claude and GPT models","buckets":[
      {"bucketId":"claude-session","displayName":"5-hour limit","remainingFraction":0.3},
      {"bucketId":"claude-weekly","displayName":"Weekly limit","remainingFraction":0.9}
    ]}
  ]}
}`)
	got, err := parseAntigravityQuotaSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.primary == nil || got.primary.UsedPercentage != 70 || got.primary.WindowMinutes == nil || *got.primary.WindowMinutes != 300 {
		t.Fatalf("primary=%+v", got.primary)
	}
	if got.secondary == nil || got.secondary.UsedPercentage != 40 || got.secondary.WindowMinutes == nil || *got.secondary.WindowMinutes != 10080 {
		t.Fatalf("secondary=%+v", got.secondary)
	}
}

func TestCollectAntigravityLimitsUsesLoopbackAPIAndCSRF(t *testing.T) {
	var quotaCSRF string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case antigravityQuotaSummaryPath:
			quotaCSRF = r.Header.Get("X-Codeium-Csrf-Token")
			return jsonResponse(`{"response":{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-session","displayName":"5 hour limit","remaining":{"remainingFraction":0.25}}]}]}}`), nil
		case antigravityUserStatusPath:
			var body strings.Builder
			_ = json.NewEncoder(&body).Encode(map[string]any{"userStatus": map[string]any{
				"email":    "dev@example.com",
				"userTier": map[string]any{"name": "Pro"},
			}})
			return jsonResponse(body.String()), nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
			}, nil
		}
	})}

	got := CollectAntigravityLimits(nil, 99, CollectAntigravityLimitsOptions{
		ProcessList: func() (string, error) {
			return "123 /opt/antigravity/language_server --csrf_token csrf-123 --app_data_dir antigravity", nil
		},
		ListeningPorts: func(pid int) ([]int, error) {
			if pid != 123 {
				t.Fatalf("pid=%d", pid)
			}
			return []int{8443}, nil
		},
		HTTPClient: client,
	})
	if quotaCSRF != "csrf-123" {
		t.Fatalf("csrf=%q", quotaCSRF)
	}
	if got.Primary == nil || got.Primary.UsedPercentage != 75 {
		t.Fatalf("got=%+v", got)
	}
	if got.PlanType == nil || *got.PlanType != "Pro" {
		t.Fatalf("plan=%v", got.PlanType)
	}
	if got.Note == nil || !strings.Contains(*got.Note, "dev@example.com") {
		t.Fatalf("note=%v", got.Note)
	}
}

func TestCollectAntigravityLimitsWhenNotRunning(t *testing.T) {
	got := CollectAntigravityLimits(nil, 1, CollectAntigravityLimitsOptions{
		ProcessList: func() (string, error) { return "1 /sbin/init", nil },
	})
	if got.Note == nil || !strings.Contains(*got.Note, "start Antigravity") {
		t.Fatalf("note=%v", got.Note)
	}
}
