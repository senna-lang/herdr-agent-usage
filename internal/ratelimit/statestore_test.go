/**
 * Tests for WithLockedState / AcquireLock — focused on not destroying another process's lock.
 */
package ratelimit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withTempStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USAGEBAR_STATE_DIR", dir)
	return dir
}

func TestAcquireLock_Fresh(t *testing.T) {
	dir := withTempStateDir(t)
	if !AcquireLock() {
		t.Fatal("expected lock")
	}
	if _, err := os.Stat(filepath.Join(dir, "rate-limit-state.lock")); err != nil {
		t.Fatal(err)
	}
	ReleaseLock()
}

func TestAcquireLock_DoesNotDestroyFresh(t *testing.T) {
	dir := withTempStateDir(t)
	lockPath := filepath.Join(dir, "rate-limit-state.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = os.Chtimes(lockPath, now, now)
	if AcquireLock() {
		t.Fatal("should fail to acquire")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatal("lock should remain")
	}
}

func TestWithLockedState_Writes(t *testing.T) {
	dir := withTempStateDir(t)
	b50 := Bucket50
	next := WithLockedState(func(current ClaudeNotifyState) ClaudeNotifyState {
		return ClaudeNotifyState{
			FiveHour: &WindowState{ResetsAt: 100, NotifiedBucket: &b50, FailedNotifyAttempts: 0},
			SevenDay: nil,
		}
	})
	if next.FiveHour == nil || next.FiveHour.NotifiedBucket == nil || *next.FiveHour.NotifiedBucket != Bucket50 {
		t.Fatalf("%+v", next)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "rate-limit-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	_ = json.Unmarshal(raw, &onDisk)
	five := onDisk["fiveHour"].(map[string]any)
	if five["notifiedBucket"] != "50" {
		t.Fatalf("%v", five)
	}
	if _, err := os.Stat(filepath.Join(dir, "rate-limit-state.lock")); !os.IsNotExist(err) {
		t.Fatal("lock should be released")
	}
}

func TestWithLockedState_NoWriteWhenLocked(t *testing.T) {
	dir := withTempStateDir(t)
	statePath := filepath.Join(dir, "rate-limit-state.json")
	existing := []byte(`{"fiveHour":{"resetsAt":1,"notifiedBucket":"20","failedNotifyAttempts":0},"sevenDay":null}`)
	if err := os.WriteFile(statePath, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "rate-limit-state.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = os.Chtimes(lockPath, now, now)

	b5 := Bucket5
	next := WithLockedState(func(current ClaudeNotifyState) ClaudeNotifyState {
		return ClaudeNotifyState{
			FiveHour: &WindowState{ResetsAt: 999, NotifiedBucket: &b5},
		}
	})
	if next.FiveHour == nil || *next.FiveHour.NotifiedBucket != Bucket5 {
		t.Fatalf("update still runs: %+v", next)
	}
	onDisk, _ := os.ReadFile(statePath)
	var parsed map[string]any
	_ = json.Unmarshal(onDisk, &parsed)
	five := parsed["fiveHour"].(map[string]any)
	if five["notifiedBucket"] != "20" {
		t.Fatalf("disk should keep 20, got %v", five)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatal("other lock must remain")
	}
}
