package catalog

// DerivedFieldResolver describes an app-derived record field: a value resolved
// from a session-scoped lookup table and stamped onto records so the pure
// column and KeyFact readers can read it like any other field. It is the
// reusable shape behind ADR-0015's id→name enrichment and ADR-0016's `__`-hidden
// synthetic keys — register a resolver here to enrich any future
// opaque-id→friendly-value mapping without touching the TUI shell.
//
// Lifecycle (all on bubbletea's update goroutine, never a query goroutine): the
// dataSource fires Query once per session (ensureDerived), builds the lookup
// with Parse, then stamps rec[Key] = table[rec[SourceKey]] onto every fetched
// record (enrichRecords). The resolved value lands under Key — a `__`-hidden
// synthetic key — so it never appears as a Grail key in the raw record dump.
type DerivedFieldResolver struct {
	Name      string // stable id: the fetch owner and the cache key
	Query     string // one-shot DQL for the lookup table (session-wide, no scope)
	Limit     int64  // result-record cap for the fetch
	Parse     func(records []map[string]any) map[string]string
	SourceKey string // record key holding the raw id to resolve
	Key       string // `__`-hidden destination key for the resolved value
}

// DerivedFieldResolvers is the registry applied to every fetched record. Order
// is irrelevant; each resolver stamps an independent field.
var DerivedFieldResolvers = []DerivedFieldResolver{azureSubscriptionResolver}

// azureSubscriptionResolver resolves an Azure resource's subscription GUID
// (azure.subscription) to its friendly name via the subscriptions Smartscape
// nodes, stamped under SubscriptionNameKey. See ADR-0015 / ADR-0016.
var azureSubscriptionResolver = DerivedFieldResolver{
	Name:      "azure-subscriptions",
	Query:     AzureSubscriptionsQuery(),
	Limit:     AzureSubscriptionLimit,
	Parse:     ParseAzureSubscriptions,
	SourceKey: "azure.subscription",
	Key:       SubscriptionNameKey,
}
