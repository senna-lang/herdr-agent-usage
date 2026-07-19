/**
 * Tests for sidebar metadata write routing and retry behavior.
 */
package update

import "testing"

func TestWriteMetadataTokenWith_SetSuccessDeduplicates(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	setCalls := 0
	clearCalls := 0
	writer := metadataTokenWriter{
		set: func(_, _, _, _ string) bool {
			setCalls++
			return true
		},
		clear: func(_, _, _ string) bool {
			clearCalls++
			return true
		},
	}

	writeMetadataTokenWith(writer, "w1:p1", "limit", "5h 72%", false)
	writeMetadataTokenWith(writer, "w1:p1", "limit", "5h 72%", false)
	if setCalls != 1 || clearCalls != 0 {
		t.Fatalf("set=%d clear=%d", setCalls, clearCalls)
	}
}

func TestWriteMetadataTokenWith_ClearSuccessDeduplicates(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	setCalls := 0
	clearCalls := 0
	writer := metadataTokenWriter{
		set: func(_, _, _, _ string) bool {
			setCalls++
			return true
		},
		clear: func(_, _, _ string) bool {
			clearCalls++
			return true
		},
	}

	writeMetadataTokenWith(writer, "w1:p1", "context", "", false)
	writeMetadataTokenWith(writer, "w1:p1", "context", "", false)
	if setCalls != 0 || clearCalls != 1 {
		t.Fatalf("set=%d clear=%d", setCalls, clearCalls)
	}
}

func TestWriteMetadataTokenWith_FailureRetries(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	setCalls := 0
	writer := metadataTokenWriter{
		set: func(_, _, _, _ string) bool {
			setCalls++
			return false
		},
		clear: func(_, _, _ string) bool { return false },
	}

	writeMetadataTokenWith(writer, "w1:p1", "limit", "7d 42%", false)
	writeMetadataTokenWith(writer, "w1:p1", "limit", "7d 42%", false)
	if setCalls != 2 {
		t.Fatalf("set=%d want 2 retries", setCalls)
	}
}
