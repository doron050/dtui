package tui

import (
	"testing"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// azureSubsCache seeds the derived cache with the Azure subscription resolver's
// table so enrichRecords resolves GUID→name without a fetch.
func azureSubsCache(m map[string]string) map[string]map[string]string {
	return map[string]map[string]string{"azure-subscriptions": m}
}

// enrichRecords resolves the registered derived fields (here the Azure
// subscription GUID→name) in place, leaves the field unset (never the id) when
// unresolved, and no-ops on a cold cache.
func TestEnrichRecordsSubscription(t *testing.T) {
	ds := &dataSource{derived: azureSubsCache(map[string]string{"guid-1": "Prod"})}
	rows := []map[string]any{
		{"azure.subscription": "guid-1"},
		{"azure.subscription": "guid-unknown"},
		{"name": "no-subscription-field"},
	}
	if !ds.enrichRecords(rows) {
		t.Error("enrichRecords should report a change when a value resolves")
	}
	if rows[0][catalog.SubscriptionNameKey] != "Prod" {
		t.Errorf("row0 name = %v, want Prod", rows[0][catalog.SubscriptionNameKey])
	}
	if _, ok := rows[1][catalog.SubscriptionNameKey]; ok {
		t.Error("unknown GUID must leave subscription name unset (blank, never the id)")
	}
	if _, ok := rows[2][catalog.SubscriptionNameKey]; ok {
		t.Error("row without azure.subscription must be untouched")
	}

	// Cold cache: no-op, no change reported, nothing added.
	empty := &dataSource{}
	r := []map[string]any{{"azure.subscription": "guid-1"}}
	if empty.enrichRecords(r) {
		t.Error("cold cache should report no change")
	}
	if _, ok := r[0][catalog.SubscriptionNameKey]; ok {
		t.Error("cold cache should not enrich")
	}
}

// ensureDerived fires the lookup fetches once per session and is gated after.
func TestEnsureDerivedFiresOnce(t *testing.T) {
	var calls int
	ds := &dataSource{runFn: func(dql string) ([]map[string]any, error) {
		calls++
		return nil, nil
	}}
	if cmd := ds.ensureDerived(); cmd == nil {
		t.Fatal("first ensureDerived returned nil cmd")
	} else {
		cmd() // execute the batched fetch goroutines
	}
	if cmd := ds.ensureDerived(); cmd != nil {
		t.Error("second ensureDerived should be gated (nil)")
	}
	if want := len(catalog.DerivedFieldResolvers); calls != want {
		t.Errorf("derived fetches ran %d times, want %d (one per resolver)", calls, want)
	}
}

// Browsing an AZURE_ type through the resources browser resolves the
// SUBSCRIPTION column to the friendly name once the session cache lands.
func TestAzureBrowseResolvesSubscriptionName(t *testing.T) {
	ds := &dataSource{runFn: func(string) ([]map[string]any, error) { return nil, nil }}
	spec := catalog.Lookup("resources")
	scope := catalog.Scope{Arg: "AZURE_MICROSOFT_COMPUTE_DISKS", Timeframe: catalog.DefaultTimeframe}
	v := newTableView(ds, spec, scope)

	// List rows arrive before the cache: name stays blank.
	v.Update(dataMsg{owner: v, seq: v.seq, records: []map[string]any{
		{"id": "AZURE_MICROSOFT_COMPUTE_DISKS-1", "azure.subscription": "guid-1", "azure.location": "eastus"},
	}})
	if _, ok := v.all[0][catalog.SubscriptionNameKey]; ok {
		t.Error("subscription name should be blank before the cache loads")
	}

	// The app fills the shared cache, then broadcasts the derivedOwner result to
	// the view, which re-enriches its rows.
	ds.setDerived("azure-subscriptions", map[string]string{"guid-1": "Dynatrace-SaaS-Prod"})
	v.Update(dataMsg{owner: derivedOwner{name: "azure-subscriptions"}})
	if v.all[0][catalog.SubscriptionNameKey] != "Dynatrace-SaaS-Prod" {
		t.Errorf("subscription not resolved after cache: %v", v.all[0][catalog.SubscriptionNameKey])
	}

	// The active column set is the Azure one (SUBSCRIPTION, no ACCOUNT).
	var hasSub, hasAccount bool
	for _, c := range v.columns() {
		switch c.Title {
		case "SUBSCRIPTION":
			hasSub = true
		case "ACCOUNT":
			hasAccount = true
		}
	}
	if !hasSub || hasAccount {
		t.Errorf("azure browse columns wrong: hasSub=%v hasAccount=%v", hasSub, hasAccount)
	}
}

// Synthetic __-keys (the resolved subscription name) must never appear as a
// facetable attribute: faceting one would hit a non-existent server field and
// silently return empty (ADR-0016). Real Grail keys stay candidates.
func TestFacetCandidatesSkipSyntheticKeys(t *testing.T) {
	ds := &dataSource{runFn: func(string) ([]map[string]any, error) { return nil, nil }}
	spec := catalog.Lookup("resources")
	scope := catalog.Scope{Arg: "AZURE_MICROSOFT_COMPUTE_DISKS", Timeframe: catalog.DefaultTimeframe}
	v := newTableView(ds, spec, scope)
	v.all = []map[string]any{{
		"id":                        "AZURE_MICROSOFT_COMPUTE_DISKS-1",
		"azure.subscription":        "guid-1",
		"azure.location":            "eastus",
		catalog.SubscriptionNameKey: "Prod",
	}}
	for _, c := range v.facetCandidates() {
		if c == catalog.SubscriptionNameKey {
			t.Errorf("synthetic key %q must not be a facet candidate", c)
		}
	}
	// The real Grail keys are still candidates.
	var hasLoc, hasSub bool
	for _, c := range v.facetCandidates() {
		switch c {
		case "azure.location":
			hasLoc = true
		case "azure.subscription":
			hasSub = true
		}
	}
	if !hasLoc || !hasSub {
		t.Errorf("real keys missing from candidates: location=%v subscription=%v", hasLoc, hasSub)
	}
}

// A derivedOwner result is broadcast to every view (app.go dataMsg dispatch),
// not only the one that fired the fetch. A second Azure view — a parent below a
// drill-down, or a sibling opened while the fetch was in flight — must fill its
// own rows from the shared cache when the broadcast lands, not stay blank.
func TestDerivedBroadcastEnrichesNonInitiatingView(t *testing.T) {
	ds := &dataSource{runFn: func(string) ([]map[string]any, error) { return nil, nil }}
	spec := catalog.Lookup("resources")
	scope := catalog.Scope{Arg: "AZURE_MICROSOFT_COMPUTE_DISKS", Timeframe: catalog.DefaultTimeframe}

	other := newTableView(ds, spec, scope)
	other.Update(dataMsg{owner: other, seq: other.seq, records: []map[string]any{
		{"id": "disk-2", "azure.subscription": "guid-1", "azure.location": "eastus"},
	}})
	if _, ok := other.all[0][catalog.SubscriptionNameKey]; ok {
		t.Fatal("precondition: view should be blank before the cache lands")
	}

	// The broadcast reaches this view even though it did not fire the fetch.
	ds.setDerived("azure-subscriptions", map[string]string{"guid-1": "Dynatrace-SaaS-Prod"})
	other.Update(dataMsg{owner: derivedOwner{name: "azure-subscriptions"}})
	if other.all[0][catalog.SubscriptionNameKey] != "Dynatrace-SaaS-Prod" {
		t.Errorf("non-initiating view not enriched on broadcast: %v", other.all[0][catalog.SubscriptionNameKey])
	}
}
