package limitscore

import "testing"

// Status is derived from structured fields only. Every Status value must be
// reachable from some field combination, so a new value without a derivation
// rule fails here.
func TestProviderLimitsStatus_DerivesFromStructuredFields(t *testing.T) {
	age := 42
	window := &LimitWindow{UsedPercentage: 10}
	note := "free text that must never decide the status"
	cases := []struct {
		name string
		pl   ProviderLimits
		want Status
	}{
		{"fresh windows are ok", ProviderLimits{Primary: window, Note: &note}, StatusOK},
		{"old windows are stale", ProviderLimits{Secondary: window, StaleMinutes: &age}, StatusStale},
		{"a login action needs login", ProviderLimits{Action: "grok login"}, StatusNeedsLogin},
		{"a login action outranks stale windows", ProviderLimits{Primary: window, StaleMinutes: &age, Action: "grok login"}, StatusNeedsLogin},
		{"no windows and no fix is unavailable", ProviderLimits{Note: &note}, StatusUnavailable},
		{"no reading yet is no-data", ProviderLimits{Note: &note, NoReadingYet: true}, StatusNoData},
		{"a reading outranks no-reading-yet", ProviderLimits{Primary: window, NoReadingYet: true}, StatusOK},
		{"a tertiary window alone counts", ProviderLimits{Tertiary: window}, StatusOK},
	}
	seen := map[Status]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pl.Status(); got != tc.want {
				t.Fatalf("Status() = %q, want %q", got, tc.want)
			}
		})
		seen[tc.want] = true
	}
	for _, s := range AllStatuses {
		if !seen[s] {
			t.Errorf("status %q has no derivation case", s)
		}
	}
}
