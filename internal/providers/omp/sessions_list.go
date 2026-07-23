/**
 * Lists OMP / Pi session jsonl files for panel spend aggregation.
 */
package omp

import (
	"os"
	"path/filepath"
	"strings"
)

// ListSessionJSONLUnder walks root/*/*.jsonl (one project dir deep).
func ListSessionJSONLUnder(root string) []string {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			out = append(out, filepath.Join(dir, f.Name()))
		}
	}
	return out
}

// ListAllOMPSessionFiles returns every session jsonl under the OMP sessions root.
func ListAllOMPSessionFiles() []string {
	return ListSessionJSONLUnder(ompSessionsRoot())
}

// ListAllPiSessionFiles returns every session jsonl under the Pi sessions root.
func ListAllPiSessionFiles() []string {
	return ListSessionJSONLUnder(piSessionsRoot())
}
