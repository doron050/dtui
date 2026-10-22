package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/tui/theme"
)

// enrichCap bounds how many rows one batched enrichment query covers.
const enrichCap = 120

// tableView is the generic engine behind every catalog view: a DQL-backed
// table with incremental filtering and the universal drill-down vocabulary.
type tableView struct {
	ds    *dataSource
	spec  *catalog.Spec
	scope catalog.Scope

	all  []map[string]any // as fetched
	rows []map[string]any // after /-filter and sort
	scroller

	loading bool
	err     error
	seq     int
	dql     string
	elapsed string

	sortCol  int // -1 = fetch order
	sortDesc bool

	// Raw reader mode (RawReadable specs — logs): renders the flex body column
	// file-like and wrapped — content only, so it reads like a log file. Per-
	// record metadata stays one enter away (the inspector).
	reader     bool
	readerTop  int // index of the top visible record (variable-height layout)
	readerPage int // records shown in the last render (for pgup/pgdn)

	filterInput textinput.Model
	filtering   bool // input focused
	filter      string

	searches []string        // server-side | search terms ('/' + enter stacks them)
	facets   []catalog.Facet // server-side attribute filters ('f')

	// Facet manager overlay ('f'): the first stage lists the active filters
	// (edit/remove) above the attribute candidates from the fetched records'
	// keys; the value stage picks from the server's fieldsSummary.
	facetMode    facetStage
	facetInput   textinput.Model
	facetFields  []string             // attribute candidates (manager stage)
	facetField   string               // explored attribute ("" = editing a search term)
	facetOptions []catalog.FacetValue // fieldsSummary suggestions
	facetLoading bool
	facetErr     error
	facetSel     int
	facetEdit    int // index of the search/facet being edited; -1 = adding

	// hopDone marks the spec's scope-widening pre-query as resolved (or
	// skipped); refreshes then reuse the widened scope.Entities.
	hopDone bool

	// discoverDone marks the metric-roster discovery pre-query (Spec.DiscoverQuery)
	// as resolved. Reset when the segment snapshot changes — the dim universe is
	// segment-dependent, not timeframe-dependent.
	discoverDone bool
	// discoverSegSnap is the segment ref snapshot at the last discovery; used to
	// detect segment changes that require re-discovery.
	discoverSegSnap string
	// metricDims is the discovered service-metric dimension set (pass-1 results
	// minus ServiceDimExclude). Used for facet candidates and /- search.
	metricDims []string
	// rosterPrimaryTagKeys is the ordered union of declared workspace primary-tag
	// keys (ds.primaryTags) and any primary_tags.* dims found in pass 1.
	rosterPrimaryTagKeys []string

	// tagContexts is the discovered smartscape tag census: raw context
	// ("tags:"-stripped, e.g. "k8s.labels", "aws", "azure") → sorted keys.
	// Populated lazily on facet-picker open for smartscape entity lists, cached
	// until the scoped discovery query changes. nil = not yet discovered.
	tagContexts map[string][]string
	// tagRefs maps a map-facet display identity ("k8s.labels:app", "azure:Name")
	// to its raw DQL parts, built alongside facetCandidates.
	tagRefs map[string]tagRef
	// tagDiscoverKey is the discovery query at the last tag census; a change
	// (scope shift) triggers re-discovery.
	tagDiscoverKey string
	// facetMapField/facetMapKey carry the in-flight value-stage target when it
	// is a map subkey (both empty for a scalar facet).
	facetMapField string
	facetMapKey   string

	// onData observes fetched records before filtering (the query escape
	// hatch derives result columns from it).
	onData func(records []map[string]any)

	// dynCols is the column set derived from the fetched records when the
	// spec is Dynamic (the record sampler browses arbitrary tables).
	dynCols []catalog.Column

	// lensDigits marks a table nested in an entered page (set by tabSet):
	// its lens strip is then the innermost numbered strip — it renders digit
	// labels and the page routes 1-9 to it. Top-level tables stay
	// un-numbered; their digits are the global bookmarks.
	lensDigits bool

	width, height int
}

// enrichOwner tags enrichment queries so their results are told apart from
// the view's list query (both share the view's seq generation).
type enrichOwner struct{ v *tableView }

// hopOwner tags the scope-widening pre-query (Spec.Hop) that runs before
// the first list fetch.
type hopOwner struct{ v *tableView }

// discoverOwner tags the metric-roster dimension-discovery pre-query
// (Spec.DiscoverQuery). Its result feeds metricDims and rosterPrimaryTagKeys
// on the view, then triggers the real list fetch.
type discoverOwner struct{ v *tableView }

// tagDiscoverOwner tags the lazy smartscape tag-census query fired on
// facet-picker open. Its result feeds tagContexts on the view.
type tagDiscoverOwner struct{ v *tableView }

func newTableView(ds *dataSource, spec *catalog.Spec, scope catalog.Scope) *tableView {
	ti := newTextInput()
	ti.Prompt = "/"
	ti.CharLimit = 64
	fi := newTextInput()
	fi.Prompt = "⌕ "
	fi.CharLimit = 64
	return &tableView{ds: ds, spec: spec, scope: scope, filterInput: ti, facetInput: fi, sortCol: -1, facetEdit: -1}
}

func (v *tableView) Init() tea.Cmd { return v.Refresh() }

// baseColumns returns the spec-driven column set before primary-tag columns
// are spliced in: scope-derived overrides, lens overrides, dynamic columns,
// or the spec's static columns.
func (v *tableView) baseColumns() []catalog.Column {
	if v.spec.ScopeColumns != nil {
		if cols := v.spec.ScopeColumns(v.scope); cols != nil {
			return cols
		}
	}
	if len(v.spec.Lenses) > 0 {
		if cols := v.spec.LensAt(v.scope.Lens).Columns; cols != nil {
			return cols
		}
	}
	if v.spec.Dynamic && v.dynCols != nil {
		return v.dynCols
	}
	return v.spec.Columns
}

// primaryTagColumns builds one catalog.Column per active primary_tags.* key.
// Field is set to the raw "primary_tags.<key>" path; Title is the uppercased
// key name. No Enrich — these are flat fields already present in the record.
func (v *tableView) primaryTagColumns() []catalog.Column {
	keys := v.ds.primaryTags
	if len(keys) == 0 {
		return nil
	}
	cols := make([]catalog.Column, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, catalog.Column{
			Title: strings.ToUpper(k),
			Field: "primary_tags." + k,
		})
	}
	return cols
}

// columns returns the active column set: the base columns (scope/lens/dynamic
// or static) with primary-tag columns spliced in at the metric boundary (the
// index of the first column with a non-nil Enrich, or at the end when none).
func (v *tableView) columns() []catalog.Column {
	base := v.baseColumns()
	tagCols := v.primaryTagColumns()
	if len(tagCols) == 0 {
		return base
	}
	// Metric boundary: first column whose parent spec carries Enrich is not
	// identifiable from Column itself (Enrich is on Spec, not Column).
	// Instead we find the first SparkColumn-style column — those have Width>0
	// and a non-nil Sort set to the enrichment key — but that is fragile.
	// The simpler and correct heuristic: find the first column whose Field
	// starts with "__enrich." (enrichment columns are stored under that prefix
	// after applyEnrichment) — except those don't appear in spec.Columns.
	//
	// The design says: "before the first column whose Enrich is non-nil".
	// Since Column has no Enrich field, we use the spec-level Enrich to find
	// the metric boundary: if the spec has Enrich, place tag columns just
	// before the columns whose Title matches the sparkline aliases (req, cpu…).
	// As a practical heuristic we place them before the first column that has
	// a non-zero Width AND a non-empty Field that begins with a known enrichment
	// alias (from spec.Enrich.Series). When no such column exists, tags go at
	// the end (before the last fixed-width columns).
	//
	// Implementation concern #1: The Column struct has no Enrich field. The
	// boundary is approximated by matching against spec.Enrich.Series names via
	// EnrichKey. Columns whose Title matches a series alias from spec.Enrich
	// are treated as the metric boundary.
	boundary := len(base)
	if v.spec.Enrich != nil {
		seriesSet := make(map[string]bool, len(v.spec.Enrich.Series))
		for _, s := range v.spec.Enrich.Series {
			seriesSet[strings.ToUpper(s)] = true
		}
		for i, c := range base {
			if seriesSet[c.Title] {
				boundary = i
				break
			}
		}
	}
	result := make([]catalog.Column, 0, len(base)+len(tagCols))
	result = append(result, base[:boundary]...)
	result = append(result, tagCols...)
	result = append(result, base[boundary:]...)
	return result
}

// setLens activates a lens by index (wrapping when asked — the brackets
// cycle) and refetches. Sort resets: lenses may carry different columns.
func (v *tableView) setLens(i int, wrap bool) tea.Cmd {
	n := len(v.spec.Lenses)
	if n == 0 {
		return nil
	}
	if wrap {
		i = ((i % n) + n) % n
	}
	if i < 0 || i >= n || i == v.scope.Lens {
		return nil
	}
	v.scope.Lens = i
	v.sortCol = -1
	v.cursor, v.offset = 0, 0
	l := v.spec.Lenses[i]
	return tea.Batch(v.Refresh(), markHistory, status(fmt.Sprintf("lens: %s — %s", l.Name, l.Desc)))
}

// composeDQL renders the list query: the spec's scope query with the server
// searches and bucket facets injected after the source (DQL rejects search
// later in the pipeline; the bucket filter prunes physical reads at the
// source), the record's bucket projected in on bucket-backed views (skipped
// for API views — their query feeds an analyzer, not the table), any
// primary-tag fieldsAdd stages for ProjectPrimaryTags specs, and the
// remaining facet filters before the sort/limit tail. API-backed views
// without a query render "".
//
// For MetricRoster specs the path differs: facets and searches embed into
// filter:{} pre-aggregation via Scope.FilterConds rather than being injected
// as | filter / | search stages. Primary-tag keys and discovered dims are
// threaded via Scope so the spec's Query closure can use them.
func (v *tableView) composeDQL() string {
	if v.spec.Query == nil {
		return ""
	}
	if v.spec.MetricRoster {
		return v.composeRosterDQL()
	}
	dql := catalog.ComposeQuery(v.spec.Query(v.scope), v.searches, v.facets, v.spec.API == "")
	// Primary-tag column injection. Two paths depending on query shape:
	//   InjectPrimaryTags set → spec uses | fields/| summarize; hook embeds
	//     keys inside the projection boundary before the body is finalized.
	//   InjectPrimaryTags nil → spec uses | fieldsRemove; primary_tags.* still
	//     exists at this point, so post-hoc | fieldsAdd stages suffice.
	// See Spec.ProjectPrimaryTags doc and tui.md "Primary-tag columns and projection".
	if v.spec.ProjectPrimaryTags && len(v.ds.primaryTags) > 0 {
		if v.spec.InjectPrimaryTags != nil {
			dql = v.spec.InjectPrimaryTags(dql, v.ds.primaryTags)
		} else {
			stages := make([]string, 0, len(v.ds.primaryTags))
			for _, k := range v.ds.primaryTags {
				stages = append(stages, "| fieldsAdd "+catalog.EscapeField("primary_tags."+k))
			}
			dql = catalog.InjectStages(dql, stages)
		}
	}
	return dql
}

// composeRosterDQL builds the DQL for a MetricRoster spec by embedding facets
// and searches into filter:{} pre-aggregation, and threading discovered keys
// and dims into the scope for the Query closure.
func (v *tableView) composeRosterDQL() string {
	// Use the discovered primary-tag keys if available, else fall back to the
	// declared workspace keys.
	ptKeys := v.rosterPrimaryTagKeys
	if len(ptKeys) == 0 {
		ptKeys = v.ds.primaryTags
	}

	// Build filter conditions from active facets and searches.
	var conds []string
	for _, f := range v.facets {
		conds = append(conds, f.Condition())
	}
	for _, term := range v.searches {
		conds = append(conds, catalog.ContainsSearchCondition(term, v.metricDims))
	}

	s := v.scope
	s.PrimaryTagKeys = ptKeys
	s.MetricDims = v.metricDims
	s.FilterConds = conds
	return v.spec.Query(s)
}

func (v *tableView) Refresh() tea.Cmd {
	v.seq++
	v.loading = true
	v.err = nil
	if cmd := v.discoverCmd(); cmd != nil {
		return cmd
	}
	if cmd := v.hopCmd(); cmd != nil {
		return cmd
	}
	return v.fetch()
}

// segSnap builds a cheap string snapshot of the current segment refs for
// change detection in the discovery cache.
func (v *tableView) segSnap() string {
	ids := make([]string, len(v.ds.segments))
	for i, s := range v.ds.segments {
		ids[i] = s.ID
	}
	return strings.Join(ids, ",")
}

// discoverCmd issues the metric-roster discovery pre-query once per
// segment combination. Returns nil when the spec has no DiscoverQuery or
// discovery is already current.
func (v *tableView) discoverCmd() tea.Cmd {
	if v.spec.DiscoverQuery == nil {
		return nil
	}
	// Re-run if segments changed since last discovery.
	snap := v.segSnap()
	if v.discoverDone && v.discoverSegSnap == snap {
		return nil
	}
	v.discoverDone = false
	v.metricDims = nil
	v.rosterPrimaryTagKeys = nil
	q := v.spec.DiscoverQuery(v.scope)
	if q == "" {
		v.discoverDone = true
		v.discoverSegSnap = snap
		return nil
	}
	v.dql = ""
	return v.ds.query(discoverOwner{v}, v.seq, q)
}

// tagDiscoverCmd issues the lazy smartscape tag-census query (one per scoped
// query, cached). Returns nil unless the view is a smartscapeNodes-backed
// entity list whose census is stale/missing.
func (v *tableView) tagDiscoverCmd() tea.Cmd {
	if v.spec.API != "" || v.spec.MetricRoster || v.spec.Kind != catalog.KindEntity {
		return nil
	}
	q := catalog.SmartscapeTagsQuery(v.spec.Query(v.scope))
	if q == "" {
		return nil
	}
	if v.tagContexts != nil && v.tagDiscoverKey == q {
		return nil // census current for this scope
	}
	v.tagDiscoverKey = q
	return v.ds.query(tagDiscoverOwner{v}, v.seq, q)
}

// fetch composes and issues the list query for the current scope.
func (v *tableView) fetch() tea.Cmd {
	v.dql = v.composeDQL()
	if v.spec.API != "" {
		return v.ds.call(v, v.seq, v.spec.API, v.scope, v.dql)
	}
	if v.spec.RecordCap > 0 {
		return v.ds.queryCapped(v, v.seq, v.dql, v.spec.RecordCap)
	}
	return v.ds.query(v, v.seq, v.dql)
}

// hopCmd issues the spec's scope-widening pre-query once, before the first
// fetch (nil = no hop needed, fetch directly).
func (v *tableView) hopCmd() tea.Cmd {
	if v.spec.Hop == nil || v.hopDone {
		return nil
	}
	q := v.spec.Hop.Query(v.scope)
	if q == "" {
		v.hopDone = true
		return nil
	}
	v.dql = "" // the list query composes once the widened scope is known
	return v.ds.query(hopOwner{v}, v.seq, q)
}

func (v *tableView) SetTimeframe(tf catalog.Timeframe) tea.Cmd {
	v.scope.Timeframe = tf
	return v.Refresh()
}

func (v *tableView) InputActive() bool { return v.filtering || v.facetMode != facetOff }

// Busy reports whether the list query is in flight (animates the spinner).
func (v *tableView) Busy() bool { return v.loading || v.facetLoading }

// RowCount reports the fetched row count for tab badges — valid only once a
// fetch has completed cleanly.
func (v *tableView) RowCount() (int, bool) {
	return len(v.all), v.seq > 0 && !v.loading && v.err == nil
}

func (v *tableView) Crumb() string {
	label := v.spec.Name
	// The default lens is the view's understood state; only deviations show.
	if len(v.spec.Lenses) > 0 && v.scope.Lens > 0 {
		label += "·" + v.spec.LensAt(v.scope.Lens).Name
	}
	if v.scope.Arg != "" {
		label += fmt.Sprintf(" (%s)", strings.ToLower(v.scope.Arg))
	}
	if v.scope.Entity != nil && v.spec.UsesScope() {
		name := v.scope.Entity.Name
		if name == "" {
			name = v.scope.Entity.ID
		}
		label += fmt.Sprintf(" (%s)", name)
	}
	if v.scope.Pattern != "" {
		label += " [pattern]"
	}
	// Server-side narrowing is scope the query kept — the breadcrumb (and the
	// history trail snapshotted from it) must say so.
	var narrow []string
	for _, s := range v.searches {
		narrow = append(narrow, "⌕"+s)
	}
	for _, f := range v.facets {
		narrow = append(narrow, f.Label())
	}
	if len(narrow) > 0 {
		label += " [" + strings.Join(narrow, " ") + "]"
	}
	return label
}

// setFilter pre-fills the incremental filter (command-bar args, home jumps).
func (v *tableView) setFilter(f string) {
	v.filterInput.SetValue(f)
	v.filter = f
	v.applyFilter()
}

func (v *tableView) Echo() string {
	if v.spec.API != "" && v.spec.Echo != nil {
		return v.spec.Echo(v.scope, v.dql)
	}
	if v.dql == "" {
		return ""
	}
	return v.ds.echoQuery(v.dql)
}

// DQL reveals the view's generated query (ctrl+q).
func (v *tableView) DQL() string { return v.dql }

func (v *tableView) Hints() []keyHint {
	switch v.facetMode {
	case facetFieldStage:
		hints := []keyHint{{"type", "find attribute"}, {"↑/↓", "move"}, {"enter", "add / edit"}}
		if len(v.searches)+len(v.facets) > 0 {
			hints = append(hints, keyHint{"ctrl+x", "remove"})
		}
		return append(hints, keyHint{"esc", "close"})
	case facetValueStage:
		if v.facetField == "" {
			return []keyHint{{"type", "edit term"}, {"enter", "apply"}, {"esc", "back"}}
		}
		return []keyHint{{"type", "narrow · * = pattern"}, {"↑/↓", "move"}, {"enter", "apply filter"}, {"esc", "back"}}
	}
	if v.filtering {
		return []keyHint{{"type", "filter rows"}, {"enter", "add server search"}, {"alt+enter", "replace"}, {"esc", "clear"}}
	}
	var hints []keyHint
	if len(v.spec.Lenses) > 0 {
		if v.lensDigits {
			hints = append(hints, keyHint{"1-9", "lens"})
		} else {
			hints = append(hints, keyHint{"tab", "lens"})
		}
	}
	if !v.ds.previewOn() {
		hints = append(hints, keyHint{"P", "preview"})
	}
	if h, sentinel := enterHandlers[v.spec.EnterTarget]; sentinel {
		if label := h.hint(v); label != "" {
			hints = append(hints, keyHint{"enter", label})
		}
	} else {
		switch {
		case v.spec.EnterTarget != "":
			hints = append(hints, keyHint{"enter", v.spec.EnterTarget})
			if v.spec.EnterArg == nil && v.spec.Kind == catalog.KindEntity {
				hints = append(hints, keyHint{"d", "details"})
			}
		case v.spec.Kind == catalog.KindEntity && v.spec.Entity != nil:
			hints = append(hints, keyHint{"enter", "details"}, keyHint{"d", "record"})
		default:
			hints = append(hints, keyHint{"enter", "inspect"})
		}
	}
	// Stable order for the drill keys.
	for _, k := range []string{"l", "s", "m", "p", "v", "u", "e", "a"} {
		if target, ok := v.spec.Drills[k]; ok {
			hints = append(hints, keyHint{k, target})
		}
	}
	hints = append(hints, keyHint{"/", "filter"})
	if v.spec.API == "" {
		hints = append(hints, keyHint{"f", "facets"})
	}
	if len(v.searches) > 0 || len(v.facets) > 0 {
		hints = append(hints, keyHint{"F", "clear facets"})
	}
	hints = append(hints, keyHint{"J/K", "sort"})
	if v.spec.RawReadable {
		if v.reader {
			hints = append(hints, keyHint{"w", "list"})
		} else {
			hints = append(hints, keyHint{"w", "raw reader"})
		}
		hints = append(hints, keyHint{"T", "head/tail"}, keyHint{"+", "more"})
	}
	return hints
}

func (v *tableView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case bodySizeMsg:
		v.width, v.height = msg.width, msg.height
		return nil

	case dataMsg:
		if do, ok := msg.owner.(discoverOwner); ok && do.v == v {
			if msg.seq != v.seq {
				return nil
			}
			v.discoverDone = true
			v.discoverSegSnap = v.segSnap()
			// Discovery failure/empty degrades: keep the declared primary-tag keys
			// and an empty dim set — the list still fetches, facets are empty.
			if msg.err == nil && len(msg.records) > 0 {
				v.metricDims, v.rosterPrimaryTagKeys = catalog.ScanMetricDims(
					msg.records, catalog.ServiceDimExclude, v.ds.primaryTags)
			} else {
				v.rosterPrimaryTagKeys = v.ds.primaryTags
			}
			return v.fetch()
		}
		if ho, ok := msg.owner.(hopOwner); ok && ho.v == v {
			if msg.seq != v.seq {
				return nil
			}
			v.hopDone = true
			// A hop failure degrades to the unwidened scope — the entity's
			// own filter arms still match directly-stamped records.
			if msg.err == nil && v.spec.Hop != nil {
				v.scope.Entities = v.spec.Hop.Apply(v.scope, msg.records)
				if n := len(v.scope.Entities) - 1; n > 0 {
					return tea.Batch(v.fetch(),
						status(fmt.Sprintf("scope widened to %d runtime entities — logs rarely carry service IDs", n)))
				}
			}
			return v.fetch()
		}
		if to, ok := msg.owner.(tagDiscoverOwner); ok && to.v == v {
			// Census is scope-based, not seq-based — accept regardless of seq,
			// but only for the current scope: a late result from a prior scope
			// (msg.dql != the active discovery key) would poison the cache and
			// suppress re-discovery. Drop it.
			if msg.dql != v.tagDiscoverKey {
				return nil
			}
			// Failure/empty degrades to an empty (non-nil) census: no tag groups.
			if msg.err == nil {
				v.tagContexts = catalog.ParseSmartscapeTags(msg.records)
			} else if v.tagContexts == nil {
				v.tagContexts = map[string][]string{}
			}
			// Rebuild candidates so the just-discovered groups appear if the
			// manager is still open.
			if v.facetMode == facetFieldStage {
				v.facetFields = v.facetCandidates()
			}
			return nil
		}
		if _, ok := msg.owner.(derivedOwner); ok {
			// A derived-field lookup landed (the app filled the shared cache and
			// broadcast). Re-enrich this view's rows; re-filter only if a value
			// actually changed — a no-op for views with no matching records, so
			// unrelated views never lose their cursor/scroll to a needless
			// applyFilter.
			if v.ds.enrichRecords(v.all) {
				// Re-run the client filter/sort so the newly filled values fold
				// into membership and ordering, not just cell text.
				v.applyFilter()
			}
			return nil
		}
		if eo, ok := msg.owner.(enrichOwner); ok && eo.v == v {
			if msg.seq == v.seq && msg.err == nil {
				v.applyEnrichment(msg.records)
			}
			return nil // enrichment failures degrade to blank cells
		}
		if fo, ok := msg.owner.(facetOwner); ok && fo.v == v {
			// Only the exploration the picker is currently standing on counts;
			// a result for a superseded field or a closed picker is stale.
			if msg.seq != v.seq || v.facetMode != facetValueStage || fo.field != v.facetField {
				return nil
			}
			v.facetLoading = false
			v.facetErr = msg.err
			if msg.err == nil {
				v.facetOptions = catalog.ParseFieldsSummary(msg.records)
			}
			return nil
		}
		if msg.owner != any(v) || msg.seq != v.seq {
			return nil
		}
		v.loading = false
		v.err = msg.err
		if msg.err == nil {
			v.all = msg.records
			// Stamp any warm derived fields (e.g. resolved Azure subscription
			// names) in place — no-op when a cache is cold or no row matches; the
			// derivedOwner broadcast re-enriches once a lookup lands.
			v.ds.enrichRecords(v.all)
			v.elapsed = catalog.FormatDuration(msg.elapsed)
			if v.spec.Dynamic {
				// nil on empty keeps the spec's declared fallback columns —
				// deriveColumns would pin a "(no rows)" placeholder.
				v.dynCols = nil
				if len(msg.records) > 0 {
					v.dynCols = deriveColumns(msg.records)
					// Timestamped tables sample newest-first client-side. The
					// server query cannot sort blindly — sorting on a field
					// the table lacks is a hard FIELD_DOES_NOT_EXIST
					// (validated live) — so the fetched page orders here.
					if v.sortCol < 0 {
						for i, c := range v.dynCols {
							if c.Field == "timestamp" {
								v.sortCol, v.sortDesc = i, true
								break
							}
						}
					}
				}
			}
			if v.onData != nil {
				v.onData(msg.records)
			}
			v.applyFilter()
			return v.enrich()
		}
		return nil

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return nil
}

// enrich issues the batched sparkline query for the fetched page (capped).
func (v *tableView) enrich() tea.Cmd {
	es := v.spec.Enrich
	if es == nil || len(v.all) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var keys []string
	for _, rec := range v.all {
		if len(keys) >= enrichCap {
			break
		}
		if k := es.Key(rec); k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return v.ds.query(enrichOwner{v}, v.seq, es.Query(v.scope.Timeframe, keys))
}

// applyEnrichment joins batched timeseries results onto the fetched rows.
func (v *tableView) applyEnrichment(results []map[string]any) {
	es := v.spec.Enrich
	if es == nil {
		return
	}
	byKey := map[string]map[string]any{}
	for _, res := range results {
		if k := catalog.Str(res, es.By); k != "" {
			byKey[k] = res
		}
	}
	for _, rec := range v.all {
		res := byKey[es.Key(rec)]
		if res == nil {
			continue
		}
		for _, alias := range es.Series {
			if series, ok := res[alias]; ok {
				rec[catalog.EnrichKey(alias)] = series
			}
		}
	}
	v.applyFilter() // re-render (and re-sort: sparkline columns sort by last value)
}

func (v *tableView) handleKey(msg tea.KeyMsg) tea.Cmd {
	if v.facetMode != facetOff {
		return v.handleFacetKey(msg)
	}
	if v.filtering {
		switch msg.String() {
		case "enter", "alt+enter":
			v.filtering = false
			v.filterInput.Blur()
			// Enter promotes the typed text to a server-side search over the
			// whole dataset (any field, case-insensitive) — the client filter
			// only ever narrowed the fetched page's visible columns. The
			// client filter clears: server-matched rows may match on fields
			// no column shows, and hiding them again would lie. Terms stack
			// (each is a chained | search stage); alt+enter replaces the
			// active terms with this one instead.
			if term := strings.TrimSpace(v.filter); term != "" {
				// A view without a query has nothing to inject a server
				// search into — the client filter stays as the narrowing.
				if v.spec.Query == nil {
					return status(fmt.Sprintf("client filter %q (no server search on %s)", term, v.spec.Name))
				}
				v.filterInput.SetValue("")
				v.filter = ""
				v.applyFilter()
				return v.addSearch(term, msg.String() == "alt+enter")
			}
		case "esc":
			v.filtering = false
			v.filterInput.Blur()
			v.filterInput.SetValue("")
			v.filter = ""
			v.applyFilter()
		default:
			var cmd tea.Cmd
			v.filterInput, cmd = v.filterInput.Update(msg)
			v.filter = v.filterInput.Value()
			v.applyFilter()
			return cmd
		}
		return nil
	}

	if v.reader {
		if v.handleReaderKey(msg.String()) {
			return nil
		}
	} else if v.scroller.handleKey(msg.String(), len(v.rows), v.pageSize()) {
		return nil
	}

	switch msg.String() {
	case "J": // next sort column (wraps through "no sort")
		v.sortCol++
		if v.sortCol >= len(v.columns()) {
			v.sortCol = -1
		}
		v.sortDesc = v.defaultDesc()
		v.applyFilter()
		if v.sortCol < 0 {
			return status("sort: fetch order")
		}
		return status(fmt.Sprintf("sort: %s %s", v.columns()[v.sortCol].Title, sortArrow(v.sortDesc)))
	case "K": // toggle direction of the current sort column
		if v.sortCol < 0 {
			return status("no sort column (J selects one)")
		}
		v.sortDesc = !v.sortDesc
		v.applyFilter()
		return status(fmt.Sprintf("sort: %s %s", v.columns()[v.sortCol].Title, sortArrow(v.sortDesc)))
	case "]":
		if len(v.spec.Lenses) > 0 {
			return v.setLens(v.scope.Lens+1, true)
		}
		return status("no lens strip on this view")
	case "[":
		if len(v.spec.Lenses) > 0 {
			return v.setLens(v.scope.Lens-1, true)
		}
		return status("no lens strip on this view")
	case "tab":
		// tab cycles the view's primary strip; on a plain table that is the
		// lens strip. (A table nested in a detail tab never sees tab — the
		// page's tab bar claims it first.)
		if len(v.spec.Lenses) > 0 {
			return v.setLens(v.scope.Lens+1, true)
		}
	case "shift+tab":
		if len(v.spec.Lenses) > 0 {
			return v.setLens(v.scope.Lens-1, true)
		}
	case "/":
		v.filtering = true
		v.filterInput.Focus()
		return textinput.Blink
	case "f":
		return v.openFacets()
	case "F":
		return v.clearFacets()
	case "esc":
		if v.filter != "" {
			v.filterInput.SetValue("")
			v.filter = ""
			v.applyFilter()
			return claimKey
		}
		if len(v.searches) > 0 || len(v.facets) > 0 {
			return v.clearFacets()
		}
		return nil // app pops the stack
	case "enter":
		rec := v.selected()
		if rec == nil {
			return nil
		}
		// Containment navigation (k9s-style) when the spec declares it:
		// bespoke sentinels resolve through the enterHandlers registry
		// (waterfall, chart, pattern-logs, …); anything else is a catalog
		// view name (workload → its pods, census row → typed list).
		if h, ok := enterHandlers[v.spec.EnterTarget]; ok {
			return h.open(v, rec)
		}
		if v.spec.EnterTarget != "" {
			if target := catalog.Lookup(v.spec.EnterTarget); target != nil {
				scope := catalog.Scope{Timeframe: v.scope.Timeframe}
				if v.spec.EnterArg != nil {
					if scope.Arg = v.spec.EnterArg(rec); scope.Arg == "" {
						return statusErr("row is not enterable — this data object cannot be fetched")
					}
				} else if e := v.entityOf(rec); e != nil {
					scope.Entity = e
				}
				return func() tea.Msg { return pushViewMsg{spec: target, scope: scope} }
			}
		}
		// Entity rows open the tabbed detail page; signal rows (logs,
		// events) open the record inspector — except Davis problems, which
		// get the bespoke problem page ('d' keeps the raw record). A row the
		// spec marks Inspect (a synthetic scope row with no node of its own —
		// a fleet module) always opens the static inspector, never navigating
		// into its scope entity.
		if v.spec.Inspect != nil && v.spec.Inspect(rec) {
			return v.inspect(rec)
		}
		if v.spec.Kind == catalog.KindEntity {
			if e := v.entityOf(rec); e != nil {
				entity := *e
				return func() tea.Msg { return detailMsg{entity: entity, rec: rec} }
			}
		}
		if catalog.IsProblem(rec) {
			return func() tea.Msg { return problemMsg{rec: rec} }
		}
		if catalog.IsVulnerability(rec) {
			return func() tea.Msg { return vulnMsg{rec: rec} }
		}
		return v.inspect(rec)
	case "w":
		if !v.spec.RawReadable {
			return status("no raw reader on this view")
		}
		v.reader = !v.reader
		v.readerTop = v.cursor
		if v.reader {
			return status("raw reader on — enter inspects the record")
		}
		return status("raw reader off")
	case "T":
		if !v.spec.RawReadable {
			break
		}
		v.scope.LogHead = !v.scope.LogHead
		v.cursor, v.offset, v.readerTop = 0, 0, 0
		ord := "tail (newest first)"
		if v.scope.LogHead {
			ord = "head (oldest first)"
		}
		return tea.Batch(v.Refresh(), status("showing "+ord))
	case "+", "=":
		if !v.spec.RawReadable {
			break
		}
		cur := v.logLimit()
		next := nextLogLimit(cur)
		if next == cur {
			return status(fmt.Sprintf("already at the %d-line cap", cur))
		}
		v.scope.Limit = next
		return tea.Batch(v.Refresh(), status(fmt.Sprintf("loading up to %d lines", next)))
	case "d":
		rec := v.selected()
		if rec == nil {
			return nil
		}
		// Views whose enter follows containment keep the detail page on d.
		if v.spec.EnterTarget != "" && v.spec.EnterArg == nil && v.spec.Kind == catalog.KindEntity {
			if e := v.entityOf(rec); e != nil {
				entity := *e
				return func() tea.Msg { return detailMsg{entity: entity, rec: rec} }
			}
		}
		return v.inspect(rec)
	default:
		if target, ok := v.spec.Drills[msg.String()]; ok {
			return v.drill(target)
		}
	}
	return nil
}

// Selection exposes the highlighted row and its entity for app-level actions
// (pin, yank, open in browser, relations).
func (v *tableView) Selection() (map[string]any, *catalog.Entity) {
	rec := v.selected()
	if rec == nil {
		return nil, nil
	}
	return rec, v.entityOf(rec)
}

func (v *tableView) selected() map[string]any {
	if v.cursor >= 0 && v.cursor < len(v.rows) {
		return v.rows[v.cursor]
	}
	return nil
}

func (v *tableView) pageSize() int {
	chrome := 3
	if len(v.spec.Lenses) > 0 {
		chrome++ // the lens strip line
	}
	if v.previewBottom() {
		chrome += previewBottomH + 1 // panel plus its rule
	}
	if v.height > chrome {
		return v.height - chrome
	}
	return 10
}

// applyFilter recomputes rows from the fetched page (client-side, consistent
// with the design: server-side narrowing is what scope is for).
func (v *tableView) applyFilter() {
	if v.filter == "" {
		v.rows = v.all
	} else {
		needle := strings.ToLower(v.filter)
		v.rows = nil
		for _, rec := range v.all {
			var cells []string
			for _, c := range v.columns() {
				cells = append(cells, c.Text(rec))
			}
			if strings.Contains(strings.ToLower(strings.Join(cells, " ")), needle) {
				v.rows = append(v.rows, rec)
			}
		}
	}
	v.sortRows()
	// A parked cursor may now point past the narrowed rows.
	v.scroller.move(0, len(v.rows), v.pageSize())
}

func (v *tableView) View(width, height int) string {
	v.width, v.height = width, height

	if v.facetMode != facetOff {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Position(0.2),
			theme.OverlayBox.Render(v.renderFacetPicker()))
	}
	if v.reader {
		return v.renderReader(width, height)
	}
	return previewLayout(v.ds.previewOn(), width, height, v.renderTable, v.previewLines)
}

func (v *tableView) renderTable(width, height int) string {
	var b strings.Builder

	// Status/filter line.
	head := " " + theme.Count.Render(fmt.Sprintf("%d", len(v.rows)))
	if len(v.rows) != len(v.all) {
		head += theme.Dim.Render(fmt.Sprintf("/%d", len(v.all)))
	}
	head += theme.Dim.Render(" rows")
	if v.elapsed != "" {
		head += theme.Dim.Render(" · " + v.elapsed)
	}
	if v.sortCol >= 0 && v.sortCol < len(v.columns()) {
		head += theme.Dim.Render(" · ") +
			theme.SortMark.Render(sortArrow(v.sortDesc)+" "+v.columns()[v.sortCol].Title)
	}
	for _, s := range v.searches {
		head += "  " + theme.Badge.Render("⌕ "+s)
	}
	for _, f := range v.facets {
		head += " " + theme.Badge.Render(f.Label())
	}
	if v.filtering || v.filter != "" {
		head += "  " + v.filterInput.View()
	}
	if v.loading {
		head = " " + theme.Spinner.Render(theme.Spin()) + head
	}
	b.WriteString(ansi.Truncate(head, width, "…"))
	b.WriteString("\n")

	// Lens strip: the view's quick subsets (tab and the brackets cycle).
	// Nested in an entered page it is the innermost numbered strip, so it
	// carries digit labels and the digits address it; at the top level it
	// stays un-numbered — digits are the global bookmarks there.
	chrome := 2
	if n := len(v.spec.Lenses); n > 0 {
		chrome = 3
		labels := make([]string, n)
		for i, l := range v.spec.Lenses {
			name := l.Name
			if v.lensDigits {
				name = fmt.Sprintf("%d %s", i+1, name)
			}
			if i == v.scope.Lens {
				labels[i] = theme.TabActive.Render(name)
			} else {
				labels[i] = theme.TabInactive.Render(name)
			}
		}
		b.WriteString(ansi.Truncate(" "+strings.Join(labels, "  "), width, "…"))
		b.WriteString("\n")
	}

	if v.err != nil {
		b.WriteString("\n" + theme.Error.Render("✗ "+wrap(v.err.Error(), width-2)))
		return b.String()
	}

	// One gutter cell marks the cursor row; columns share the rest.
	widths := v.columnWidths(width - 1)

	// Header row.
	var hdr []string
	for i, c := range v.columns() {
		title := c.Title
		if i == v.sortCol {
			title += sortArrow(v.sortDesc)
		}
		hdr = append(hdr, cell(title, widths[i], c.Right))
	}
	b.WriteString(" " + theme.TableHeader.Render(ansi.Truncate(strings.Join(hdr, "  "), width-1, "")))
	b.WriteString("\n")

	visible := height - chrome
	if visible < 1 {
		visible = 1
	}
	end := v.offset + visible
	if end > len(v.rows) {
		end = len(v.rows)
	}
	for i := v.offset; i < end; i++ {
		b.WriteString(v.renderRow(i, widths, width))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	if len(v.rows) == 0 && !v.loading {
		if v.filter != "" && len(v.all) > 0 {
			b.WriteString(theme.Dim.Render(fmt.Sprintf("  no rows match %q (%d fetched — esc clears the filter)", v.filter, len(v.all))))
		} else {
			empty := "∅ no data in timeframe (last " + v.scope.Timeframe.Label + ")"
			if len(v.spec.Lenses) > 0 {
				what := v.spec.LensAt(v.scope.Lens).Name
				if what == "all" {
					what = "data" // "no all in timeframe" reads broken
				}
				empty = fmt.Sprintf("∅ no %s in timeframe (last %s) — [/] switches lens",
					what, v.scope.Timeframe.Label)
			}
			b.WriteString("\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, theme.Dim.Render(empty)))
		}
	}
	return b.String()
}

func (v *tableView) renderRow(i int, widths []int, width int) string {
	rec := v.rows[i]
	selected := i == v.cursor

	var cells []string
	for ci, c := range v.columns() {
		text := cell(c.Text(rec), widths[ci], c.Right)
		if !selected && c.Class != nil {
			if class := c.Class(strings.TrimSpace(text)); class != "" {
				text = theme.Class(class, text)
			}
		}
		cells = append(cells, text)
	}
	row := ansi.Truncate(strings.Join(cells, "  "), width-1, "…")
	if selected {
		// Selected rows drop per-cell colors so the highlight reads as one
		// bar: an accent gutter mark plus a background wash.
		return theme.Gutter.Render("▌") + theme.Selected.Render(pad(row, width-1))
	}
	return " " + row
}

// columnWidths assigns fixed widths and shares the remainder among flex columns.
func (v *tableView) columnWidths(total int) []int {
	cols := v.columns()
	widths := make([]int, len(cols))
	fixed, flexCount := 0, 0
	for i, c := range cols {
		if c.Width > 0 {
			widths[i] = c.Width
			fixed += c.Width
		} else {
			flexCount++
		}
	}
	gaps := 2 * (len(cols) - 1)
	remaining := total - fixed - gaps
	if flexCount > 0 {
		per := remaining / flexCount
		if per < 8 {
			per = 8
		}
		for i, c := range cols {
			if c.Width == 0 {
				widths[i] = per
			}
		}
	}
	return widths
}

// flatten collapses line breaks and tabs to single spaces — a multi-line
// value (SQL statements, log content) must not shear a table row apart.
func flatten(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") {
		return s
	}
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t' || r == ' '
	}), " ")
}

// pad truncates or pads s to exactly w display cells (ANSI-aware).
func pad(s string, w int) string {
	s = ansi.Truncate(flatten(s), w, "…")
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// cell renders a table cell at exactly w cells, right-aligning when asked.
func cell(s string, w int, right bool) string {
	if !right {
		return pad(s, w)
	}
	s = ansi.Truncate(flatten(s), w, "…")
	if gap := w - lipgloss.Width(s); gap > 0 {
		s = strings.Repeat(" ", gap) + s
	}
	return s
}

// wrap soft-wraps text to a width using lipgloss.
func wrap(s string, w int) string {
	if w <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(w).Render(s)
}
