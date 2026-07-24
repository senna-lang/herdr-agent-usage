package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func registryClient(t *testing.T, versions map[string]string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		name, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(req.URL.EscapedPath(), "/"), "/latest"))
		if err != nil {
			return nil, err
		}
		version, ok := versions[name]
		if !ok {
			return response(http.StatusNotFound, `{"error":"not found"}`), nil
		}
		return response(http.StatusOK, `{"version":"`+version+`"}`), nil
	})}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestCheckCleanAndDrift(t *testing.T) {
	contracts := []upstreamContract{
		{ID: "a", Label: "A", Package: "@scope/a", TestedVersion: "1.0.0", Contracts: []string{"session schema"}},
		{ID: "b", Label: "B", Package: "b", TestedVersion: "2.0.0", Contracts: []string{"limit schema"}},
	}
	client := registryClient(t, map[string]string{"@scope/a": "1.0.0", "b": "2.1.0"})
	drifts, err := check(client, "https://registry.test", contracts)
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 1 || drifts[0].Contract.ID != "b" || drifts[0].Latest != "2.1.0" {
		t.Fatalf("drifts = %#v", drifts)
	}
	out := report(drifts, nil)
	for _, want := range []string{"Upstream contract drift", "Harness releases", "Tested: `2.0.0`", "Latest: `2.1.0`", "limit schema", "real authenticated pane"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCheckPublicContractsCleanAndDrift(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/clean":
			return response(http.StatusOK, "<p>Shared WEEKLY usage&nbsp;pool resets every week</p>"), nil
		case "/drift":
			return response(http.StatusOK, "<p>A new quota system</p>"), nil
		default:
			return response(http.StatusNotFound, "nope"), nil
		}
	})}
	contracts := []publicContract{
		{ID: "a", Label: "A", URL: "https://docs.test/clean", RequiredText: []string{"shared weekly usage pool", "resets every week"}, Contracts: []string{"weekly reset"}},
		{ID: "b", Label: "B", URL: "https://docs.test/drift", RequiredText: []string{"five hour"}, Contracts: []string{"shortest window"}},
	}
	drifts, err := checkPublicContracts(client, contracts)
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 1 || drifts[0].Contract.ID != "b" || len(drifts[0].Missing) != 1 {
		t.Fatalf("drifts = %#v", drifts)
	}
	out := report(nil, drifts)
	for _, want := range []string{"Public provider semantics", "https://docs.test/drift", "`five hour`", "shortest window"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCheckPublicContractsRejectsManifestAndHTTPError(t *testing.T) {
	if _, err := checkPublicContracts(&http.Client{}, []publicContract{{ID: "a"}}); err == nil {
		t.Fatal("expected incomplete manifest error")
	}
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(http.StatusBadGateway, "nope"), nil
	})}
	contracts := []publicContract{{ID: "a", Label: "A", URL: "https://docs.test/a", RequiredText: []string{"x"}, Contracts: []string{"y"}}}
	if _, err := checkPublicContracts(client, contracts); err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("expected provider status error, got %v", err)
	}
}

func TestCheckRejectsIncompleteAndDuplicateManifest(t *testing.T) {
	client := &http.Client{}
	if _, err := check(client, "http://unused", []upstreamContract{{ID: "a"}}); err == nil {
		t.Fatal("expected incomplete manifest error")
	}
	contracts := []upstreamContract{
		{ID: "a", Label: "A", Package: "a", TestedVersion: "1", Contracts: []string{"x"}},
		{ID: "a", Label: "B", Package: "b", TestedVersion: "1", Contracts: []string{"y"}},
	}
	client = registryClient(t, map[string]string{"a": "1"})
	if _, err := check(client, "https://registry.test", contracts); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestCheckFailsOnRegistryError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(http.StatusBadGateway, "nope"), nil
	})}
	contracts := []upstreamContract{{ID: "a", Label: "A", Package: "a", TestedVersion: "1", Contracts: []string{"x"}}}
	if _, err := check(client, "https://registry.test", contracts); err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("expected registry status error, got %v", err)
	}
}
