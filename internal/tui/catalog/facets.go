package catalog

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

// Server-side narrowing for table views: the '/'-promoted full-text search
// and the 'f' facet picker.
//
// The injectors below rewrite generated DQL by LINE CONVENTION rather than a
// structured pipeline type: the source command owns the first line and every
// query ends in a `| sort`/`| limit` tail. convention_test.go enforces both
// on every spec, so a Query closure that drifts fails the suite instead of
// silently mis-splicing here.
//
// Validated live (see docs/dev/learnings.md §1.10):
//   - `| search "text"` matches case-insensitively across all fields on
//     fetch and smartscapeNodes pipelines alike, but is rejected after
//     transforming commands — parse/expand/summarize —
//     (SEARCH_COMMAND_NOT_ALLOWED_AFTER), while filter and fieldsRemove
//     before it are fine. It therefore injects directly after the source.
//   - Facet stages never wrap the field in toString() — a function call on
//     the field mutes Grail's index features. Stage() instead branches on
//     the shape of the (always stringified — fieldsSummary) value.
//   - matchesValue supports leading/trailing '*' wildcards, case-insensitive,
//     applies element-wise to string arrays, and never matches non-string
//     fields (warning, not error).

// Facet is one attribute filter: an exact value, a '*' wildcard pattern, or
// — with Tokens — a `~` token search, the only operator that matches inside
// arrays of records or numbers server-side.
type Facet struct {
	Field  string
	Value  string
	Tokens bool `json:",omitempty"`
	// MapField/MapKey turn a facet into a subkey filter on a smartscape
	// `tags:<context>` record/map field: the DQL reference becomes
	// `<MapField>`[`<MapKey>`]. Field then holds the friendly display identity
	// (e.g. "k8s.labels:app", "azure:Dtp_Stage", "gcp:zone") — non-empty and
	// unique, so it still serves as the identity key in exclude-self and
	// bucket comparisons. See docs/dev/learnings.md §1.4b.
	MapField string `json:",omitempty"` // e.g. "tags:k8s.labels" (empty ⇒ scalar facet)
	MapKey   string `json:",omitempty"` // e.g. "app"
}

// Pattern reports whether the facet value is a wildcard pattern.
func (f Facet) Pattern() bool { return !f.Tokens && strings.Contains(f.Value, "*") }

// fieldExpr is the DQL field reference the filter/summary stages compare on:
// a bracket-indexed map subkey for map facets (context and key each
// backtick-quoted — both may carry '.', '/' or ':'), else the escaped scalar
// field. Never wraps the field in a function call (that mutes Grail indexes).
func (f Facet) fieldExpr() string {
	if f.MapField != "" {
		return "`" + f.MapField + "`[`" + f.MapKey + "`]"
	}
	return escapeField(f.Field)
}

// Value shapes that pick the exact-facet encoding in Stage. fieldsSummary
// hands every value over stringified, so the field's server-side type is
// unknowable here; the shape of the value decides which typed comparisons
// can match it (all validated live).
var (
	smartscapeIDShape = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9A-F]{16}$`)
	hexShape          = regexp.MustCompile(`^[0-9a-fA-F]{16,64}$`)                // span.id (16), trace.id (32)
	uidHexShape       = regexp.MustCompile(`^([0-9a-fA-F]{16}|[0-9a-fA-F]{32})$`) // toUid()'s two accepted lengths
	uuidShape         = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)
	integerShape      = regexp.MustCompile(`^-?[0-9]+$`)
	decimalShape      = regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)
)

// idShaped reports whether the value reads as a typed scalar's string
// representation — smartscape ID, UID hex, UUID, or IP address. Those field
// types never equal a string literal (`==` is silently empty), but `~`
// auto-converts and matches their exact representation.
func idShaped(v string) bool {
	return smartscapeIDShape.MatchString(v) || hexShape.MatchString(v) ||
		uuidShape.MatchString(v) || net.ParseIP(v) != nil
}

// Stage renders the facet as a DQL filter stage. Every encoding compares the
// bare field — wrapping it in toString() mutes Grail's indexes — and covers
// each field type the stringified value could have come from:
//   - Tokens          → field ~ "v": token search, the only operator that
//     matches inside arrays of records or numbers.
//   - '*' patterns    → matchesValue(field, "v"): whole-value wildcards,
//     element-wise on string arrays; never matches non-string fields.
//   - true/false      → == true or == "true" (`~` never matches booleans).
//   - ID-shaped       → == "v" or ~ "v" (typed scalars need the `~` leg);
//     16-/32-hex UID values (span.id, trace.id, subtrace.id) add an explicit
//     `== toUid("v")` leg — the canonical UID comparison — since the `~`
//     auto-convert is a token-index trick that is not guaranteed across
//     tenants, while toUid() no-ops to null (never errors) on non-UID fields.
//   - integers        → == 2 or == "2" or == duration(2, "ns") — the
//     duration leg matches duration fields, whose fieldsSummary values are
//     nanosecond counts that toString() never matched.
//   - decimals        → == 2.5 or == "2.5"
//   - other strings   → == "v" alone: exact and case-sensitive. No `~` leg —
//     it is case-insensitive and token-based, so "Running" would also match
//     "Running fast" (validated live).
func (f Facet) Stage() string {
	field := f.fieldExpr()
	v := f.Value
	switch {
	case f.Tokens:
		return fmt.Sprintf("| filter %s ~ %q", field, v)
	case f.Pattern():
		return fmt.Sprintf("| filter matchesValue(%s, %q)", field, v)
	case v == "true" || v == "false":
		return fmt.Sprintf("| filter %s == %s or %s == %q", field, v, field, v)
	case uidHexShape.MatchString(v):
		return fmt.Sprintf("| filter %s == %q or %s == toUid(%q) or %s ~ %q", field, v, field, v, field, v)
	case idShaped(v):
		return fmt.Sprintf("| filter %s == %q or %s ~ %q", field, v, field, v)
	case integerShape.MatchString(v):
		return fmt.Sprintf(`| filter %s == %s or %s == %q or %s == duration(%s, "ns")`, field, v, field, v, field, v)
	case decimalShape.MatchString(v):
		return fmt.Sprintf("| filter %s == %s or %s == %q", field, v, field, v)
	default:
		return fmt.Sprintf("| filter %s == %q", field, v)
	}
}

// Condition returns the bare DQL predicate that Stage() wraps in "| filter …".
// When the predicate contains " or " (multi-leg encodings for typed scalars,
// integers, etc.) it is parenthesized so it ANDs correctly inside filter:{}.
func (f Facet) Condition() string {
	stage := f.Stage()
	pred := strings.TrimPrefix(stage, "| filter ")
	if strings.Contains(pred, " or ") {
		return "(" + pred + ")"
	}
	return pred
}

// ContainsSearchCondition builds a `contains(toString(<dim>), term,
// caseSensitive:false) or …` OR-chain over the given dims. toString() is
// required — contains on a non-string dim returns silent-empty, not an error
// (validated live). Falls back to searching dt.service.name when dims
// is empty.
func ContainsSearchCondition(term string, dims []string) string {
	if len(dims) == 0 {
		return fmt.Sprintf("contains(toString(dt.service.name), %q, caseSensitive:false)", term)
	}
	parts := make([]string, 0, len(dims))
	for _, d := range dims {
		parts = append(parts, fmt.Sprintf("contains(toString(%s), %q, caseSensitive:false)", escapeField(d), term))
	}
	return "(" + strings.Join(parts, " or ") + ")"
}

// ServiceFieldsSummaryQuery renders the value-picker exploration query for one
// field against the service metric roster: explores the raw series descriptors
// so every dimension's distinct values are reachable regardless of how they
// collapse in the summarize step.
func ServiceFieldsSummaryQuery(tf Timeframe, target Facet, facets []Facet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "metrics from:%s\n| filter in(metric.key, {%s})",
		tf.DQL(), serviceMetricKeyListInline())
	for _, f := range facets {
		if f.Field != target.Field {
			fmt.Fprintf(&b, "\n| filter %s", f.fieldExpr()+" == "+fmt.Sprintf("%q", f.Value))
		}
	}
	fmt.Fprintf(&b, "\n| fieldsSummary %s, topValues: %d", target.fieldExpr(), FacetTopValues)
	return b.String()
}

// Label is the compact pill text shown in the table header.
func (f Facet) Label() string {
	if f.Tokens || f.Pattern() {
		return f.Field + "~" + f.Value
	}
	return f.Field + "=" + f.Value
}

// SearchStage renders the server-side full-text stage ('/' + enter). The
// search command matches whole tokens — "fss" misses pod "…-fssb5"
// (validated live) — so a term without wildcards of its own wraps in *…*
// for the contains semantics the '/' filter promises.
func SearchStage(term string) string {
	if !strings.Contains(term, "*") {
		term = "*" + term + "*"
	}
	return fmt.Sprintf("| search %q", term)
}

// bareField matches field names DQL accepts unquoted (dotted paths).
var bareField = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// mapIndexRef matches a facet field that indexes into a projected map: a base
// field followed by one or more [key] segments (labels[app.kubernetes.io/name]).
// K8s label/annotation keys are DNS-style and never contain brackets, so the
// [ split is unambiguous. The keys carry dots/slashes/colons that a bare
// bracket access would mis-read as nested field access, so Stage/fieldsSummary
// must backtick every key — see docs/dev/learnings.md §1.22.
var mapIndexRef = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.:]*)((?:\[[^\[\]]+\])+)$`)

// indexSegment pulls each [key] out of a map-index ref's suffix.
var indexSegment = regexp.MustCompile(`\[([^\[\]]+)\]`)

// escapeBase backtick-quotes a plain field name DQL rejects bare (`tags:aws`).
func escapeBase(name string) string {
	if bareField.MatchString(name) {
		return name
	}
	return "`" + name + "`"
}

// escapeField renders a facet field as valid DQL. A map-index ref
// (labels[app.kubernetes.io/name]) becomes base[`key`] with every key
// backtick-quoted; anything else backtick-quotes the whole name when it is not
// bare.
func escapeField(name string) string {
	if m := mapIndexRef.FindStringSubmatch(name); m != nil {
		var b strings.Builder
		b.WriteString(escapeBase(m[1]))
		for _, seg := range indexSegment.FindAllStringSubmatch(m[2], -1) {
			b.WriteString("[`" + seg[1] + "`]")
		}
		return b.String()
	}
	return escapeBase(name)
}

// EscapeField is the exported form of escapeField, usable by packages outside
// catalog that compose DQL field references (primary-tag column injection,
// service metrics queries). Backtick-quotes field names DQL rejects bare.
func EscapeField(name string) string { return escapeField(name) }

// tailStart returns the index of the first line of a query's trailing
// `| sort` / `| limit` suffix (len(lines) when there is none). Every catalog
// query ends with that suffix by convention, and narrowing stages must go
// before it so the filter applies before the cap truncates.
func tailStart(lines []string) int {
	cut := len(lines)
	for cut > 0 {
		t := strings.TrimSpace(lines[cut-1])
		if !strings.HasPrefix(t, "| sort") && !strings.HasPrefix(t, "| limit") {
			break
		}
		cut--
	}
	return cut
}

// InjectStages inserts pipeline stages before a query's sort/limit tail.
// Filter stages are legal anywhere, and the tail position filters the exact
// fields the columns (and the facet picker) present — post-summarize, in
// views that aggregate.
func InjectStages(dql string, stages []string) string {
	if len(stages) == 0 {
		return dql
	}
	lines := strings.Split(dql, "\n")
	cut := tailStart(lines)
	out := make([]string, 0, len(lines)+len(stages))
	out = append(out, lines[:cut]...)
	out = append(out, stages...)
	out = append(out, lines[cut:]...)
	return strings.Join(out, "\n")
}

// InjectSearches inserts one full-text stage per term directly after the
// source command (always the query's first line by catalog convention) —
// DQL rejects `search` after transforming commands like parse, expand, or
// summarize (SEARCH_COMMAND_NOT_ALLOWED_AFTER), but chains of search stages
// compose as AND (both validated live).
func InjectSearches(dql string, terms []string) string {
	if len(terms) == 0 {
		return dql
	}
	lines := strings.Split(dql, "\n")
	out := make([]string, 0, len(lines)+len(terms))
	out = append(out, lines[0])
	for _, t := range terms {
		out = append(out, SearchStage(t))
	}
	out = append(out, lines[1:]...)
	return strings.Join(out, "\n")
}

// --- buckets ----------------------------------------------------------------

// BucketField is Grail's physical-partition metadata field. Every record in a
// bucket-backed table carries it and it is queryable at any point before an
// aggregation, but responses only include it once a `fieldsAdd
// dt.system.bucket` projects it (validated live). Buckets are physical data
// separation — the primary Grail narrowing axis — so eligible views project
// the field into every record and the facet manager pins it first.
const BucketField = "dt.system.bucket"

// bucketFieldStage projects the record's bucket into the response.
const bucketFieldStage = "| fieldsAdd " + BucketField

// bucketTables are the Grail tables whose records live in buckets — the
// dt.system.table values of `fetch dt.system.buckets` (validated live).
// Referencing dt.system.bucket on any other table fails the whole query with
// FIELD_DOES_NOT_EXIST (validated live on dt.entity.*, dt.system.buckets and
// dt.semantic_dictionary.*) — not null — so eligibility is a whitelist.
var bucketTables = map[string]bool{
	"logs": true, "events": true, "spans": true, "bizevents": true,
	"metrics": true, "security.events": true, "application.snapshots": true,
	"user.events": true, "user.sessions": true, "user.replays": true,
	"dt.system.events": true,
}

// sourceTable returns the table a query's first line fetches ("" when the
// source is not a fetch command).
func sourceTable(dql string) string {
	first := strings.TrimSpace(strings.SplitN(dql, "\n", 2)[0])
	table, ok := strings.CutPrefix(first, "fetch ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(table, ",", 2)[0])
}

// BucketEligible reports whether a query's records carry dt.system.bucket:
// the source fetches a bucket-backed table — including the dt.davis.* and
// dt.synthetic.* views over events (validated live) — and no later stage
// drops the field (summarize aggregates it away, a fields projection
// excludes it).
func BucketEligible(dql string) bool {
	table := sourceTable(dql)
	if table == "" {
		return false
	}
	if !bucketTables[table] && !strings.HasPrefix(table, "dt.davis.") && !strings.HasPrefix(table, "dt.synthetic.") {
		return false
	}
	for _, l := range strings.Split(dql, "\n")[1:] {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "| summarize") || strings.HasPrefix(t, "| fields ") {
			return false
		}
	}
	return true
}

// ComposeQuery renders a view's final list query from its narrowing state:
// search terms directly after the source (InjectSearches), then — on
// bucket-eligible queries — the bucket facet. A single exact bucket facet on
// a real table becomes the fetch command's bucket: parameter, Grail's
// native physical-read pruning; the dt.davis.*/dt.synthetic.* views reject
// that parameter (PARAMETER_NOT_ALLOWED_FOR_FETCH_VIEW, validated live) and
// keep a head filter stage instead, as do multiple bucket facets (whose AND
// semantics a multi-value parameter would flip to OR). Then the bucket
// projection (projectBucket; off for API views, whose query is analyzer
// input rather than the records the table shows), and the remaining facets
// before the sort/limit tail as usual. The stage order
// source → search → bucket filter → fieldsAdd is validated live.
//
// Map facets (tags:<ctx>[key]) are injected in the head block, after any
// injected searches and bucket head stages (which may include the bucket
// | fieldsAdd projection) but before the | fields / | summarize transforms
// that drop the tag map fields — a filter after those fails with
// FIELD_DOES_NOT_EXIST (validated live on an Azure-enabled tenant: HOST/AZURE/K8S, 2026-07-24).
// The bucket | fieldsAdd is additive, so the tag maps still reach the filter.
func ComposeQuery(dql string, searches []string, facets []Facet, projectBucket bool) string {
	var bucketFacets []Facet
	var mapFacets []string // must land before | fields / | summarize projections
	var tail []string
	eligible := BucketEligible(dql)
	for _, f := range facets {
		switch {
		case eligible && f.Field == BucketField:
			bucketFacets = append(bucketFacets, f)
		case f.MapField != "":
			mapFacets = append(mapFacets, f.Stage())
		default:
			tail = append(tail, f.Stage())
		}
	}
	var head []string
	if len(bucketFacets) == 1 && !bucketFacets[0].Pattern() && !bucketFacets[0].Tokens && bucketTables[sourceTable(dql)] {
		lines := strings.SplitN(dql, "\n", 2)
		lines[0] += fmt.Sprintf(", bucket:{%q}", bucketFacets[0].Value)
		dql = strings.Join(lines, "\n")
	} else {
		for _, f := range bucketFacets {
			head = append(head, f.Stage())
		}
	}
	if eligible && projectBucket {
		head = append(head, bucketFieldStage)
	}
	out := InjectSearches(dql, searches)
	// Everything that must precede projection stages goes in the head: bucket
	// head stages first (they may include the fieldsAdd projection), then the
	// map-facet filters that reference the raw tag maps at source.
	head = append(head, mapFacets...)
	if len(head) > 0 {
		lines := strings.Split(out, "\n")
		at := 1 + len(searches)
		spliced := make([]string, 0, len(lines)+len(head))
		spliced = append(spliced, lines[:at]...)
		spliced = append(spliced, head...)
		spliced = append(spliced, lines[at:]...)
		out = strings.Join(spliced, "\n")
	}
	return InjectStages(out, tail)
}

// FacetTopValues is how many suggestions the value picker requests. Kept
// generous (not the ~20 a human eyeballs) because the picker filters and
// scrolls client-side: on a high-cardinality field the value you want is
// often past the first handful, so fetch a deep slice once and narrow it by
// typing rather than round-tripping. fieldsSummary aggregates server-side, so
// a larger topValues only grows the returned list, not the scan.
const FacetTopValues = 250

// FieldsSummaryQuery renders the top-values exploration query for one field:
// the view's query minus its sort/limit tail, narrowed by the active
// searches and every facet except the explored field's own (so re-faceting
// a field offers all its values), summarized server-side.
//
// Map facets (tags:<ctx>[key]) use a stripped pipeline — the source plus its
// leading | filter scope stages only — because any | fieldsAdd / | fields /
// | summarize in the template drops the tag map before the bracket-access
// runs, causing FIELD_DOES_NOT_EXIST (validated on an Azure-enabled tenant, 2026-07-24). The
// bracket expression summarizes directly: fieldsSummary accepts it (validated
// live — no temporary projection needed).
func FieldsSummaryQuery(dql string, target Facet, searches []string, facets []Facet) string {
	var head string
	if target.MapField != "" {
		lines := strings.Split(dql, "\n")
		kept := []string{lines[0]}
		for _, ln := range lines[1:] {
			if !strings.HasPrefix(strings.TrimSpace(ln), "| filter ") {
				break
			}
			kept = append(kept, ln)
		}
		head = InjectSearches(strings.Join(kept, "\n"), searches)
	} else {
		lines := strings.Split(dql, "\n")
		head = InjectSearches(strings.Join(lines[:tailStart(lines)], "\n"), searches)
	}
	var b strings.Builder
	b.WriteString(head)
	for _, f := range facets {
		if f.Field == target.Field {
			continue
		}
		// The map branch's head is stripped to source + leading | filter, so the
		// projections that create scalar facet fields are gone — a scalar
		// | filter here hits FIELD_DOES_NOT_EXIST (silent Grail empty). Only
		// other map facets read straight off the source via bracket access.
		if target.MapField != "" && f.MapField == "" {
			continue
		}
		b.WriteString("\n" + f.Stage())
	}
	fmt.Fprintf(&b, "\n| fieldsSummary %s, topValues: %d", target.fieldExpr(), FacetTopValues)
	return b.String()
}

// FacetValue is one fieldsSummary suggestion.
type FacetValue struct {
	Value string // stringified — drops straight into Facet.Value
	Count string
}

// ParseFieldsSummary extracts the top values from a fieldsSummary result
// (shape validated live: one record per field, values: [{value, count}]).
func ParseFieldsSummary(records []map[string]any) []FacetValue {
	if len(records) == 0 {
		return nil
	}
	values, _ := records[0]["values"].([]any)
	out := make([]FacetValue, 0, len(values))
	for _, v := range values {
		m, _ := v.(map[string]any)
		if m == nil {
			continue
		}
		// Skip the null bucket — its value is nil or formats to ""; selecting
		// it would build `| filter field == ""` which matches nothing useful.
		if m["value"] == nil || FormatValue(m["value"]) == "" {
			continue
		}
		out = append(out, FacetValue{Value: FormatValue(m["value"]), Count: Str(m, "count")})
	}
	return out
}

// SmartscapeTagsQuery builds a tag-discovery query from a smartscape list's
// query: it keeps the source command and any leading `| filter` scope stages,
// drops the projection/transform stages and the sort/limit tail, strips the
// heavy `*.object` manifests, and samples a bounded slice. The unprojected
// sample carries every `tags:<context>` map, so contexts and their sub-keys are
// read client-side (ParseSmartscapeTags) with no schema guessing. Returns ""
// when base is not a smartscapeNodes query (the caller then skips discovery).
// Applies to any smartscape entity list — k8s, AWS, Azure, GCP alike.
func SmartscapeTagsQuery(base string) string {
	lines := strings.Split(base, "\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "smartscapeNodes") {
		return ""
	}
	kept := []string{lines[0]}
	for _, ln := range lines[1:] {
		if !strings.HasPrefix(strings.TrimSpace(ln), "| filter ") {
			break // stop at the first non-filter stage (parse/expand/fields/summarize/sort/limit)
		}
		kept = append(kept, ln)
	}
	// fieldsRemove tolerates absent fields (validated live), so listing every
	// heavy manifest is safe across entity types.
	kept = append(kept,
		"| fieldsRemove k8s.object, aws.object, azure.object, references",
		"| limit 200")
	return strings.Join(kept, "\n")
}

// ParseSmartscapeTags reads the tag-discovery sample: for every `tags:<context>`
// map field on the sampled records it collects the union of sub-keys, returning
// raw-context → sorted keys. Contexts with no map values are omitted.
func ParseSmartscapeTags(records []map[string]any) map[string][]string {
	sets := map[string]map[string]bool{}
	for _, rec := range records {
		for k, val := range rec {
			ctx, ok := strings.CutPrefix(k, "tags:")
			if !ok {
				continue
			}
			m, ok := val.(map[string]any)
			if !ok || len(m) == 0 {
				continue
			}
			set := sets[ctx]
			if set == nil {
				set = map[string]bool{}
				sets[ctx] = set
			}
			for kk := range m {
				set[kk] = true
			}
		}
	}
	out := make(map[string][]string, len(sets))
	for ctx, set := range sets {
		keys := make([]string, 0, len(set))
		for kk := range set {
			keys = append(keys, kk)
		}
		sort.Strings(keys)
		out[ctx] = keys
	}
	return out
}
