/**
 * Resolves the newest session jsonl for a pane cwd when Herdr has not yet
 * reported agent_session.kind=path (common before the pi/omp integration
 * extension is loaded, or right after /reload).
 */
package omp

import (
	"os"
	"path/filepath"
	"strings"
)

// EncodePiSessionDir encodes a cwd the way stock pi does:
//
//	/Users/me/proj -> --Users-me-proj--
func EncodePiSessionDir(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	cleaned := filepath.Clean(cwd)
	if cleaned == string(filepath.Separator) {
		return "----"
	}
	trimmed := strings.TrimPrefix(cleaned, string(filepath.Separator))
	return "--" + strings.ReplaceAll(trimmed, string(filepath.Separator), "-") + "--"
}

// EncodeOMPSessionDir encodes a cwd the way OMP organizes sessions:
// prefer path relative to $HOME (develop/foo -> -develop-foo), else absolute.
func EncodeOMPSessionDir(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	cleaned := filepath.Clean(cwd)
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		home = filepath.Clean(home)
		if cleaned == home {
			return "--" + strings.ReplaceAll(strings.TrimPrefix(cleaned, string(filepath.Separator)), string(filepath.Separator), "-") + "--"
		}
		if rel, err := filepath.Rel(home, cleaned); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return "-" + strings.ReplaceAll(rel, string(filepath.Separator), "-")
		}
	}
	trimmed := strings.TrimPrefix(cleaned, string(filepath.Separator))
	return "-" + strings.ReplaceAll(trimmed, string(filepath.Separator), "-")
}

func piSessionsRoot() string {
	if v := os.Getenv("PI_SESSIONS_ROOT"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

func ompSessionsRoot() string {
	if v := os.Getenv("OMP_SESSIONS_ROOT"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".omp", "agent", "sessions")
}

// FindLatestSessionInDir returns the newest *.jsonl under dir by mtime.
func FindLatestSessionInDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestMod int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mod := info.ModTime().UnixNano()
		if best == "" || mod >= bestMod {
			best = filepath.Join(dir, e.Name())
			bestMod = mod
		}
	}
	return best
}

// FindLatestPiSessionForCwd looks under ~/.pi/agent/sessions/--encoded--/.
func FindLatestPiSessionForCwd(cwd string) string {
	root := piSessionsRoot()
	enc := EncodePiSessionDir(cwd)
	if root == "" || enc == "" {
		return ""
	}
	return FindLatestSessionInDir(filepath.Join(root, enc))
}

// FindLatestOMPSessionForCwd looks under ~/.omp/agent/sessions/<encoded>/.
func FindLatestOMPSessionForCwd(cwd string) string {
	root := ompSessionsRoot()
	enc := EncodeOMPSessionDir(cwd)
	if root == "" || enc == "" {
		return ""
	}
	if path := FindLatestSessionInDir(filepath.Join(root, enc)); path != "" {
		return path
	}
	// Fallback: also try pi-style encoding under the OMP root (some builds).
	return FindLatestSessionInDir(filepath.Join(root, EncodePiSessionDir(cwd)))
}
