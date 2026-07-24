/**
 * Reports supported harness releases and public provider limit semantics that
 * differ from the contracts this repository has explicitly verified.
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
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed contracts.json
var contractsJSON []byte

//go:embed provider-contracts.json
var providerContractsJSON []byte

var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

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

type publicContract struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	URL          string   `json:"url"`
	RequiredText []string `json:"requiredText"`
	Contracts    []string `json:"contracts"`
}

type publicDrift struct {
	Contract publicContract
	Missing  []string
}

func main() {
	var contracts []upstreamContract
	if err := json.Unmarshal(contractsJSON, &contracts); err != nil {
		fmt.Fprintf(os.Stderr, "contract-drift: invalid manifest: %v\n", err)
		os.Exit(1)
	}
	var publicContracts []publicContract
	if err := json.Unmarshal(providerContractsJSON, &publicContracts); err != nil {
		fmt.Fprintf(os.Stderr, "contract-drift: invalid provider manifest: %v\n", err)
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
	publicDrifts, err := checkPublicContracts(&http.Client{Timeout: 30 * time.Second}, publicContracts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contract-drift: provider checker failed: %v\n", err)
		os.Exit(1)
	}
	if len(drifts) == 0 && len(publicDrifts) == 0 {
		fmt.Println("No upstream contract drift: harness releases and public provider semantics match their tested baselines.")
		return
	}
	fmt.Print(report(drifts, publicDrifts))
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

func checkPublicContracts(client *http.Client, contracts []publicContract) ([]publicDrift, error) {
	seen := make(map[string]bool, len(contracts))
	var drifts []publicDrift
	for _, contract := range contracts {
		if contract.ID == "" || contract.Label == "" || contract.URL == "" || len(contract.RequiredText) == 0 || len(contract.Contracts) == 0 {
			return nil, fmt.Errorf("incomplete provider contract manifest entry: %#v", contract)
		}
		if seen[contract.ID] {
			return nil, fmt.Errorf("duplicate provider contract id %q", contract.ID)
		}
		seen[contract.ID] = true
		body, err := fetchText(client, contract.URL)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", contract.ID, err)
		}
		normalized := normalizeText(body)
		var missing []string
		for _, required := range contract.RequiredText {
			if !strings.Contains(normalized, normalizeText(required)) {
				missing = append(missing, required)
			}
		}
		if len(missing) > 0 {
			drifts = append(drifts, publicDrift{Contract: contract, Missing: missing})
		}
	}
	sort.Slice(drifts, func(i, j int) bool { return drifts[i].Contract.ID < drifts[j].Contract.ID })
	return drifts, nil
}

func fetchText(client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "herdr-agent-usage-contract-drift/1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", endpoint, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func normalizeText(value string) string {
	value = html.UnescapeString(value)
	value = htmlTagRE.ReplaceAllString(value, " ")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func report(drifts []drift, publicDrifts []publicDrift) string {
	var b strings.Builder
	b.WriteString("## Upstream contract drift detected\n\n")
	b.WriteString("A supported harness release or public provider semantic contract differs from the baseline this repository has verified. This is an audit signal, not proof that Agent Usage is broken.\n\n")
	if len(drifts) > 0 {
		b.WriteString("## Harness releases\n\n")
	}
	for _, d := range drifts {
		fmt.Fprintf(&b, "### %s (`%s`)\n\n", d.Contract.Label, d.Contract.Package)
		fmt.Fprintf(&b, "- Tested: `%s`\n- Latest: `%s`\n- Contracts to recheck:\n", d.Contract.TestedVersion, d.Latest)
		for _, contract := range d.Contract.Contracts {
			fmt.Fprintf(&b, "  - %s\n", contract)
		}
		b.WriteString("\n")
	}
	if len(publicDrifts) > 0 {
		b.WriteString("## Public provider semantics\n\n")
	}
	for _, d := range publicDrifts {
		fmt.Fprintf(&b, "### %s\n\n", d.Contract.Label)
		fmt.Fprintf(&b, "- Source: %s\n- Expected text no longer found:\n", d.Contract.URL)
		for _, missing := range d.Missing {
			fmt.Fprintf(&b, "  - `%s`\n", missing)
		}
		b.WriteString("- Contracts to recheck:\n")
		for _, contract := range d.Contract.Contracts {
			fmt.Fprintf(&b, "  - %s\n", contract)
		}
		b.WriteString("\n")
	}
	b.WriteString("### Required follow-up\n\n")
	b.WriteString("- Run the repository contract tests against sanitized artifacts produced by each new harness version.\n")
	b.WriteString("- Compare provider documentation changes with the collector's window units, used/left semantics, reset handling, and subscription/API routing.\n")
	b.WriteString("- Verify subscription/API routing and every limit window on a real authenticated pane where public fixtures cannot cover the provider response.\n")
	b.WriteString("- Update parser fixtures and implementation if shapes changed; otherwise advance the relevant baseline manifest.\n\n")
	b.WriteString("---\n_Automatically opened or updated by the weekly upstream-contract drift workflow._\n")
	return b.String()
}
