package update

import "testing"

func TestVerifyBandPublicationSingle(t *testing.T) {
	snapshot, tokens := map[string]string{"limit_high": "5h 94%"}, map[string]string{"limit_high": "5h 94%"}
	writer := metadataTokenWriter{
		set:   func(_, _, name, value string) bool { tokens[name] = value; return true },
		clear: func(_, _, name string) bool { delete(tokens, name); return true },
	}
	writeLimitBandTokensWith(writer, snapshot, "pane", "5h 20%", 20, false, false)
	if len(tokens) != 1 || tokens["limit_low"] != "5h 20%" {
		t.Fatalf("expected only the new critical row: %v", tokens)
	}
}
