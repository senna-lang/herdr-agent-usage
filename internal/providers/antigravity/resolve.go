/**
 * Antigravity context resolution.
 *
 * Resolution is deliberately conservative: it prefers an exact session match,
 * falls back to pane identity, and returns nothing whenever the best
 * available match is stale or cannot be attributed to a single session.
 * Showing another session's context would be worse than showing none, so
 * every uncertain case resolves to nil rather than to a plausible-looking
 * number.
 *
 * The pane fallback exists because Antigravity mints a new conversation id
 * when a session is cleared or resumed into a new conversation while herdr
 * keeps reporting the agent_session it observed at launch, leaving the two
 * identifiers disagreeing for the same live pane — the same failure mode
 * Cursor's provider documents and handles the same way. Pane identity, unlike
 * cwd, still distinguishes two Antigravity panes opened in one repository.
 */
package antigravity

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// SnapshotFreshnessMs bounds how long a snapshot may be trusted.
//
// Snapshots are refreshed on every conversation update, so an active session
// stays far inside this bound and a resumed one refreshes immediately. The
// bound exists for the cases where no teardown ran at all: a pane whose agent
// was killed, or a session abandoned mid-conversation.
const SnapshotFreshnessMs int64 = 4 * 60 * 60 * 1000

// ResolveUsageIn resolves context usage for one pane from the snapshots under
// sessionsDir. Every argument except sessionsDir may be absent.
func ResolveUsageIn(sessionsDir string, sessionID, paneID, cwd *string, nowMs int64) *core.ContextUsage {
	if snap, ok := resolveBySession(sessionsDir, sessionID, paneID, nowMs); ok {
		return toContextUsage(snap)
	}
	if snap, ok := resolveByPane(sessionsDir, paneID, cwd, nowMs); ok {
		return toContextUsage(snap)
	}
	return nil
}

// resolveBySession takes the snapshot whose conversation id herdr reported,
// when one exists, is still fresh, and has not been superseded on its own
// pane. Any other outcome falls through to the pane fallback rather than
// failing outright.
func resolveBySession(sessionsDir string, sessionID, paneID *string, nowMs int64) (Snapshot, bool) {
	if sessionID == nil || *sessionID == "" {
		return Snapshot{}, false
	}
	snap, err := ReadSnapshot(sessionsDir, *sessionID)
	if err != nil {
		return Snapshot{}, false
	}
	if !isFresh(snap, nowMs) {
		return Snapshot{}, false
	}
	if supersededOnPane(sessionsDir, snap, paneID, nowMs) {
		return Snapshot{}, false
	}
	return snap, true
}

// supersededOnPane reports whether a newer fresh snapshot claims the same pane.
//
// herdr keeps reporting the agent_session captured when the pane launched,
// but clearing or resuming a conversation makes Antigravity mint a new
// conversation id and write every subsequent update under it. The reported
// session's snapshot is then recent enough to look valid while describing a
// conversation that no longer exists, so recency on the pane, not the
// reported identity, decides.
func supersededOnPane(sessionsDir string, snap Snapshot, paneID *string, nowMs int64) bool {
	// Without a pane id there is nothing to compare against, so the reported
	// session remains the best available answer.
	if paneID == nil || *paneID == "" || snap.PaneID == "" {
		return false
	}
	for _, other := range ListSnapshots(sessionsDir) {
		if other.SessionID == snap.SessionID || other.PaneID != *paneID {
			continue
		}
		if isFresh(other, nowMs) && other.UpdatedAtMs > snap.UpdatedAtMs {
			return true
		}
	}
	return false
}

// resolveByPane takes the newest fresh snapshot claiming this pane, provided
// a single session owns that moment. Candidates are additionally required to
// agree with the pane's working directory when both are known, so a snapshot
// left behind by a pane id that herdr has since reused cannot be adopted.
func resolveByPane(sessionsDir string, paneID, cwd *string, nowMs int64) (Snapshot, bool) {
	if paneID == nil || *paneID == "" {
		return Snapshot{}, false
	}
	var candidates []Snapshot
	for _, snap := range ListSnapshots(sessionsDir) {
		if snap.PaneID != *paneID || !isFresh(snap, nowMs) {
			continue
		}
		if !cwdConsistent(snap, cwd) {
			continue
		}
		candidates = append(candidates, snap)
	}
	return newestUnambiguous(candidates)
}

// newestUnambiguous returns the single newest candidate. Two distinct
// sessions sharing the newest timestamp are ambiguous: there is no basis to
// prefer either, so neither is shown.
func newestUnambiguous(candidates []Snapshot) (Snapshot, bool) {
	if len(candidates) == 0 {
		return Snapshot{}, false
	}
	newest := candidates[0]
	tied := false
	for _, snap := range candidates[1:] {
		switch {
		case snap.UpdatedAtMs > newest.UpdatedAtMs:
			newest, tied = snap, false
		case snap.UpdatedAtMs == newest.UpdatedAtMs && snap.SessionID != newest.SessionID:
			tied = true
		}
	}
	if tied {
		return Snapshot{}, false
	}
	return newest, true
}

// cwdConsistent reports whether a candidate may be attributed to this pane.
// An unknown value on either side cannot contradict the other, so it passes.
func cwdConsistent(snap Snapshot, cwd *string) bool {
	if cwd == nil || *cwd == "" || snap.Cwd == "" {
		return true
	}
	return snap.Cwd == *cwd
}

func isFresh(snap Snapshot, nowMs int64) bool {
	age := nowMs - snap.UpdatedAtMs
	return age >= 0 && age <= SnapshotFreshnessMs
}

// LatestFreshSnapshot returns the most recently updated fresh snapshot across
// every session under sessionsDir. Quota and plan tier are account-wide, not
// session-scoped, so unlike context resolution this deliberately ignores pane
// and session identity: whichever pane most recently observed the account's
// usage speaks for the whole account, and snapshots taken close together are
// expected to agree rather than conflict.
func LatestFreshSnapshot(sessionsDir string, nowMs int64) (Snapshot, bool) {
	var latest Snapshot
	found := false
	for _, snap := range ListSnapshots(sessionsDir) {
		if !isFresh(snap, nowMs) {
			continue
		}
		if !found || snap.UpdatedAtMs > latest.UpdatedAtMs {
			latest = snap
			found = true
		}
	}
	return latest, found
}

// toContextUsage converts a snapshot into the shared domain type.
func toContextUsage(snap Snapshot) *core.ContextUsage {
	usage := core.ContextUsage{ContextTokens: snap.ContextTokens, Cache: snap.Cache}
	if snap.WindowTokens != nil && *snap.WindowTokens > 0 {
		window := *snap.WindowTokens
		usage.WindowTokens = &window
	}
	return &usage
}

// resolveAntigravityUsage adapts the shared resolve input to this provider.
// herdr's antigravity_cli integration reports agent_session with kind "id"
// (the conversation id), unlike the path-based integrations, so resolution
// keys off provider.SessionID rather than a session path.
func resolveAntigravityUsage(input provider.UsageResolveInput) *core.ContextUsage {
	stateDir := StateDir()
	if stateDir == "" {
		return nil
	}
	return ResolveUsageIn(SessionsDir(stateDir), provider.SessionID(input), input.PaneID, input.Cwd, nowUnixMs())
}
