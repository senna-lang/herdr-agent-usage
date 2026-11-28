package update

import (
	"maps"
	"testing"
)

func TestVerifyBandPublicationConcurrent(t *testing.T) {
	var snapshot map[string]string
	tokens := map[string]string{}
	writer := metadataTokenWriter{
		set: func(_, _, name, value string) bool {
			// The other publisher snapshots the pane while this request is in flight.
			if name == "limit_low" {
				snapshot = maps.Clone(tokens)
			}
			tokens[name] = value
			return true
		},
		clear: func(_, _, name string) bool { delete(tokens, name); return true },
	}
	writeLimitBandTokensWith(writer, map[string]string{}, "pane", "5h 20%", 20, false, false)
	writeLimitBandTokensWith(writer, snapshot, "pane", "5h 80%", 80, false, false)
	if len(tokens) != 1 || (tokens["limit_high"] != "5h 80%" && tokens["limit_low"] != "5h 20%") {
		t.Fatalf("expected exactly one publisher's complete row after both finished: %v", tokens)
	}
}
