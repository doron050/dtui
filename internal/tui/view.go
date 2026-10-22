package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// viewModel is the contract every screen on the breadcrumb stack implements.
// Views mutate in place (pointer receivers) and are kept alive when covered,
// so esc restores them with data and cursor intact.
type viewModel interface {
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	// View renders the body at the given size (chrome excluded).
	View(width, height int) string
	// Crumb is the breadcrumb label, including scope where relevant.
	Crumb() string
	// Hints lists the key bindings shown in the footer for this view.
	Hints() []keyHint
	// Echo is the CLI equivalent of what the view shows ("" = none).
	Echo() string
	// InputActive reports whether a text input owns the keyboard, which
	// suppresses global single-letter keys like q.
	InputActive() bool
	Refresh() tea.Cmd
	SetTimeframe(tf catalog.Timeframe) tea.Cmd
}

type keyHint struct {
	Key  string
	Desc string
}

// busyReporter is implemented by views that know when they are waiting on
// data; the app animates the loading spinner while the visible view is busy.
type busyReporter interface{ Busy() bool }

// --- navigation messages ------------------------------------------------------

// pushViewMsg opens a catalog view. replace resets the stack (command-bar
// jumps); otherwise the view is pushed as a drill-down. filter pre-fills the
// incremental table filter (command-bar arguments, home-panel jumps);
// searches/facets pre-fill the server-side narrowing (the patterns drill
// analyzes exactly the list the user was looking at).
type pushViewMsg struct {
	spec     *catalog.Spec
	scope    catalog.Scope
	replace  bool
	filter   string
	searches []string
	facets   []catalog.Facet
}

// NavigateMsg is an external deep-link request: the wasm host (a browser
// extension recognizing an id on some other web page) asks dtui to open a
// curated view — or the trace waterfall — pre-narrowed by server-side facets.
// It is pure navigation: it composes into the same pushViewMsg/waterfallMsg the
// in-app drill-downs use, so the read-only contract (ADR-0011) is unaffected.
// Exported because it crosses the package boundary from cmd/web (js/wasm).
type NavigateMsg struct {
	// Target is a catalog view name/alias (e.g. "pods", "genai"), or the
	// pseudo-target "trace" to open the waterfall for a trace id.
	Target string
	// Facets are server-side field==value filters applied to the target view
	// (the "pre-applied filter"). Ignored by the "trace" pseudo-target.
	Facets []NavFacet
	// ID is the identifier for pseudo-targets ("trace" → trace id). When empty
	// for "trace", the first facet's Value is used.
	ID string
	// Lens optionally selects a lens on the target view by name (e.g. "genai"
	// on the traces view). Ignored if the view has no matching lens.
	Lens string
	// Timeframe optionally overrides the window as a label ("7d", "30d", "2h").
	// Empty or unparseable falls back to the current session timeframe.
	Timeframe string
	// Context optionally names the environment/context the deep link targets.
	// It is a resolved context name (the wasm host maps a portable tenant URL
	// to the local context via host match before sending this). When set and
	// different from the session's current context, dtui switches to it first
	// (via Options.SwitchContext) and applies the rest of the navigation once the
	// switch completes. Empty stays put.
	Context string
}

// NavFacet is a single server-side field==value filter for NavigateMsg. It maps
// directly to a catalog.Facet; kept separate so callers outside this package
// need not import the catalog package.
type NavFacet struct{ Field, Value string }

// waterfallMsg opens the span waterfall for one trace. focusSpanID anchors
// the cursor on the span the jump came from ("" = trace root). at is that
// span's start_time when known — the waterfall scopes its fetch to a tight
// window around it so drilling in doesn't scan the whole global timeframe just
// to filter down to one trace (which hits Grail's scan limit on a large
// tenant). Zero at (e.g. the manual `trace <id>` command) falls back to the
// inherited global window.
type waterfallMsg struct {
	traceID     string
	focusSpanID string
	at          time.Time
}

// timelineMsg opens the session timeline (the RUM waterfall) for one
// session; rec is the sessions-list row for the header (nil = unknown).
type timelineMsg struct {
	sessionID string
	rec       map[string]any
}

// navMsg opens the smartscape navigator: the overview (zero value), the type
// browser (typ), walk mode rooted at an entity (root; wins over typ), or the
// name-resolution browser (search — a unique match walks straight to the
// entity). replace resets the stack (command-bar jumps).
type navMsg struct {
	typ     string
	root    *catalog.Entity
	search  string
	replace bool
}

// queryMsg opens the DQL escape hatch, optionally pre-filled (reveal query).
type queryMsg struct {
	dql string
}

// inspectMsg opens the record inspector for a selected row.
type inspectMsg struct {
	title string
	rec   map[string]any
}

// detailMsg opens the tabbed entity detail page.
type detailMsg struct {
	entity catalog.Entity
	rec    map[string]any // selected row's record; nil = fetch on open
}

// moduleDetailMsg opens the tabbed detail page for a fleet module row: a static
// record tab plus the module's own agent logs and events (scoped to the process
// it monitors).
type moduleDetailMsg struct {
	rec map[string]any
}

// problemMsg opens the tabbed problem page for a Davis problem record.
type problemMsg struct {
	rec map[string]any
}

// vulnMsg opens the tabbed vulnerability page for a security.events
// vulnerability record (summarized list row or raw state report).
type vulnMsg struct {
	rec map[string]any
}

// metricsMsg opens the canned metrics charts for an entity.
type metricsMsg struct {
	entity catalog.Entity
}

// metricChartMsg opens the explorer chart for one metric key (enter on a
// metric-explorer row); entity is the explorer's scope (nil = unscoped).
type metricChartMsg struct {
	key    string
	entity *catalog.Entity
}

// applyFacetMsg asks the app to facet the nearest list view beneath the
// current page by field=value (the inspector's 'f'). A value wrapped in '*'
// applies as a contains pattern (element-wise on string arrays); tokens
// asks for a `~` token-search facet instead — the only operator that
// matches inside arrays of records or numbers.
type applyFacetMsg struct {
	field  string
	value  string
	tokens bool
}

// historyMarkMsg asks the app to snapshot the current stack into the
// persistent history — a view's server-side narrowing (search, facets)
// changed its identity without a navigation.
type historyMarkMsg struct{}

func markHistory() tea.Msg { return historyMarkMsg{} }

// statusMsg shows a transient message in the footer.
type statusMsg struct {
	text  string
	isErr bool
}

func status(text string) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text} }
}

func statusErr(text string) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text, isErr: true} }
}

// bodySizeMsg tells views how much room the body has (chrome excluded).
type bodySizeMsg struct {
	width, height int
}

// claimKey is a no-op command a view returns to consume a key press the app
// would otherwise act on itself, e.g. esc that clears a filter instead of
// popping the stack (bubbletea discards nil messages).
var claimKey tea.Cmd = func() tea.Msg { return nil }

// entityName is the display label for an entity (ID when unnamed).
func entityName(e catalog.Entity) string {
	if e.Name != "" {
		return e.Name
	}
	return e.ID
}
