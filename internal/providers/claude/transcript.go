/**
 * Reads the latest assistant usage for a given session ID from a Claude Code
 * session transcript (jsonl).
 */
package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
)

const tailScanBytes = 512 * 1024

func projectsRoot() string {
	if v := os.Getenv("CLAUDE_PROJECTS_ROOT"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// findSessionFileIn scans root's project directories for {sessionId}.jsonl.
// The project directory name (encoded cwd) is lossy; UUIDs are globally unique.
func findSessionFileIn(root, sessionID string) string {
	if root == "" {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	fileName := sessionID + ".jsonl"
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(root, e.Name(), fileName)
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

// ResolveTranscriptPathForSession returns the transcript jsonl path under the
// default projects root, or empty when not found.
func ResolveTranscriptPathForSession(sessionID string) string {
	return ResolveTranscriptPathForSessionIn(projectsRoot(), sessionID)
}

// ResolveTranscriptPathForSessionIn is ResolveTranscriptPathForSession scoped
// to an explicit projects root, so a multi-profile caller can search one
// profile's root without touching global env state.
func ResolveTranscriptPathForSessionIn(root, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return findSessionFileIn(root, sessionID)
}

func totalTokensOf(usage TranscriptUsage) int {
	return usage.InputTokens + usage.CacheReadInputTokens +
		usage.CacheCreationInputTokens + usage.OutputTokens
}

// ExtractLatestUsageFromLines walks jsonl lines from the end and returns the
// latest assistant usage row with isSidechain=false and real token counts.
func ExtractLatestUsageFromLines(lines []string) *TranscriptUsage {
	for i := len(lines) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(lines[i])
		if raw == "" {
			continue
		}
		var parsed struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Message     *struct {
				Model string `json:"model"`
				Usage *struct {
					InputTokens              *float64 `json:"input_tokens"`
					CacheReadInputTokens     *float64 `json:"cache_read_input_tokens"`
					CacheCreationInputTokens *float64 `json:"cache_creation_input_tokens"`
					OutputTokens             *float64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Type != "assistant" || parsed.IsSidechain {
			continue
		}
		if parsed.Message == nil || parsed.Message.Usage == nil || parsed.Message.Model == "" {
			continue
		}
		u := parsed.Message.Usage
		candidate := TranscriptUsage{
			Model:                    parsed.Message.Model,
			InputTokens:              intOrZero(u.InputTokens),
			CacheReadInputTokens:     intOrZero(u.CacheReadInputTokens),
			CacheCreationInputTokens: intOrZero(u.CacheCreationInputTokens),
			OutputTokens:             intOrZero(u.OutputTokens),
		}
		if totalTokensOf(candidate) == 0 {
			continue
		}
		return &candidate
	}
	return nil
}

func intOrZero(n *float64) int {
	if n == nil {
		return 0
	}
	return int(*n)
}

// ResolveUsageForSession resolves the latest usage for a session ID under the
// default projects root.
func ResolveUsageForSession(sessionID string) *TranscriptUsage {
	return ResolveUsageForSessionIn(projectsRoot(), sessionID)
}

// ResolveUsageForSessionIn is ResolveUsageForSession scoped to an explicit
// projects root.
func ResolveUsageForSessionIn(root, sessionID string) *TranscriptUsage {
	path := findSessionFileIn(root, sessionID)
	if path == "" {
		return nil
	}
	lines, err := fsutil.ReadLastNLines(path, tailScanBytes)
	if err != nil {
		return nil
	}
	return ExtractLatestUsageFromLines(lines)
}

// ResolveProfileForSession scans each profile's ProjectsRoot for the session's
// transcript file. Session UUIDs are globally unique, so at most one profile
// should match; returns ok=false when none do (never guesses).
func ResolveProfileForSession(sessionID string, roots map[string]string) (providerID string, ok bool) {
	if sessionID == "" {
		return "", false
	}
	for id, root := range roots {
		if root == "" {
			continue
		}
		if findSessionFileIn(root, sessionID) != "" {
			return id, true
		}
	}
	return "", false
}
