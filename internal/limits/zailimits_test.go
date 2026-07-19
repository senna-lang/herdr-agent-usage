package limits

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestCollectZaiLimitsFetchesAndOrdersWindows(t *testing.T) {
	var authorization string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		authorization = r.Header.Get("Authorization")
		return jsonResponse(`{
  "code": 200,
  "success": true,
  "data": {
    "planName": "Coding Pro",
    "limits": [
      {"type":"TOKENS_LIMIT","unit":6,"number":1,"usage":1000,"currentValue":200,"remaining":800,"percentage":99,"nextResetTime":2000000000000},
      {"type":"TIME_LIMIT","unit":5,"number":1,"usage":100,"currentValue":40,"remaining":60,"percentage":40},
      {"type":"TOKENS_LIMIT","unit":3,"number":5,"usage":200,"currentValue":100,"remaining":100,"percentage":50}
    ]
  }
}`), nil
	})}

	got := CollectZaiLimits(nil, 123, CollectZaiLimitsOptions{
		Environment: map[string]string{
			"Z_AI_API_KEY":   "secret-token",
			"Z_AI_QUOTA_URL": "https://quota.test/quota",
		},
		HTTPClient: client,
	})
	if authorization != "Bearer secret-token" {
		t.Fatalf("authorization=%q", authorization)
	}
	if got.Primary == nil || got.Primary.WindowMinutes == nil || *got.Primary.WindowMinutes != 300 {
		t.Fatalf("primary=%+v", got.Primary)
	}
	if got.Primary.UsedPercentage != 50 {
		t.Fatalf("primary used=%v want 50", got.Primary.UsedPercentage)
	}
	if got.Secondary == nil || got.Secondary.WindowMinutes == nil || *got.Secondary.WindowMinutes != 10080 {
		t.Fatalf("secondary=%+v", got.Secondary)
	}
	if got.Secondary.UsedPercentage != 20 {
		t.Fatalf("secondary used=%v want 20", got.Secondary.UsedPercentage)
	}
	if got.Tertiary == nil || got.Tertiary.WindowMinutes == nil || *got.Tertiary.WindowMinutes != 43200 {
		t.Fatalf("tertiary=%+v", got.Tertiary)
	}
	if got.PlanType == nil || *got.PlanType != "Coding Pro" {
		t.Fatalf("plan=%v", got.PlanType)
	}
}

func TestCollectZaiLimitsTeamHeaders(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("type") != "2" {
			t.Errorf("type=%q", r.URL.Query().Get("type"))
		}
		if r.Header.Get("Bigmodel-Organization") != "org-1" || r.Header.Get("Bigmodel-Project") != "project-1" {
			t.Errorf("team headers=%q/%q", r.Header.Get("Bigmodel-Organization"), r.Header.Get("Bigmodel-Project"))
		}
		return jsonResponse(`{"code":200,"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":10}]}}`), nil
	})}

	got := CollectZaiLimits(nil, 1, CollectZaiLimitsOptions{
		Environment: map[string]string{
			"Z_AI_API_KEY":               "token",
			"Z_AI_QUOTA_URL":             "https://quota.test/quota",
			"Z_AI_USAGE_SCOPE":           "team",
			"Z_AI_BIGMODEL_ORGANIZATION": "org-1",
			"Z_AI_BIGMODEL_PROJECT":      "project-1",
		},
		HTTPClient: client,
	})
	if got.Primary == nil {
		t.Fatalf("got=%+v", got)
	}
}

func TestResolveZaiQuotaURLRejectsHTTPOverride(t *testing.T) {
	_, err := resolveZaiQuotaURL(map[string]string{"Z_AI_QUOTA_URL": "http://attacker.test/quota"})
	if err == nil {
		t.Fatal("expected insecure endpoint error")
	}
}

func TestCollectZaiLimitsRequiresToken(t *testing.T) {
	got := CollectZaiLimits(nil, 1, CollectZaiLimitsOptions{Environment: map[string]string{}})
	if got.Note == nil || !containsStr(*got.Note, "Z_AI_API_KEY") {
		t.Fatalf("note=%v", got.Note)
	}
}
