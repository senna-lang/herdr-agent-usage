package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/senna-lang/herdr-agent-usage/releases/latest"
	releasePageURL   = "https://github.com/senna-lang/herdr-agent-usage/releases/latest"
)

// Release is the subset of a GitHub Release needed for the check.
type Release struct {
	TagName string
	HTMLURL string
}

// FetchLatestFunc obtains the most recent stable release.
type FetchLatestFunc func(context.Context) (Release, error)

// NotifyFunc displays a toast and reports whether it was delivered.
type NotifyFunc func(title, body string) bool

// Options controls one automatic or user-requested check.
type Options struct {
	CurrentVersion string
	StateDir       string
	Force          bool
	Now            time.Time
	FetchLatest    FetchLatestFunc
	Notify         NotifyFunc
}

// Result describes the check without exposing an error to automatic callers.
type Result struct {
	Checked      bool
	Update       bool
	Current      string
	Latest       string
	ReleaseURL   string
	Notification bool
	Err          error
}

// Run checks GitHub at most once per CheckInterval unless Force is set.
// Network and state errors are returned in Result so event hooks can fail soft.
func Run(options Options) Result {
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	current, err := ParseVersion(options.CurrentVersion)
	if err != nil {
		return Result{Err: fmt.Errorf("current plugin version: %w", err)}
	}
	result := Result{Current: current.String()}
	releaseLock, locked := acquireLock(options.StateDir, now)
	if !locked {
		return result
	}
	defer releaseLock()

	state := readState(options.StateDir)
	if !shouldCheck(state, now, options.Force) {
		return result
	}
	result.Checked = true
	fetch := options.FetchLatest
	if fetch == nil {
		fetch = FetchLatestRelease
	}
	release, err := fetch(context.Background())
	if err != nil {
		result.Err = err
		return result
	}
	latest, err := ParseVersion(release.TagName)
	if err != nil {
		result.Err = fmt.Errorf("latest release: %w", err)
		return result
	}
	state.LastCheckedAt = now
	if err := writeState(options.StateDir, state); err != nil {
		result.Err = err
		return result
	}

	result.Latest = latest.String()
	result.ReleaseURL = release.HTMLURL
	if result.ReleaseURL == "" {
		result.ReleaseURL = releasePageURL
	}
	if CompareVersions(latest, current) <= 0 {
		return result
	}
	result.Update = true
	if state.LatestNotifiedVersion == latest.String() || options.Notify == nil {
		return result
	}
	if options.Notify("Agent Usage update available", latest.String()+" is available. Open "+result.ReleaseURL) {
		state.LatestNotifiedVersion = latest.String()
		if err := writeState(options.StateDir, state); err != nil {
			result.Err = err
			return result
		}
		result.Notification = true
	}
	return result
}

// FetchLatestRelease requests the stable latest release from GitHub.
func FetchLatestRelease(ctx context.Context) (Release, error) {
	return fetchLatestRelease(ctx, http.DefaultClient, latestReleaseURL)
}

func fetchLatestRelease(ctx context.Context, client *http.Client, endpoint string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "herdr-agent-usage-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("check GitHub Releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return Release{}, fmt.Errorf("check GitHub Releases: %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if strings.TrimSpace(body.TagName) == "" {
		return Release{}, fmt.Errorf("GitHub release has no tag name")
	}
	return Release{TagName: body.TagName, HTMLURL: body.HTMLURL}, nil
}
