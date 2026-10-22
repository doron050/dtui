package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dynatrace-oss/dtctl/pkg/exec"
)

func segStubSource(list []SegmentOption, err error) SegmentLister {
	return func(context.Context) ([]SegmentOption, error) { return list, err }
}

func testSegmentList() []SegmentOption {
	return []SegmentOption{
		{UID: "uid-a", Name: "payments-prod", Description: "payments services in prod"},
		{UID: "uid-b", Name: "team-checkout", HasVariables: true},
		{UID: "uid-c", Name: "infra-eu"},
	}
}

// varSegmentList is a picker list whose first entry defines a variable, so
// space-toggling it opens the value sub-picker.
func varSegmentList() []SegmentOption {
	return []SegmentOption{
		{UID: "uid-var", Name: "webshops", HasVariables: true,
			VariablesQuery: "fetch dt.entity.cloud_application_namespace | fields namespace = entity.name"},
		{UID: "uid-plain", Name: "plain"},
	}
}

// segVars returns the open segment picker's variable value sub-picker.
func segVars(t *testing.T, a *app) *segVarPicker {
	t.Helper()
	sp := overlayAs[*segmentPicker](t, a)
	if sp.vars == nil {
		t.Fatal("segment variable sub-picker is not open")
	}
	return sp.vars
}

// deliverSegVarRows feeds the value sub-picker its query result (deliver
// drops dataMsg, so tests inject it like seedRows does for tables).
func deliverSegVarRows(t *testing.T, a *app, rows []map[string]any) {
	t.Helper()
	v := segVars(t, a)
	a.Update(dataMsg{owner: v, seq: v.seq, records: rows})
}

func namespaceRows() []map[string]any {
	return []map[string]any{
		{"namespace": "astroshop"},
		{"namespace": "easytrade"},
		{"namespace": "online-boutique"},
	}
}

// multiVarSegmentList returns a segment whose variable query produces both a
// user-selected primary column and a derived array-valued secondary column
// (mirroring the k8s.namespace.name → dt.smartscape.service pattern).
func multiVarSegmentList() []SegmentOption {
	return []SegmentOption{
		{UID: "uid-multi", Name: "k8s-namespace", HasVariables: true,
			VariablesQuery: "stub-multivar-query"},
		{UID: "uid-plain", Name: "plain"},
	}
}

// multiVarRows is the variable-query result for multiVarSegmentList.
// dt.smartscape.service is an array (from collectDistinct) per namespace row.
func multiVarRows() []map[string]any {
	return []map[string]any{
		{"k8s.namespace.name": "demo-app", "dt.smartscape.service": []any{"SERVICE-AAA", "SERVICE-BBB"}},
		{"k8s.namespace.name": "demo-app-data", "dt.smartscape.service": []any{"SERVICE-CCC"}},
		{"k8s.namespace.name": "agent", "dt.smartscape.service": []any{"SERVICE-DDD"}},
	}
}

// findSegVar looks up a variable binding by name.
func findSegVar(binds []exec.FilterSegmentVariable, name string) *exec.FilterSegmentVariable {
	for i := range binds {
		if binds[i].Name == name {
			return &binds[i]
		}
	}
	return nil
}

// testAppSeeded builds an app with workspace segments pending, mirroring
// testApp but with segment options set before Init runs.
func testAppSeeded(t *testing.T, opts Options) *app {
	t.Helper()
	a, err := newApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	a.ds.runFn = func(string) ([]map[string]any, error) { return nil, nil }
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	deliver(a, a.Init())
	return a
}

func TestExecOptsCarriesSegments(t *testing.T) {
	d := &dataSource{segments: []exec.FilterSegmentRef{{ID: "uid-a"}}}
	opts := d.execOpts(500, false)
	if opts.MaxResultRecords != 500 || opts.FetchTimeoutSeconds != 60 {
		t.Errorf("execOpts basics changed: %+v", opts)
	}
	if len(opts.Segments) != 1 || opts.Segments[0].ID != "uid-a" {
		t.Errorf("Segments = %+v, want the dataSource's", opts.Segments)
	}
	if got := (&dataSource{}).execOpts(1000, false).Segments; got != nil {
		t.Errorf("unsegmented execOpts carries %+v", got)
	}
}

func TestExecOptsMetricEnrichment(t *testing.T) {
	// enrichMetrics requests the metrics metadata field — pkg/exec translates
	// it to the query API's enrich=metric-metadata parameter.
	got := (&dataSource{}).execOpts(1000, true).MetadataFields
	if len(got) != 1 || got[0] != "metrics" {
		t.Errorf("enriched MetadataFields = %v, want [metrics]", got)
	}
	if got := (&dataSource{}).execOpts(1000, false).MetadataFields; got != nil {
		t.Errorf("plain query must not request metadata, got %v", got)
	}
}

func TestSegmentPickerToggleApplyClear(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(testSegmentList(), nil)

	press(a, key("S"))
	overlayAs[*segmentPicker](t, a)
	if len(a.segList) != 3 {
		t.Fatalf("picker list = %d entries, want 3", len(a.segList))
	}

	seqBefore := a.top().(*tableView).seq
	press(a, key(" ")) // toggle payments-prod
	press(a, key("j"))
	press(a, key(" ")) // toggle team-checkout
	press(a, key("enter"))
	if a.overlay != nil {
		t.Fatal("enter did not close the picker")
	}
	if len(a.ds.segments) != 2 || a.ds.segments[0].ID != "uid-a" || a.ds.segments[1].ID != "uid-b" {
		t.Fatalf("ds.segments = %+v, want uid-a, uid-b in list order", a.ds.segments)
	}
	if a.top().(*tableView).seq <= seqBefore {
		t.Error("apply did not refetch the open view")
	}
	if header := a.renderHeader(); !strings.Contains(header, "◐ payments-prod +1") {
		t.Errorf("header misses segment pill: %q", header)
	}
	if echo := a.top().Echo(); !strings.Contains(echo, "-S uid-a") || !strings.Contains(echo, "-S uid-b") {
		t.Errorf("Echo misses -S flags: %q", echo)
	}

	// Re-enter without changes: no refetch storm.
	seqBefore = a.top().(*tableView).seq
	press(a, key("S"))
	press(a, key("enter"))
	if a.top().(*tableView).seq != seqBefore {
		t.Error("unchanged selection refetched views")
	}

	// esc discards the scratch selection.
	press(a, key("S"))
	press(a, key(" ")) // would deselect payments-prod
	press(a, key("esc"))
	if len(a.ds.segments) != 2 {
		t.Fatalf("esc committed the scratch selection: %+v", a.ds.segments)
	}

	// c clears; enter applies the empty set.
	press(a, key("S"))
	press(a, key("c"))
	press(a, key("enter"))
	if len(a.ds.segments) != 0 {
		t.Fatalf("clear left segments applied: %+v", a.ds.segments)
	}
	if header := a.renderHeader(); strings.Contains(header, "◐") {
		t.Errorf("header still shows a pill after clear: %q", header)
	}
}

func TestSegmentPickerRefusesEleventh(t *testing.T) {
	list := make([]SegmentOption, maxSegments+1)
	for i := range list {
		list[i] = SegmentOption{UID: fmt.Sprintf("uid-%02d", i), Name: fmt.Sprintf("seg-%02d", i)}
	}
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(list, nil)
	press(a, key("S"))
	sp := overlayAs[*segmentPicker](t, a)
	for i := 0; i <= maxSegments; i++ {
		press(a, key(" "))
		if i < maxSegments {
			press(a, key("j")) // any key clears the transient status — keep the 11th toggle's
		}
	}
	if len(sp.checked) != maxSegments {
		t.Fatalf("checked %d segments, want the Grail cap %d", len(sp.checked), maxSegments)
	}
	if !a.statusErr || !strings.Contains(a.status, "at most 10") {
		t.Errorf("status = %q (err=%v), want the cap notice", a.status, a.statusErr)
	}
}

func TestSegmentsCommandOpensPicker(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(testSegmentList(), nil)
	press(a, key(":"))
	press(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("segments")})
	press(a, key("enter"))
	overlayAs[*segmentPicker](t, a)
}

func TestWorkspaceSegmentSeeding(t *testing.T) {
	a := testAppSeeded(t, Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "logs",
		SegmentSource: segStubSource(testSegmentList(), nil),
		WorkspaceSegments: []WorkspaceSegment{
			{Ref: "uid-b", Variables: []exec.FilterSegmentVariable{{Name: "env", Values: []string{"prod"}}}},
			{Ref: "Payments-Prod"}, // by name, case-insensitive exact
		},
	})
	if a.segPending {
		t.Fatal("segPending still set after seeding")
	}
	if len(a.ds.segments) != 2 || a.ds.segments[0].ID != "uid-b" || a.ds.segments[1].ID != "uid-a" {
		t.Fatalf("ds.segments = %+v, want uid-b, uid-a in file order", a.ds.segments)
	}
	vars := a.ds.segments[0].Variables
	if len(vars) != 1 || vars[0].Name != "env" || vars[0].Values[0] != "prod" {
		t.Fatalf("variable bindings lost: %+v", vars)
	}
	if a.statusErr {
		t.Errorf("clean seeding reported an error: %q", a.status)
	}
	if header := a.renderHeader(); !strings.Contains(header, "◐ team-checkout +1") {
		t.Errorf("header misses seeded pill: %q", header)
	}
}

func TestWorkspaceSegmentSeedingWarnsUnknownAndAmbiguous(t *testing.T) {
	list := append(testSegmentList(), SegmentOption{UID: "uid-d", Name: "payments-prod"})
	a := testAppSeeded(t, Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "logs",
		SegmentSource: segStubSource(list, nil),
		WorkspaceSegments: []WorkspaceSegment{
			{Ref: "infra-eu"},
			{Ref: "missing-segment"},
			{Ref: "payments-prod"}, // two segments share this name
		},
	})
	if len(a.ds.segments) != 1 || a.ds.segments[0].ID != "uid-c" {
		t.Fatalf("ds.segments = %+v, want just infra-eu", a.ds.segments)
	}
	if !a.statusErr {
		t.Fatalf("no warning surfaced; status = %q", a.status)
	}
	if !strings.Contains(a.status, `"missing-segment" not found`) || !strings.Contains(a.status, "ambiguous") {
		t.Errorf("warning misses causes: %q", a.status)
	}
}

func TestWorkspaceSegmentSeedingListFailure(t *testing.T) {
	a := testAppSeeded(t, Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "logs",
		SegmentSource:     segStubSource(nil, errors.New("HTTP 403: missing storage:filter-segments:read")),
		WorkspaceSegments: []WorkspaceSegment{{Ref: "payments-prod"}},
	})
	if a.segPending {
		t.Fatal("segPending stuck after a failed fetch")
	}
	if len(a.ds.segments) != 0 {
		t.Fatalf("segments applied despite fetch failure: %+v", a.ds.segments)
	}
	if !a.statusErr || !strings.Contains(a.status, "workspace segments not applied") {
		t.Errorf("status = %q, want the not-applied warning", a.status)
	}
	if header := a.renderHeader(); strings.Contains(header, "◐") || strings.Contains(header, "◌") {
		t.Errorf("header claims a scope that was never applied: %q", header)
	}
}

func TestSegmentsRejectedOnAPIBackedViews(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(testSegmentList(), nil)
	press(a, key("S"))
	press(a, key(" "))
	press(a, key("enter"))

	if a.segmentsRejected("logs") {
		t.Error("segments wrongly rejected on a DQL view")
	}
	if !a.segmentsRejected("slos") {
		t.Error("slos is API-backed — segments should be rejected")
	}
	deliver(a, a.jumpTo("slos", ""))
	if !a.statusErr || !strings.Contains(a.status, "segments don't apply") {
		t.Errorf("status = %q, want the API-backed notice", a.status)
	}
}

func TestSegVarSubPickerBindsValues(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(varSegmentList(), nil)

	press(a, key("S"))
	press(a, key(" ")) // toggling the unbound vars segment opens the sub-picker
	v := segVars(t, a)
	if !v.loading {
		t.Fatal("sub-picker not in loading state")
	}
	deliverSegVarRows(t, a, namespaceRows())
	if v.loading || len(v.rows) != 3 {
		t.Fatalf("rows not delivered: loading=%v rows=%d", v.loading, len(v.rows))
	}
	if len(v.cols) != 1 || v.cols[0] != "namespace" {
		t.Fatalf("cols = %v, want [namespace]", v.cols)
	}

	press(a, key("j"))
	press(a, key(" ")) // check easytrade
	press(a, key("enter"))
	sp := overlayAs[*segmentPicker](t, a) // sub-picker enter returns to the segment picker
	if sp.vars != nil {
		t.Fatal("enter did not close the sub-picker")
	}
	if !sp.checked["uid-var"] {
		t.Fatal("segment lost its check after binding")
	}
	binds := a.segVars["uid-var"]
	if len(binds) != 1 || binds[0].Name != "namespace" || len(binds[0].Values) != 1 || binds[0].Values[0] != "easytrade" {
		t.Fatalf("bindings = %+v, want namespace=[easytrade]", binds)
	}

	press(a, key("enter")) // apply the segment selection
	if len(a.ds.segments) != 1 || a.ds.segments[0].ID != "uid-var" {
		t.Fatalf("ds.segments = %+v", a.ds.segments)
	}
	if vars := a.ds.segments[0].Variables; len(vars) != 1 || vars[0].Values[0] != "easytrade" {
		t.Fatalf("applied refs miss the binding: %+v", a.ds.segments[0])
	}
}

func TestSegVarSubPickerEscCancelsToggle(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(varSegmentList(), nil)
	press(a, key("S"))
	press(a, key(" "))
	deliverSegVarRows(t, a, namespaceRows())
	press(a, key("esc"))
	sp := overlayAs[*segmentPicker](t, a)
	if sp.vars != nil {
		t.Fatal("esc did not close the sub-picker")
	}
	if sp.checked["uid-var"] {
		t.Fatal("esc from the value prompt should cancel the segment toggle")
	}
	if len(a.segVars["uid-var"]) != 0 {
		t.Fatalf("esc bound values: %+v", a.segVars["uid-var"])
	}
}

func TestSegVarSubPickerEnterNeedsAValue(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(varSegmentList(), nil)
	press(a, key("S"))
	press(a, key(" "))
	deliverSegVarRows(t, a, namespaceRows())
	press(a, key("enter")) // nothing checked
	segVars(t, a)          // still open
	if !a.statusErr || !strings.Contains(a.status, "at least one value") {
		t.Errorf("status = %q, want the pick-a-value hint", a.status)
	}
}

func TestSegVarSubPickerFilter(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(varSegmentList(), nil)
	press(a, key("S"))
	press(a, key(" "))
	deliverSegVarRows(t, a, namespaceRows())

	press(a, key("/"))
	press(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("boutique")})
	press(a, key("enter")) // leave filter mode, keep the filter
	v := segVars(t, a)
	visible := v.visible()
	if len(visible) != 1 || visible[0] != 2 {
		t.Fatalf("visible = %v, want just online-boutique's row", visible)
	}
	press(a, key(" ")) // toggles the filtered row's absolute index
	if !v.checked[2] {
		t.Fatal("toggle under filter hit the wrong row")
	}
	press(a, key("enter"))
	if got := a.segVars["uid-var"][0].Values[0]; got != "online-boutique" {
		t.Fatalf("bound %q, want online-boutique", got)
	}
}

func TestSegVarVOpensWithExistingBindingsPrechecked(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(varSegmentList(), nil)
	a.segVars = map[string][]exec.FilterSegmentVariable{
		"uid-var": {{Name: "namespace", Values: []string{"astroshop"}}},
	}
	press(a, key("S"))
	press(a, key("v"))
	v := segVars(t, a)
	deliverSegVarRows(t, a, namespaceRows())
	if !v.checked[0] || v.checked[1] {
		t.Fatalf("pre-check wrong: %+v (want only astroshop's row)", v.checked)
	}

	// v on a segment without variables refuses.
	press(a, key("esc"))
	press(a, key("j"))
	press(a, key("v"))
	if overlayAs[*segmentPicker](t, a).vars != nil {
		t.Fatal("v opened a sub-picker for a variable-less segment")
	}
	if !a.statusErr || !strings.Contains(a.status, "no variables") {
		t.Errorf("status = %q, want the no-variables notice", a.status)
	}
}

func TestSegVarSubPickerFetchError(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(varSegmentList(), nil)
	press(a, key("S"))
	press(a, key(" "))
	v := segVars(t, a)
	a.Update(dataMsg{owner: v, seq: v.seq, err: errors.New("HTTP 500")})
	if v.loading || v.err == "" {
		t.Fatalf("fetch error not surfaced: loading=%v err=%q", v.loading, v.err)
	}
	// Space-toggling already checked the segment; the segment stays checked
	// unless the user esc's out — verify esc still cancels cleanly.
	press(a, key("esc"))
	if overlayAs[*segmentPicker](t, a).checked["uid-var"] {
		t.Fatal("esc after a fetch error left the segment checked")
	}
}

func altS() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s"), Alt: true}
}

func TestSegmentsPauseResume(t *testing.T) {
	a := testAppSeeded(t, Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "logs",
		SegmentSource: segStubSource(testSegmentList(), nil),
		WorkspaceSegments: []WorkspaceSegment{
			{Ref: "uid-b", Variables: []exec.FilterSegmentVariable{{Name: "env", Values: []string{"prod"}}}},
		},
	})
	if len(a.ds.segments) != 1 {
		t.Fatalf("seeding failed: %+v", a.ds.segments)
	}

	seqBefore := a.top().(*tableView).seq
	press(a, altS())
	if !a.segPaused || a.ds.segments != nil {
		t.Fatalf("pause: paused=%v ds.segments=%+v", a.segPaused, a.ds.segments)
	}
	if a.top().(*tableView).seq <= seqBefore {
		t.Error("pause did not refetch the open view")
	}
	if header := a.renderHeader(); !strings.Contains(header, "◌ team-checkout off") {
		t.Errorf("header misses the off pill: %q", header)
	}
	if echo := a.top().Echo(); strings.Contains(echo, "-S") {
		t.Errorf("paused Echo still carries -S: %q", echo)
	}
	if a.segmentsRejected("slos") {
		t.Error("paused segments filter nothing — nothing to reject on API views")
	}

	press(a, altS())
	if a.segPaused || len(a.ds.segments) != 1 || a.ds.segments[0].ID != "uid-b" {
		t.Fatalf("resume: paused=%v ds.segments=%+v", a.segPaused, a.ds.segments)
	}
	if vars := a.ds.segments[0].Variables; len(vars) != 1 || vars[0].Values[0] != "prod" {
		t.Fatalf("resume lost the bindings: %+v", a.ds.segments[0])
	}
	if header := a.renderHeader(); !strings.Contains(header, "◐ team-checkout") {
		t.Errorf("header misses the restored pill: %q", header)
	}
}

func TestSegmentsPauseWithNothingApplied(t *testing.T) {
	a := testApp(t, "logs")
	press(a, altS())
	if a.segPaused {
		t.Fatal("paused with nothing applied")
	}
	if !a.statusErr || !strings.Contains(a.status, "no segments selected") {
		t.Errorf("status = %q, want the nothing-selected hint", a.status)
	}
}

func TestSegmentPickerEnterResumesPausedSet(t *testing.T) {
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(testSegmentList(), nil)
	press(a, key("S"))
	press(a, key(" "))
	press(a, key("enter"))
	press(a, altS()) // pause
	press(a, key("S"))
	press(a, key("enter")) // unchanged set, but paused — intent is resume
	if a.segPaused || len(a.ds.segments) != 1 {
		t.Fatalf("picker enter did not resume: paused=%v ds.segments=%+v", a.segPaused, a.ds.segments)
	}
}

func TestSegmentSummary(t *testing.T) {
	if got := segmentSummary(nil); got != "" {
		t.Errorf("empty summary = %q", got)
	}
	one := []SegmentOption{{UID: "u", Name: "a-very-long-segment-name-indeed"}}
	if got := segmentSummary(one); !strings.HasSuffix(got, "…") {
		t.Errorf("long name not truncated: %q", got)
	}
	three := []SegmentOption{{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"}}
	if got := segmentSummary(three); got != "alpha +2" {
		t.Errorf("summary = %q, want %q", got, "alpha +2")
	}
	unnamed := []SegmentOption{{UID: "uid-x"}}
	if got := segmentSummary(unnamed); got != "uid-x" {
		t.Errorf("unnamed summary = %q, want the UID", got)
	}
}

func TestRewriteSegmentVarError(t *testing.T) {
	if rewriteSegmentVarError(nil) != nil {
		t.Error("nil error rewritten")
	}
	plain := errors.New("HTTP 500: something else")
	if rewriteSegmentVarError(plain) != plain {
		t.Error("unrelated error rewritten")
	}
	cli := fmt.Errorf("segment uid-b requires variable %q\n\n"+
		"Bind the variable inline on -S using URL-query syntax:\n\n"+
		"  dtctl query \"...\" -S \"uid-b?env=your-value-here\"", "env")
	got := rewriteSegmentVarError(cli).Error()
	if strings.Contains(got, "-S \"") || strings.Contains(got, "--segments-file") {
		t.Errorf("CLI flag examples survived: %q", got)
	}
	if !strings.Contains(got, `segment uid-b requires variable "env"`) || !strings.Contains(got, "v on it to pick values") {
		t.Errorf("rewritten error misses cause or remedy: %q", got)
	}
}

func TestInitialTimeframeFromWorkspace(t *testing.T) {
	a := testAppSeeded(t, Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "logs",
		InitialTimeframe: "24h",
	})
	if a.tf.Label != "24h" {
		t.Errorf("tf = %q, want 24h", a.tf.Label)
	}
	// The picker derives its highlight from the applied window on open.
	press(a, key("t"))
	if sel := overlayAs[*timeframePicker](t, a).sel; sel != 2 {
		t.Errorf("picker highlight = %d, want the 24h preset index", sel)
	}
}

func TestStartupNotice(t *testing.T) {
	a := testAppSeeded(t, Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "logs",
		StartupNotice: "workspace my-service: 1 segment, last 24h",
	})
	if a.statusErr || !strings.Contains(a.status, "workspace my-service") {
		t.Errorf("status = %q (err=%v), want the workspace notice", a.status, a.statusErr)
	}
}

func TestSegVarCellStrings(t *testing.T) {
	if got := segVarCellStrings(nil); got != nil {
		t.Errorf("nil → %v, want nil", got)
	}
	if got := segVarCellStrings(""); got != nil {
		t.Errorf("empty string → %v, want nil", got)
	}
	if got := segVarCellStrings("demo-app"); len(got) != 1 || got[0] != "demo-app" {
		t.Errorf("string → %v, want [demo-app]", got)
	}
	arr := []any{"SERVICE-AAA", "SERVICE-BBB"}
	if got := segVarCellStrings(arr); len(got) != 2 || got[0] != "SERVICE-AAA" || got[1] != "SERVICE-BBB" {
		t.Errorf("[]any → %v, want [SERVICE-AAA SERVICE-BBB]", got)
	}
	// Empty elements in an array are skipped.
	arrWithEmpty := []any{"SERVICE-AAA", "", "SERVICE-BBB"}
	if got := segVarCellStrings(arrWithEmpty); len(got) != 2 {
		t.Errorf("array-with-empty → %v, want 2 elements", got)
	}
	// Non-string types return nil.
	if got := segVarCellStrings(42.0); got != nil {
		t.Errorf("float → %v, want nil", got)
	}
}

func TestSegVarSubPickerBindsArrayValues(t *testing.T) {
	// bindings() must expand array-valued cells (e.g. dt.smartscape.service
	// from collectDistinct) into individual Values rather than dropping them.
	a := testApp(t, "logs")
	a.opts.SegmentSource = segStubSource(multiVarSegmentList(), nil)

	press(a, key("S"))
	press(a, key(" ")) // opens the sub-picker for uid-multi
	deliverSegVarRows(t, a, multiVarRows())

	press(a, key(" ")) // check first row: demo-app / [SERVICE-AAA, SERVICE-BBB]
	press(a, key("enter"))

	binds := a.segVars["uid-multi"]
	ns := findSegVar(binds, "k8s.namespace.name")
	if ns == nil || len(ns.Values) != 1 || ns.Values[0] != "demo-app" {
		t.Errorf("k8s.namespace.name = %+v, want [demo-app]", ns)
	}
	svc := findSegVar(binds, "dt.smartscape.service")
	if svc == nil {
		t.Fatalf("dt.smartscape.service binding missing; binds = %+v", binds)
	}
	if len(svc.Values) != 2 || svc.Values[0] != "SERVICE-AAA" || svc.Values[1] != "SERVICE-BBB" {
		t.Errorf("dt.smartscape.service = %v, want [SERVICE-AAA SERVICE-BBB]", svc.Values)
	}
}

func TestWorkspaceSegmentVariableExpansion(t *testing.T) {
	// Workspace provides only the primary variable (k8s.namespace.name).
	// handleSegmentList must run the segment's variable query, match the
	// workspace-provided namespace rows, expand the derived array-valued
	// secondary (dt.smartscape.service), and apply all bindings.
	const varQuery = "stub-expansion-query"
	varSeg := SegmentOption{
		UID: "uid-ns", Name: "k8s-namespace",
		HasVariables: true, VariablesQuery: varQuery,
	}
	a, err := newApp(Options{
		ContextName: "test", SafetyLevel: "readonly", InitialView: "logs",
		SegmentSource: segStubSource([]SegmentOption{varSeg}, nil),
		WorkspaceSegments: []WorkspaceSegment{{
			Ref: "uid-ns",
			Variables: []exec.FilterSegmentVariable{
				{Name: "k8s.namespace.name", Values: []string{"demo-app", "demo-app-data"}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.ds.runFn = func(dql string) ([]map[string]any, error) {
		if dql == varQuery {
			return multiVarRows(), nil
		}
		return nil, nil
	}
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	deliver(a, a.Init())

	if a.segExpanding != 0 {
		t.Fatalf("segExpanding = %d after seeding; applySegments never fired", a.segExpanding)
	}
	if len(a.ds.segments) != 1 || a.ds.segments[0].ID != "uid-ns" {
		t.Fatalf("ds.segments = %+v, want uid-ns applied", a.ds.segments)
	}

	vars := a.segVars["uid-ns"]
	// k8s.namespace.name: only the two matched namespaces (not "agent").
	ns := findSegVar(vars, "k8s.namespace.name")
	if ns == nil || len(ns.Values) != 2 {
		t.Errorf("k8s.namespace.name = %+v, want 2 values", ns)
	}
	// dt.smartscape.service: expanded from matched rows' arrays;
	// agent's SERVICE-DDD must not appear.
	svc := findSegVar(vars, "dt.smartscape.service")
	if svc == nil {
		t.Fatalf("dt.smartscape.service missing; vars = %+v", vars)
	}
	if len(svc.Values) != 3 { // SERVICE-AAA + SERVICE-BBB + SERVICE-CCC
		t.Errorf("dt.smartscape.service = %v, want 3 values", svc.Values)
	}
	for _, v := range svc.Values {
		if v == "SERVICE-DDD" {
			t.Errorf("agent's SERVICE-DDD must not appear in expansion: %v", svc.Values)
		}
	}
	if header := a.renderHeader(); !strings.Contains(header, "◐ k8s-namespace") {
		t.Errorf("header misses expanded segment pill: %q", header)
	}
}

// primaryTagColumns extracts the Title fields of primary-tag columns (those
// whose Field starts with "primary_tags.") from the top tableView's column set.
func primaryTagColumnTitles(t *testing.T, a *app) []string {
	t.Helper()
	tv, ok := a.top().(*tableView)
	if !ok {
		t.Fatalf("top is %T, want *tableView", a.top())
	}
	var titles []string
	for _, c := range tv.columns() {
		if strings.HasPrefix(c.Field, "primary_tags.") {
			titles = append(titles, c.Title)
		}
	}
	return titles
}

func TestPrimaryTagColumnsFromWorkspace(t *testing.T) {
	// Global workspace primaryTags appear as columns on the initial view.
	a := testAppSeeded(t, Options{
		ContextName:          "test",
		SafetyLevel:          "readonly",
		InitialView:          "logs",
		WorkspacePrimaryTags: []string{"app", "env"},
	})
	titles := primaryTagColumnTitles(t, a)
	if len(titles) != 2 || titles[0] != "APP" || titles[1] != "ENV" {
		t.Errorf("column titles = %v, want [APP ENV]", titles)
	}
}

func TestPrimaryTagColumnsSegmentFirstThenGlobal(t *testing.T) {
	// Segment-declared keys come first; global keys follow (deduped).
	// Applying a segment with primaryTags prepends its keys; unapplying reverts.
	seg := SegmentOption{UID: "uid-ptag", Name: "team-segment"}
	a := testAppSeeded(t, Options{
		ContextName:          "test",
		SafetyLevel:          "readonly",
		InitialView:          "logs",
		SegmentSource:        segStubSource([]SegmentOption{seg}, nil),
		WorkspacePrimaryTags: []string{"app", "env"},
		WorkspaceSegments: []WorkspaceSegment{{
			Ref:         "uid-ptag",
			PrimaryTags: []string{"team", "env"}, // "env" dedupe: keep segment-first occurrence
		}},
	})

	// After seeding: segment keys first, global keys deduped.
	titles := primaryTagColumnTitles(t, a)
	if len(titles) != 3 || titles[0] != "TEAM" || titles[1] != "ENV" || titles[2] != "APP" {
		t.Errorf("with segment: titles = %v, want [TEAM ENV APP]", titles)
	}

	// Pause segments (alt+s): only global keys remain.
	press(a, altS())
	titles = primaryTagColumnTitles(t, a)
	if len(titles) != 2 || titles[0] != "APP" || titles[1] != "ENV" {
		t.Errorf("paused: titles = %v, want [APP ENV]", titles)
	}

	// Resume: segment keys reappear.
	press(a, altS())
	titles = primaryTagColumnTitles(t, a)
	if len(titles) != 3 || titles[0] != "TEAM" || titles[1] != "ENV" || titles[2] != "APP" {
		t.Errorf("resumed: titles = %v, want [TEAM ENV APP]", titles)
	}
}

func TestPrimaryTagNoColumnsWhenNoneConfigured(t *testing.T) {
	// Zero primaryTags configured: no primary-tag columns, no regression.
	a := testApp(t, "logs")
	if titles := primaryTagColumnTitles(t, a); len(titles) != 0 {
		t.Errorf("unconfigured app has primary-tag columns: %v", titles)
	}
}

func TestPrimaryTagFacetCandidatesFromRecord(t *testing.T) {
	// A populated primary_tags.* field on a fetched record appears as a facet
	// candidate in the existing f picker (existing facetCandidates behavior).
	a := testAppSeeded(t, Options{
		ContextName:          "test",
		SafetyLevel:          "readonly",
		InitialView:          "logs",
		WorkspacePrimaryTags: []string{"app"},
	})
	seedRows(t, a, []map[string]any{
		{"content": "msg", "primary_tags.app": "demo-app"},
		{"content": "msg", "primary_tags.app": "easytrade"},
	})
	candidates := a.top().(*tableView).facetCandidates()
	var found bool
	for _, c := range candidates {
		if c == "primary_tags.app" {
			found = true
		}
	}
	if !found {
		t.Errorf("primary_tags.app not in facet candidates: %v", candidates)
	}
}

func TestExecOptsTagsClientContext(t *testing.T) {
	if got := (&dataSource{}).execOpts(1, false).ClientContext; got != "dtui" {
		t.Errorf("ClientContext = %q, want %q", got, "dtui")
	}
}
