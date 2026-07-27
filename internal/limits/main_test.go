/**
 * Package-wide test defaults.
 */
package limits

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the window pool at a path that cannot exist.
//
// Collectors now fall back to another agent's observations, so without this a
// developer's real ~/.omp/agent/agent.db would leak into every "no data"
// assertion and the suite would pass or fail depending on whose machine ran
// it. A test that wants observations overrides the variable with t.Setenv.
func TestMain(m *testing.M) {
	_ = os.Setenv("USAGEBAR_OMP_AGENT_DB", filepath.Join(os.TempDir(), "usagebar-absent-agent.db"))
	os.Exit(m.Run())
}
