/**
 * Reports supported harness releases newer than the versions whose local
 * storage and billing contracts this repository has explicitly verified.
 *
 * A version change is an audit signal, not proof of breakage. The report names
 * every external contract that must be rechecked before advancing the tested
 * baseline. Fixture tests cover known shapes; this checker prevents a new
 * upstream release from passing unnoticed.
 *
 * Exit codes (consumed by .github/workflows/contract-drift.yml):
 *   0  every latest npm release matches its tested baseline
 *   1  checker failure (network, manifest, or registry parse error)
 *   2  one or more upstream versions differ from the tested baseline
 */
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

//go:embed contracts.json
var contractsJSON []byte

const defaultRegistryURL = "https://registry.npmjs.org"

type upstreamContract struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Package       string   `json:"package"`
	TestedVersion string   `json:"testedVersion"`
	Contracts     []string `json:"contracts"`
}

type drift struct {
	Contract upstreamContract
	Latest   string
}

func main() {
	var contracts []upstreamContract
	if err := json.Unmarshal(contractsJSON, &contracts); err != nil {
		fmt.Fprintf(os.Stderr, "contract-drift: invalid manifest: %v\n", err)
		os.Exit(1)
	}
	registryURL := strings.TrimRight(os.Getenv("NPM_REGISTRY_URL"), "/")
	if registryURL == "" {
		registryURL = defaultRegistryURL
	}
	drifts, err := check(&http.Client{Timeout: 30 * time.Second}, registryURL, contracts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contract-drift: checker failed: %v\n", err)
		os.Exit(1)
	}
	if len(drifts) == 0 {
		fmt.Println("No upstream contract drift: all supported harness releases match their tested baselines.")
		return
	}
	fmt.Print(report(drifts))
	os.Exit(2)
}

func check(client *http.Client, registryURL string, contracts []upstreamContract) ([]drift, error) {
	seen := make(map[string]bool, len(contracts))
	var drifts []drift
	for _, contract := range contracts {
		if contract.ID == "" || contract.Label == "" || contract.Package == "" || contract.TestedVersion == "" || len(contract.Contracts) == 0 {
			return nil, fmt.Errorf("incomplete contract manifest entry: %#v", contract)
		}
		if seen[contract.ID] {
			return nil, fmt.Errorf("duplicate contract id %q", contract.ID)
		}
		seen[contract.ID] = true
		latest, err := fetchLatest(client, registryURL, contract.Package)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", contract.ID, err)
		}
		if latest != contract.TestedVersion {
			drifts = append(drifts, drift{Contract: contract, Latest: latest})
		}
	}
	sort.Slice(drifts, func(i, j int) bool { return drifts[i].Contract.ID < drifts[j].Contract.ID })
	return drifts, nil
}

func fetchLatest(client *http.Client, registryURL, packageName string) (string, error) {
	endpoint := registryURL + "/" + url.PathEscape(packageName) + "/latest"
	resp, err := client.Get(endpoint)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", endpoint, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse %s: %w", endpoint, err)
	}
	if payload.Version == "" {
		return "", fmt.Errorf("%s returned no version", endpoint)
	}
	return payload.Version, nil
}

func report(drifts []drift) string {
	var b strings.Builder
	b.WriteString("## Upstream harness contract drift detected\n\n")
	b.WriteString("A newer (or otherwise different) official harness release exists than the version whose local storage and billing contracts this repository has verified. A version difference is an audit signal, not proof that Agent Usage is broken.\n\n")
	for _, d := range drifts {
		fmt.Fprintf(&b, "### %s (`%s`)\n\n", d.Contract.Label, d.Contract.Package)
		fmt.Fprintf(&b, "- Tested: `%s`\n- Latest: `%s`\n- Contracts to recheck:\n", d.Contract.TestedVersion, d.Latest)
		for _, contract := range d.Contract.Contracts {
			fmt.Fprintf(&b, "  - %s\n", contract)
		}
		b.WriteString("\n")
	}
	b.WriteString("### Required follow-up\n\n")
	b.WriteString("- Run the repository contract tests against sanitized artifacts produced by each new harness version.\n")
	b.WriteString("- Verify subscription/API routing and every limit window on a real authenticated pane where public fixtures cannot cover the provider response.\n")
	b.WriteString("- Update parser fixtures and implementation if shapes changed; otherwise advance `testedVersion` in `scripts/contractdrift/contracts.json`.\n\n")
	b.WriteString("---\n_Automatically opened or updated by the weekly upstream-contract drift workflow._\n")
	return b.String()
}
