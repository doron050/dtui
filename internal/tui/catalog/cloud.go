package catalog

import (
	"fmt"
	"strings"
)

// Cloud inventory and the generic entity browser. AWS nodes on real tenants
// are name-poor: `name` is an empty string on ~98%, the display name lives in
// the `tags:aws` Name tag or the ARN (validated live). The census views feed
// the typed browser through Scope.Arg — every Smartscape type is browsable
// without a bespoke screen.

// censusSpec builds a type-census view (all types, or one cloud's slice) —
// :aws is :entities filtered to AWS_ types, one definition apart. The
// unfiltered census keeps raw type names ("pod" without its K8S_ prefix is
// ambiguous there); a single-cloud slice reads better prettified.
func censusSpec(name string, aliases []string, desc, filter string, limit int, pretty bool) *Spec {
	typeCol := Column{Title: "TYPE", Field: "type"}
	if pretty {
		typeCol = Column{Title: "TYPE", Value: func(rec map[string]any) string { return prettyType(Str(rec, "type")) }}
	}
	return &Spec{
		Name:    name,
		Aliases: aliases,
		Kind:    KindEntity,
		Desc:    desc,
		Query: func(s Scope) string {
			var b strings.Builder
			b.WriteString(`smartscapeNodes "*"`)
			if filter != "" {
				fmt.Fprintf(&b, "\n| filter %s", filter)
			}
			fmt.Fprintf(&b, "\n| summarize count = count(), by:{type}\n| sort count desc\n| limit %d", limit)
			return b.String()
		},
		Columns: []Column{
			typeCol,
			{Title: "COUNT", Field: "count", Width: 6, Right: true},
		},
		EnterTarget: "resources",
		EnterArg:    func(rec map[string]any) string { return Str(rec, "type") },
		Drills:      map[string]string{},
	}
}

var awsSpec = censusSpec("aws", []string{"cloud"},
	"AWS inventory by resource type", `startsWith(type, "AWS_")`, 100, true)

var azureSpec = censusSpec("azure", nil,
	"Azure inventory by resource type", `startsWith(type, "AZURE_")`, 100, true)

var entitiesSpec = censusSpec("entities", []string{"topo", "census"},
	"All Smartscape entity types (topology census)", "", 200, false)

// resourceDisplay is the NAME cell: the coalesced display name (from the query)
// or the raw id as a last resort.
func resourceDisplay(rec map[string]any) string {
	if d := Str(rec, "display"); d != "" {
		return d
	}
	return Str(rec, "id")
}

func resourceType(rec map[string]any) string { return prettyType(Str(rec, "type")) }

// azureSubscriptionlessTypes are the AZURE_ Smartscape types that live above a
// subscription and never carry azure.subscription — Azure regions, availability
// zones, and the AAD tenant (the only null-subscription AZURE_ types on the validated tenant).
// Browsing one of these drops the SUBSCRIPTION column rather than showing a
// column that is blank by construction.
var azureSubscriptionlessTypes = map[string]bool{
	"AZURE_MICROSOFT_RESOURCES_LOCATIONS":                   true,
	"AZURE_MICROSOFT_RESOURCES_LOCATIONS_AVAILABILITYZONES": true,
	"AZURE_MICROSOFT_RESOURCES_TENANTS":                     true,
}

func azureSubscriptionless(typ string) bool { return azureSubscriptionlessTypes[typ] }

var resourcesSpec = &Spec{
	Name:         "resources",
	Aliases:      []string{"res"},
	Kind:         KindEntity,
	EntityScoped: false,
	Desc:         "Entities of one type (generic browser)",
	// The account-analog fields differ by provider; the priority list carries
	// all of them, absent ones are harmless.
	FacetPriority: []string{"type", "aws.region", "aws.account.id", "azure.location", "azure.subscription"},
	Query: func(s Scope) string {
		typ := s.Arg
		if typ == "" {
			typ = "*"
		}
		// fieldsRemove (not | fields) keeps the tags:<ctx> maps on the row so
		// the peek and the facet picker can read them (aws/azure/gcp/k8s);
		// a | fields projection would drop them. aws.object/azure.object are
		// multi-KB manifests, references is a lazy graph-edge object — both
		// too heavy for a list row. The display coalesce serves every provider:
		// AWS is name-poor (name empty ~98%) so it falls to the Name tag / ARN;
		// Azure is name-rich (name populated) so name wins, with the Azure
		// resource name/id as the tail fallback.
		return fmt.Sprintf("smartscapeNodes %q\n"+
			"| fieldsAdd display = coalesce(if(name != \"\", name), `tags:aws`[`Name`], azure.resource.name, aws.arn, azure.resource.id)\n"+
			"| fieldsRemove aws.object, azure.object, references, k8s.object\n"+
			"| sort display asc\n| limit 500", typ)
	},
	// ScopeColumns swaps in the Azure column set (REGION=azure.location,
	// SUBSCRIPTION=the resolved subscription name) when browsing an AZURE_ type;
	// nil for every other provider falls through to the AWS/generic Columns
	// below, so AWS and the mixed :entities census are unchanged.
	ScopeColumns: func(s Scope) []Column {
		if strings.HasPrefix(s.Arg, "AZURE_") {
			cols := []Column{
				{Title: "NAME", Value: resourceDisplay},
				{Title: "TYPE", Width: 26, Value: resourceType},
				{Title: "REGION", Field: "azure.location", Width: 14},
			}
			// Azure geography/tenant metadata (regions, availability zones, the
			// AAD tenant) sit above any subscription and carry none — validated
			// on the validated tenant, the only AZURE_ types with a null azure.subscription. Omit
			// the SUBSCRIPTION column entirely for them rather than show a column
			// that is blank by construction.
			if !azureSubscriptionless(s.Arg) {
				// The resolved subscription name is enriched client-side from the
				// session cache (dataSource.enrichRecords) into SubscriptionNameKey;
				// blank — never the raw GUID — when unresolved. Column title is
				// curated; the field is the __-hidden synthetic key (ADR-0016).
				cols = append(cols, Column{Title: "SUBSCRIPTION", Field: SubscriptionNameKey, Width: 24})
			}
			return append(cols, Column{Title: "SEEN", Width: 5, Right: true, Value: lifetimeAge})
		}
		return nil
	},
	Columns: []Column{
		{Title: "NAME", Value: resourceDisplay},
		{Title: "TYPE", Width: 26, Value: resourceType},
		{Title: "REGION", Field: "aws.region", Width: 12},
		{Title: "ACCOUNT", Field: "aws.account.id", Width: 12},
		{Title: "SEEN", Width: 5, Right: true, Value: lifetimeAge},
	},
	Entity: func(rec map[string]any) *Entity {
		id := Str(rec, "id")
		if id == "" {
			return nil
		}
		name := Str(rec, "display")
		if name == "" {
			name = Str(rec, "name")
		}
		return &Entity{ID: id, Name: name, Type: Str(rec, "type")}
	},
	Drills: map[string]string{"l": "logs", "s": "traces", "m": "metrics", "p": "problems", "v": "events"},
}

// prettyType renders an entity type compactly: AWS_EC2_INSTANCE → ec2
// instance, K8S_POD → pod, AZURE_MICROSOFT_COMPUTE_DISKS → compute disks (the
// redundant AZURE/MICROSOFT provider prefix is dropped). The raw type stays
// visible in detail pages.
func prettyType(t string) string {
	t = strings.TrimPrefix(t, "AWS_")
	t = strings.TrimPrefix(t, "K8S_")
	t = strings.TrimPrefix(t, "AZURE_MICROSOFT_")
	t = strings.TrimPrefix(t, "AZURE_")
	return strings.ToLower(strings.ReplaceAll(t, "_", " "))
}

// SubscriptionNameKey holds the client-resolved Azure subscription friendly
// name. The "__" prefix keeps it out of the raw record dump (the inspector
// hides __-prefixed keys) so it never masquerades as a Grail key; it surfaces
// only through a deliberately non-Grail display label. See ADR-0016.
const SubscriptionNameKey = "__azure.subscription.name"

// AzureSubscriptionLimit bounds the one-shot subscription-name fetch. Tenants
// have at most a few hundred subscriptions; cap generously.
const AzureSubscriptionLimit = 4000

// AzureSubscriptionsQuery fetches the GUID→friendly-name map from the
// subscription Smartscape nodes (smartscapeNodes only — the classic
// dt.entity.azure_subscription is keyed by a DT-hashed id, not the GUID that
// every other Azure node stamps in azure.subscription). See
// docs/dev/learnings.md.
func AzureSubscriptionsQuery() string {
	return fmt.Sprintf("smartscapeNodes \"AZURE_MICROSOFT_RESOURCES_SUBSCRIPTIONS\"\n"+
		"| fields azure.subscription, name\n| limit %d", AzureSubscriptionLimit)
}

// ParseAzureSubscriptions builds the GUID→name lookup from the subscriptions
// query records, skipping rows missing either side.
func ParseAzureSubscriptions(records []map[string]any) map[string]string {
	out := make(map[string]string, len(records))
	for _, rec := range records {
		guid := Str(rec, "azure.subscription")
		name := Str(rec, "name")
		if guid == "" || name == "" {
			continue
		}
		out[guid] = name
	}
	return out
}
