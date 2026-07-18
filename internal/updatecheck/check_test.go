package updatecheck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseVersion(t *testing.T) {
	got, err := ParseVersion("v1.2.3")
	if err != nil || got.String() != "v1.2.3" {
		t.Fatalf("ParseVersion = %#v, %v", got, err)
	}
	for _, raw := range []string{"v1.2", "v01.2.3", "v1.2.3-dev", ""} {
		if _, err := ParseVersion(raw); err == nil {
			t.Fatalf("ParseVersion(%q) succeeded", raw)
		}
	}
}

func TestRunSkipsFreshAutomaticCheck(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if err := writeState(dir, State{LastCheckedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	called := false
	result := Run(Options{CurrentVersion: "0.1.1", StateDir: dir, Now: now, FetchLatest: func(context.Context) (Release, error) {
		called = true
		return Release{}, nil
	}})
	if result.Checked || called || result.Err != nil {
		t.Fatalf("result=%+v called=%v", result, called)
	}
}

func TestRunNotifiesOnlyOncePerRelease(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	fetch := func(context.Context) (Release, error) {
		return Release{TagName: "v0.2.0", HTMLURL: "https://example.test/v0.2.0"}, nil
	}
	notifications := 0
	first := Run(Options{CurrentVersion: "0.1.1", StateDir: dir, Now: now, FetchLatest: fetch, Notify: func(_, _ string) bool { notifications++; return true }})
	if !first.Checked || !first.Update || !first.Notification || notifications != 1 {
		t.Fatalf("first=%+v notifications=%d", first, notifications)
	}
	second := Run(Options{CurrentVersion: "0.1.1", StateDir: dir, Force: true, Now: now.Add(time.Minute), FetchLatest: fetch, Notify: func(_, _ string) bool { notifications++; return true }})
	if !second.Update || second.Notification || notifications != 1 {
		t.Fatalf("second=%+v notifications=%d", second, notifications)
	}
}

func TestRunRetriesNotificationWhenToastIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	fetch := func(context.Context) (Release, error) { return Release{TagName: "v0.2.0"}, nil }
	first := Run(Options{CurrentVersion: "0.1.1", StateDir: dir, Now: now, FetchLatest: fetch, Notify: func(_, _ string) bool { return false }})
	if first.Notification {
		t.Fatalf("unexpected notification: %+v", first)
	}
	second := Run(Options{CurrentVersion: "0.1.1", StateDir: dir, Force: true, Now: now.Add(time.Minute), FetchLatest: fetch, Notify: func(_, _ string) bool { return true }})
	if !second.Notification {
		t.Fatalf("notification was not retried: %+v", second)
	}
}

func TestRunDoesNotPersistFailedCheck(t *testing.T) {
	dir := t.TempDir()
	result := Run(Options{CurrentVersion: "0.1.1", StateDir: dir, Now: time.Now(), FetchLatest: func(context.Context) (Release, error) {
		return Release{}, errors.New("offline")
	}})
	if result.Err == nil || !readState(dir).LastCheckedAt.IsZero() {
		t.Fatalf("result=%+v state=%+v", result, readState(dir))
	}
}

func TestStatePathUsesPluginConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	if got, want := statePath(dir), filepath.Join(dir, stateFileName); got != want {
		t.Fatalf("statePath=%q want %q", got, want)
	}
}

func TestFetchLatestRelease(t *testing.T) {
	client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("Accept=%q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "herdr-agent-usage-update-check" {
			t.Fatalf("User-Agent=%q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.2.0","html_url":"https://example.test/v0.2.0"}`)), Header: make(http.Header)}, nil
	})}

	got, err := fetchLatestRelease(context.Background(), client, "https://example.test/latest")
	if err != nil || got.TagName != "v0.2.0" || got.HTMLURL != "https://example.test/v0.2.0" {
		t.Fatalf("fetchLatestRelease = %#v, %v", got, err)
	}
}

func TestFetchLatestReleaseRejectsUnexpectedStatus(t *testing.T) {
	client := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("forbidden")), Header: make(http.Header)}, nil
	})}
	if _, err := fetchLatestRelease(context.Background(), client, "https://example.test/latest"); err == nil {
		t.Fatal("fetchLatestRelease succeeded")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }
