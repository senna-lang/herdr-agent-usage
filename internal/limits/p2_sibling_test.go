package limits

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers"
)

type p2SiblingFamily struct {
	id  string
	set func(*CollectOptions, []ClaudeProfileCollector)
}

func p2SiblingFamilies(t *testing.T) []p2SiblingFamily {
	t.Helper()
	families := []p2SiblingFamily{
		{claudeFamily, func(o *CollectOptions, p []ClaudeProfileCollector) { o.Claude = p }},
		{codexFamily, func(o *CollectOptions, p []ClaudeProfileCollector) { o.Codex = p }},
		{openCodeFamily, func(o *CollectOptions, p []ClaudeProfileCollector) { o.OpenCode = p }},
		{grokFamily, func(o *CollectOptions, p []ClaudeProfileCollector) { o.Grok = p }},
	}
	var got, want []string
	for _, f := range families {
		got = append(got, f.id)
	}
	for _, id := range providers.IDsWithCapability(providers.CapOwnsSubscriptionQuota) {
		single := false
		for _, s := range singleCollectorQuotaSpecs {
			if s.id == id {
				single = true
			}
		}
		if !single {
			want = append(want, id)
		}
	}
	assertSameIDSet(t, "profile family regression coverage", got, want)
	return families
}

func p2SiblingReading(id, account string, used float64, fetched int64, calls map[string]int) ClaudeProfileCollector {
	return ClaudeProfileCollector{ID: id, Label: id, Collector: func(_ *string, _ int64) ProviderLimits {
		calls[id]++
		return ProviderLimits{ProviderID: id, Label: id, ProfileLabel: id, Account: account,
			Primary: &LimitWindow{UsedPercentage: used}, FetchedAtMs: fetched, Source: id}
	}}
}

func TestP2SiblingNoAccountSkipsUnselected(t *testing.T) {
	for _, family := range p2SiblingFamilies(t) {
		t.Run(family.id, func(t *testing.T) {
			calls := map[string]int{}
			var opts CollectOptions
			family.set(&opts, []ClaudeProfileCollector{
				p2SiblingReading("first", "account", 10, 99, calls),
				p2SiblingReading("selected", "", 80, 90, calls),
				p2SiblingReading("last", "account", 20, 99, calls),
			})
			opts.Only = map[string]bool{"selected": true}
			got := CollectAllProviderLimits(nil, 100, opts)
			if !reflect.DeepEqual(calls, map[string]int{"selected": 1}) {
				t.Errorf("collector calls = %v, want selected once and no siblings", calls)
			}
			if len(got) != 1 || got[0].ProviderID != "selected" || review141Used(got[0]) != 80 || got[0].SameAccountAs != "" {
				t.Errorf("rows = %+v, want selected's local 80%%", got)
			}
		})
	}
}

func TestP2SiblingKnownAccountSharesInConfigOrder(t *testing.T) {
	for _, family := range p2SiblingFamilies(t) {
		t.Run(family.id, func(t *testing.T) {
			calls := map[string]int{}
			var opts CollectOptions
			family.set(&opts, []ClaudeProfileCollector{
				p2SiblingReading("first", "account", 10, 99, calls),
				p2SiblingReading("second", "account", 20, 99, calls),
				p2SiblingReading("selected", "account", 80, 90, calls),
				p2SiblingReading("other-selected", "account", 70, 90, calls),
			})
			opts.Families = map[string]bool{family.id: true}
			full := CollectAllProviderLimits(nil, 100, opts)
			if len(full) != 4 || review141Used(full[2]) != 10 || full[2].Source != "first via first" {
				t.Fatalf("full collection = %+v, want first configured tied sibling to supply 10%%", full)
			}
			for i, id := range []string{"first", "second", "selected", "other-selected"} {
				if full[i].ProviderID != id {
					t.Errorf("full row %d = %q, want %q in configuration order", i, full[i].ProviderID, id)
				}
			}
			clear(calls)
			opts.Only = map[string]bool{"selected": true, "other-selected": true}
			got := CollectAllProviderLimits(nil, 100, opts)
			if !reflect.DeepEqual(calls, map[string]int{"first": 1, "second": 1, "selected": 1, "other-selected": 1}) {
				t.Errorf("collector calls = %v, want every sibling exactly once", calls)
			}
			if len(got) != 2 {
				t.Fatalf("rows = %+v, want two selected rows", got)
			}
			for i, id := range []string{"selected", "other-selected"} {
				if got[i].ProviderID != id || review141Used(got[i]) != review141Used(full[i+2]) || got[i].Source != full[i+2].Source {
					t.Errorf("row %d = %+v, want corresponding full collection row %+v", i, got[i], full[i+2])
				}
			}
			if got[0].SameAccountAs != "" || got[1].SameAccountAs != "selected" {
				t.Errorf("retained anchors = %q, %q", got[0].SameAccountAs, got[1].SameAccountAs)
			}
		})
	}
}

func TestP2SiblingFamiliesExcludeSelected(t *testing.T) {
	for _, family := range p2SiblingFamilies(t) {
		t.Run(family.id, func(t *testing.T) {
			calls := map[string]int{}
			var opts CollectOptions
			family.set(&opts, []ClaudeProfileCollector{
				p2SiblingReading("selected", "account", 80, 90, calls),
				p2SiblingReading("sibling", "account", 10, 99, calls),
			})
			opts.Only = map[string]bool{"selected": true}
			opts.Families = map[string]bool{}
			got := CollectAllProviderLimits(nil, 100, opts)
			if len(calls) != 0 || len(got) != 0 {
				t.Errorf("excluded family: calls = %v, rows = %+v", calls, got)
			}
		})
	}
}

func TestP2SiblingUnknownProvenanceStaysLocal(t *testing.T) {
	review141QuietCollectors(t)
	home := t.TempDir()
	personal := filepath.Join(home, ".claude")
	work := filepath.Join(home, ".claude-work")
	review141ConfigureClaude(t, home, []struct{ id, label, dir string }{
		{"claude", "personal", personal}, {"work", "work", work},
	})
	const email, org = "same@x.com", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	review141WriteJSON(t, review141JSONPath(personal), review141Login(email, org))
	review141WriteJSON(t, review141JSONPath(work), review141Login(email, org))
	if err := WriteClaudeLimitsCache(fiveHourInput(80), review141Now-20*60_000, review141CachePath(personal)); err != nil {
		t.Fatal(err)
	}
	review141WriteJSON(t, review141CachePath(work), review141ProvenanceCache(email, org, 10, review141Now-60_000))
	opts := DefaultCollectOptions()
	calls := map[string]int{}
	for i := range opts.Claude {
		id, collector := opts.Claude[i].ID, opts.Claude[i].Collector
		opts.Claude[i].Collector = func(cwd *string, now int64) ProviderLimits {
			calls[id]++
			return collector(cwd, now)
		}
	}
	opts.Only = map[string]bool{"claude": true}
	got := CollectAllProviderLimits(nil, review141Now, opts)
	if !reflect.DeepEqual(calls, map[string]int{"claude": 1}) {
		t.Errorf("collector calls = %v, want only the unproven selected profile", calls)
	}
	if len(got) != 1 || got[0].Account != "" || got[0].SameAccountAs != "" || review141Used(got[0]) != 80 {
		t.Errorf("rows = %+v, want local unproven 80%%", got)
	}
}

func TestP2SiblingMixedSelectedAccounts(t *testing.T) {
	for _, family := range p2SiblingFamilies(t) {
		t.Run(family.id, func(t *testing.T) {
			calls := map[string]int{}
			var opts CollectOptions
			family.set(&opts, []ClaudeProfileCollector{
				p2SiblingReading("donor", "account", 10, 99, calls),
				p2SiblingReading("unknown", "", 80, 90, calls),
				p2SiblingReading("known", "account", 70, 90, calls),
			})
			opts.Only = map[string]bool{"unknown": true, "known": true}
			got := CollectAllProviderLimits(nil, 100, opts)
			if !reflect.DeepEqual(calls, map[string]int{"donor": 1, "unknown": 1, "known": 1}) {
				t.Errorf("collector calls = %v, want each profile once", calls)
			}
			if len(got) != 2 {
				t.Fatalf("rows = %+v, want unknown then known", got)
			}
			if got[0].ProviderID != "unknown" || got[0].Account != "" || review141Used(got[0]) != 80 || got[0].Source != "unknown" || got[0].SameAccountAs != "" {
				t.Errorf("unknown row = %+v, want its own local value and source", got[0])
			}
			if got[1].ProviderID != "known" || got[1].Account != "account" || review141Used(got[1]) != 10 || got[1].Source != "donor via donor" || got[1].SameAccountAs != "" {
				t.Errorf("known row = %+v, want donor value without excluded anchor", got[1])
			}
		})
	}
}

func TestP2SiblingCompactionBeforeLaterFamily(t *testing.T) {
	families := p2SiblingFamilies(t)
	for i := 0; i+1 < len(families); i++ {
		first, later := families[i], families[i+1]
		t.Run(first.id+"-then-"+later.id, func(t *testing.T) {
			calls := map[string]int{}
			var opts CollectOptions
			first.set(&opts, []ClaudeProfileCollector{
				p2SiblingReading("unused-first", "account", 1, 99, calls),
				p2SiblingReading("unused-second", "account", 2, 99, calls),
				p2SiblingReading("local", "", 80, 90, calls),
			})
			later.set(&opts, []ClaudeProfileCollector{
				p2SiblingReading("donor", "account", 10, 99, calls),
				p2SiblingReading("known", "account", 70, 90, calls),
				p2SiblingReading("known-later", "account", 60, 90, calls),
			})
			opts.Only = map[string]bool{"local": true, "known": true, "known-later": true}
			got := CollectAllProviderLimits(nil, 100, opts)
			if !reflect.DeepEqual(calls, map[string]int{"local": 1, "donor": 1, "known": 1, "known-later": 1}) {
				t.Errorf("collector calls = %v, want selected rows and later donor once", calls)
			}
			if len(got) != 3 {
				t.Fatalf("rows = %+v, want exactly the three selected rows", got)
			}
			if got[0].ProviderID != "local" || got[0].Family != first.id || got[0].Account != "" || review141Used(got[0]) != 80 || got[0].Source != "local" || got[0].SameAccountAs != "" {
				t.Errorf("first row = %+v, want compacted local row", got[0])
			}
			for i, id := range []string{"known", "known-later"} {
				row := got[i+1]
				anchor := ""
				if i == 1 {
					anchor = "known"
				}
				if row.ProviderID != id || row.Family != later.id || row.Account != "account" || review141Used(row) != 10 || row.Source != "donor via donor" || row.SameAccountAs != anchor {
					t.Errorf("later row %d = %+v, want %s sharing donor with anchor %q", i, row, id, anchor)
				}
			}
		})
	}
}
