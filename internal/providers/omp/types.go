/**
 * Usage shape derived from an OMP / pi agent session jsonl.
 */
package omp

// SessionUsage is the latest assistant usage row from an OMP session.
type SessionUsage struct {
	Provider      string
	Model         string
	ContextTokens int
	TotalTokens   int
	CostUSD       float64
}

// UsageEvent is one assistant turn's token/cost contribution.
type UsageEvent struct {
	Provider    string
	Model       string
	TotalTokens int
	CostUSD     float64
	TimestampMs int64
}
