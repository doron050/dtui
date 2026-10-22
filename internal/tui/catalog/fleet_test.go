package catalog

import (
	"strings"
	"testing"
	"time"
)

func TestFleetLookupAndLenses(t *testing.T) {
	spec := Lookup("fleet")
	if spec == nil {
		t.Fatal("fleet view not registered")
	}
	for _, alias := range []string{"oneagents", "activegates", "agents", "edge"} {
		if Lookup(alias) != spec {
			t.Errorf("alias %q does not resolve to fleet", alias)
		}
	}
	want := []string{"oneagents", "activegates", "modules"}
	if len(spec.Lenses) != len(want) {
		t.Fatalf("unexpected fleet lenses: %+v", spec.Lenses)
	}
	for i, name := range want {
		if spec.Lenses[i].Name != name {
			t.Errorf("fleet lens %d = %q, want %q", i, spec.Lenses[i].Name, name)
		}
	}
}

func TestFleetQueryPerLens(t *testing.T) {
	spec := Lookup("fleet")
	tf := Timeframe{Label: "2h"}

	oneagents := spec.Query(Scope{Timeframe: tf, Lens: 0})
	if !strings.HasPrefix(oneagents, `smartscapeNodes "ONEAGENT"`) {
		t.Errorf("oneagents lens query:\n%s", oneagents)
	}
	activegates := spec.Query(Scope{Timeframe: tf, Lens: 1})
	if !strings.HasPrefix(activegates, `smartscapeNodes "ACTIVEGATE"`) {
		t.Errorf("activegates lens query:\n%s", activegates)
	}
	for _, q := range []string{oneagents, activegates} {
		if !strings.HasSuffix(q, "| sort name asc\n| limit 500") {
			t.Errorf("query missing sort/limit tail:\n%s", q)
		}
	}
}

// The modules lens is not a Smartscape list but a deduplicated latest-state
// rollup of ONEAGENT_METADATA events: newest-first, one row per (agent,
// module), keyed so the parent OneAgent is recoverable for drills.
func TestFleetModulesLensQuery(t *testing.T) {
	spec := Lookup("fleet")
	q := spec.Query(Scope{Timeframe: Timeframe{Label: "7d", Dur: 7 * 24 * time.Hour}, Lens: 2})
	for _, want := range []string{
		"fetch events, from:now() - 30m", // wide window capped
		`event.type == "ONEAGENT_METADATA"`,
		"`timestamp` = takeMax(timestamp)",
		"`dt.agent.module.health` = takeAny(dt.agent.module.health)",
		"`dt.smartscape.process` = takeAny(dt.smartscape.process)",
		"by:{dt.smartscape.oneagent, dt.agent.module.id}",
		"| sort `dt.agent.module.health` asc, `host.name` asc",
		"| limit 1000",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("modules lens query missing %q:\n%s", want, q)
		}
	}
	// It must NOT sort the raw stream: `sort timestamp desc` forces Grail to
	// order the whole scanned window and blows the scan limit on a busy tenant.
	if strings.Contains(q, "sort timestamp") {
		t.Errorf("modules query must stream (no global timestamp sort):\n%s", q)
	}
	// The fleet-view lens is fleet-wide even under a pin.
	pinned := spec.Query(Scope{Timeframe: Timeframe{Label: "2h"}, Lens: 2, Entity: &Entity{ID: "ONEAGENT-1", Type: "ONEAGENT"}})
	if strings.Contains(pinned, "ONEAGENT-1") {
		t.Errorf("fleet modules lens should stay fleet-wide:\n%s", pinned)
	}

	// A module row (no node id) inspects on enter (Inspect) rather than
	// navigating, but its resolved entity is the process it monitors — so the
	// 'v' drill and Selection scope by the process while the row's Name stays
	// the module's. A node row keeps its own id/type and navigates normally.
	moduleRow := map[string]any{
		"dt.smartscape.process": "PROCESS-7", "dt.agent.module.name": "checkout",
		"dt.smartscape.host": "HOST-7", "dt.agent.module.type": "java",
	}
	if !spec.Inspect(moduleRow) {
		t.Error("module row should inspect on enter, not navigate")
	}
	moduleEnt := spec.Entity(moduleRow)
	if moduleEnt == nil || moduleEnt.ID != "PROCESS-7" || moduleEnt.Type != "PROCESS" || moduleEnt.Name != "checkout" {
		t.Errorf("module row entity = %+v, want its monitored process named for the module", moduleEnt)
	}
	// The module row also carries its host + log subdirectory, so its agent-log
	// tab scopes by host + subdir (see agentLogFilter) rather than the process.
	if moduleEnt != nil && (moduleEnt.ModuleHost != "HOST-7" || moduleEnt.ModuleLogDir != "java") {
		t.Errorf("module row entity log scope = host %q dir %q, want HOST-7/java", moduleEnt.ModuleHost, moduleEnt.ModuleLogDir)
	}
	nodeRow := map[string]any{"id": "ACTIVEGATE-3", "type": "ACTIVEGATE", "name": "ag-1"}
	if spec.Inspect(nodeRow) {
		t.Error("a node row should navigate to its detail, not inspect")
	}
	nodeEnt := spec.Entity(nodeRow)
	if nodeEnt == nil || nodeEnt.ID != "ACTIVEGATE-3" || nodeEnt.Type != "ACTIVEGATE" {
		t.Errorf("node row entity = %+v, want the node itself", nodeEnt)
	}
	if !hasColumn(fleetModuleStateColumns, "MODULE ID") {
		t.Error("modules columns must surface the module id")
	}
}

func hasColumn(cols []Column, title string) bool {
	for _, c := range cols {
		if c.Title == title {
			return true
		}
	}
	return false
}

// modulesSpec is both a top-level view and the OneAgent detail tab: unscoped it
// is fleet-wide, scoped to a OneAgent it composes the smartscape id filter so
// the tab shows just that agent's modules.
func TestModulesSpecScoping(t *testing.T) {
	spec := Lookup("modules")
	if spec == nil {
		t.Fatal("modules view not registered")
	}
	tf := Timeframe{Label: "2h"}
	unscoped := spec.Query(Scope{Timeframe: tf})
	if strings.Contains(unscoped, "toSmartscapeId") {
		t.Errorf("unscoped modules query should be fleet-wide:\n%s", unscoped)
	}
	scoped := spec.Query(Scope{Timeframe: tf, Entity: &Entity{ID: "ONEAGENT-9", Type: "ONEAGENT"}})
	if !strings.Contains(scoped, `dt.smartscape.oneagent == toSmartscapeId("ONEAGENT-9")`) {
		t.Errorf("OneAgent-scoped modules query missing filter:\n%s", scoped)
	}
	// A host pins by the host smartscape id the metadata also carries.
	host := spec.Query(Scope{Timeframe: tf, Entity: &Entity{ID: "HOST-2", Type: "HOST"}})
	if !strings.Contains(host, `dt.smartscape.host == toSmartscapeId("HOST-2")`) {
		t.Errorf("host-scoped modules query missing filter:\n%s", host)
	}
	// An unrelated type is not a module scope — the query stays fleet-wide, so
	// CanScope honestly reports the pin inapplicable.
	if spec.CanScope(tf, Entity{ID: "SERVICE-1", Type: "SERVICE"}) {
		t.Error("modules view should not claim a SERVICE scope")
	}
	if !spec.CanScope(tf, Entity{ID: "ONEAGENT-9", Type: "ONEAGENT"}) {
		t.Error("modules view should scope a OneAgent")
	}
}

// The fleet view is an entity list — like hosts, its Smartscape query ignores
// any pinned entity scope (a fleet is browsed, not narrowed to one agent).
func TestFleetEntityViewIgnoresScope(t *testing.T) {
	spec := Lookup("fleet")
	scoped := spec.Query(Scope{
		Timeframe: Timeframe{Label: "2h"},
		Entity:    &Entity{ID: "ONEAGENT-1", Type: "ONEAGENT"},
	})
	if strings.Contains(scoped, "ONEAGENT-1") {
		t.Errorf("fleet query should not compose entity scope:\n%s", scoped)
	}
}

// The OneAgent enrichment folds the latest FLEET_EVENT / ONEAGENT_METADATA
// state onto each row: deduplicated to the latest emission per module, keyed
// by dt.smartscape.oneagent, and cast with toSmartscapeId (a plain-string
// compare silently matches nothing on that smartscape_id field).
func TestFleetEnrichmentQuery(t *testing.T) {
	spec := Lookup("fleet")
	if spec.Enrich == nil {
		t.Fatal("fleet view has no enrichment")
	}
	q := spec.Enrich.Query(Timeframe{Label: "2h"}, []string{"ONEAGENT-1", "ONEAGENT-2"})
	for _, want := range []string{
		"fetch events, from:",
		`event.kind == "FLEET_EVENT" and event.type == "ONEAGENT_METADATA"`,
		`in(dt.smartscape.oneagent, {toSmartscapeId("ONEAGENT-1"), toSmartscapeId("ONEAGENT-2")})`,
		"by:{dt.smartscape.oneagent, dt.agent.module.id}",
		`unhealthy = countIf(health != "Healthy")`,
		"by:{dt.smartscape.oneagent}",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("enrichment query missing %q:\n%s", want, q)
		}
	}
	// ActiveGate rows are not keyed, so the batch never runs on that lens.
	if k := spec.Enrich.Key(map[string]any{"type": "ACTIVEGATE", "id": "ACTIVEGATE-1"}); k != "" {
		t.Errorf("ActiveGate row should be unkeyed for enrichment, got %q", k)
	}
	if k := spec.Enrich.Key(map[string]any{"type": "ONEAGENT", "id": "ONEAGENT-1"}); k != "ONEAGENT-1" {
		t.Errorf("OneAgent enrichment key = %q, want ONEAGENT-1", k)
	}
}

// A wide relative window is capped so the fetch events scan stays bounded, but
// an absolute (problem-pinned) window is honored verbatim.
func TestFleetStateWindowCaps(t *testing.T) {
	if got := fleetStateWindow(Timeframe{Label: "7d", Dur: 7 * 24 * time.Hour}); got != "now() - 30m" {
		t.Errorf("7d window not capped: %q", got)
	}
	if got := fleetStateWindow(Timeframe{Label: "30m", Dur: 30 * time.Minute}); got != "now() - 30m" {
		t.Errorf("30m window changed: %q", got)
	}
	if got := fleetStateWindow(Timeframe{Label: "15m", Dur: 15 * time.Minute}); got != "now() - 15m" {
		t.Errorf("15m window (under cap) should be honored: %q", got)
	}
	abs := Timeframe{From: time.Unix(1700000000, 0), To: time.Unix(1700003600, 0), Dur: time.Hour}
	if got := fleetStateWindow(abs); !strings.HasPrefix(got, "toTimestamp(") {
		t.Errorf("absolute window not honored: %q", got)
	}
}

func TestFleetOneAgentModulesRendering(t *testing.T) {
	rec := map[string]any{
		EnrichKey("modules"):   "7",
		EnrichKey("unhealthy"): "2",
	}
	if got := oneagentModules(rec); got != "5/7" {
		t.Errorf("oneagentModules = %q, want 5/7", got)
	}
	if got := oneagentModulesClass("5/7"); got != "warn" {
		t.Errorf("partial health class = %q, want warn", got)
	}
	if got := oneagentModulesClass("7/7"); got != "ok" {
		t.Errorf("full health class = %q, want ok", got)
	}
	if oneagentModules(map[string]any{}) != "" {
		t.Error("unenriched row should render empty modules cell")
	}
	// Installed agent version wins over the node's module version.
	enriched := map[string]any{EnrichKey("version"): "1.345.0", "dt.agent.module.version": "1.341.48"}
	if got := oneagentVersion(enriched); got != "1.345.0" {
		t.Errorf("oneagentVersion = %q, want installed 1.345.0", got)
	}
	if got := oneagentVersion(map[string]any{"dt.agent.module.version": "1.341.48"}); got != "1.341.48" {
		t.Errorf("oneagentVersion fallback = %q, want 1.341.48", got)
	}
}

func TestFleetEventsLens(t *testing.T) {
	spec := Lookup("events")
	var fleet *Lens
	for i := range spec.Lenses {
		if spec.Lenses[i].Name == "fleet" {
			fleet = &spec.Lenses[i]
		}
	}
	if fleet == nil {
		t.Fatal("events view has no fleet lens")
	}
	// The events fleet lens strictly shows the OneAgent module metadata stream.
	if !strings.Contains(fleet.Filter, `event.type == "ONEAGENT_METADATA"`) {
		t.Errorf("fleet lens filter = %q", fleet.Filter)
	}
	lensIdx := -1
	for i := range spec.Lenses {
		if spec.Lenses[i].Name == "fleet" {
			lensIdx = i
		}
	}
	// Scoped to a OneAgent, the fleet lens composes the smartscape id filter.
	q := spec.Query(Scope{
		Timeframe: Timeframe{Label: "2h"},
		Entity:    &Entity{ID: "ONEAGENT-9", Type: "ONEAGENT"},
		Lens:      lensIdx,
	})
	for _, want := range []string{
		"fetch events, from:now() - 2h",
		`dt.smartscape.oneagent == toSmartscapeId("ONEAGENT-9")`,
		`event.type == "ONEAGENT_METADATA"`,
	} {
		if !strings.Contains(q, want) {
			t.Errorf("scoped fleet events query missing %q:\n%s", want, q)
		}
	}
	// Its columns are module-centric.
	titles := map[string]bool{}
	for _, c := range fleet.Columns {
		titles[c.Title] = true
	}
	for _, want := range []string{"MODULE", "HEALTH", "VERSION", "INJECT"} {
		if !titles[want] {
			t.Errorf("fleet lens missing %q column", want)
		}
	}
}

// A OneAgent's events tab opens on the FLEET_EVENT module stream, not the
// generic Davis lens (which renders those records with empty columns).
func TestDefaultEventLens(t *testing.T) {
	spec := Lookup("events")
	if got := spec.Lenses[DefaultEventLens("ONEAGENT")].Name; got != "fleet" {
		t.Errorf("OneAgent default event lens = %q, want fleet", got)
	}
	if got := spec.Lenses[DefaultEventLens("HOST")].Name; got != "all" {
		t.Errorf("HOST default event lens = %q, want all", got)
	}
}

func TestIsEdgeComponent(t *testing.T) {
	for _, ty := range []string{"ONEAGENT", "ACTIVEGATE"} {
		if !IsEdgeComponent(ty) {
			t.Errorf("%s should be an edge component", ty)
		}
	}
	for _, ty := range []string{"HOST", "SERVICE", "K8S_POD", ""} {
		if IsEdgeComponent(ty) {
			t.Errorf("%q should not be an edge component", ty)
		}
	}
}

func TestAgentLogsSpecScoping(t *testing.T) {
	spec := Lookup("agentlogs")
	if spec == nil {
		t.Fatal("agentlogs view not registered")
	}
	if Lookup("agentlog") != spec {
		t.Error("alias agentlog does not resolve to agentlogs")
	}
	tf := Timeframe{Label: "2h"}

	// OneAgent: combined logs across modules, scoped by the monitored host
	// (ONEAGENT id suffix → HOST id) under the oneagent tree.
	oa := spec.Query(Scope{Timeframe: tf, Entity: &Entity{ID: "ONEAGENT-0000000000000D01", Name: "h1", Type: "ONEAGENT"}})
	if !strings.Contains(oa, `dt.smartscape.host == toSmartscapeId("HOST-0000000000000D01")`) {
		t.Errorf("OneAgent agent-log query not host-scoped:\n%s", oa)
	}
	if !strings.Contains(oa, `startsWith(log.source, "/var/log/dynatrace/oneagent/")`) {
		t.Errorf("OneAgent agent-log query not confined to the oneagent tree:\n%s", oa)
	}
	if !strings.HasPrefix(oa, "fetch logs, from:") || !strings.Contains(oa, "sort timestamp desc") {
		t.Errorf("agent-log query shape:\n%s", oa)
	}

	// Module row: scoped by the module's host + its fixed log subdirectory (the
	// stable join — the metadata snapshot's process rarely matches the process
	// that stamps the module's log files), confined to that subdir so only this
	// module's own files match.
	mod := spec.Query(Scope{Timeframe: tf, Entity: &Entity{
		ID: "PROCESS-9", Name: "oneagentloganalytics", Type: "PROCESS",
		ModuleHost: "HOST-2", ModuleLogDir: "loganalytics",
	}})
	if !strings.Contains(mod, `dt.smartscape.host == toSmartscapeId("HOST-2")`) {
		t.Errorf("module agent-log query not host-scoped:\n%s", mod)
	}
	if !strings.Contains(mod, `startsWith(log.source, "/var/log/dynatrace/oneagent/loganalytics/")`) {
		t.Errorf("module agent-log query not confined to the module subdir:\n%s", mod)
	}
	if strings.Contains(mod, "dt.smartscape.process") {
		t.Errorf("module agent-log query should not scope by the (churny) process id:\n%s", mod)
	}

	// Fallback — a real PROCESS entity, or a module type with no known subdir
	// (empty module fields): best-effort process scope, confined to the oneagent
	// tree so the process's own application logs are excluded (no conflation).
	proc := spec.Query(Scope{Timeframe: tf, Entity: &Entity{ID: "PROCESS-9", Name: "mod", Type: "PROCESS"}})
	if !strings.Contains(proc, `dt.smartscape.process == toSmartscapeId("PROCESS-9")`) {
		t.Errorf("process fallback agent-log query not process-scoped:\n%s", proc)
	}
	if !strings.Contains(proc, `startsWith(log.source, "/var/log/dynatrace/oneagent/")`) {
		t.Errorf("process fallback agent-log query not confined to the oneagent tree:\n%s", proc)
	}

	// ActiveGate: matched by host name, everything under /var/log/dynatrace/
	// EXCEPT the oneagent tree (gateway/synthetic/sc2).
	ag := spec.Query(Scope{Timeframe: tf, Entity: &Entity{ID: "ACTIVEGATE-3B41C6E", Name: "ag-host-1", Type: "ACTIVEGATE"}})
	if !strings.Contains(ag, `host.name == "ag-host-1"`) {
		t.Errorf("ActiveGate agent-log query not hostname-scoped:\n%s", ag)
	}
	if !strings.Contains(ag, `startsWith(log.source, "/var/log/dynatrace/")`) ||
		!strings.Contains(ag, `not startsWith(log.source, "/var/log/dynatrace/oneagent/")`) {
		t.Errorf("ActiveGate agent-log query does not select the AG trees:\n%s", ag)
	}
}

// moduleLogDir maps each OneAgent module type to its log subdirectory under
// /var/log/dynatrace/oneagent/ (validated live on a tenant); an unknown type returns
// "" so the logs tab falls back to the process filter rather than mis-scoping.
func TestModuleLogDir(t *testing.T) {
	cases := map[string]string{
		"log_analytics": "loganalytics",
		"os":            "os",
		"java":          "java",
		"go":            "go",
		"python":        "python",
		"nginx":         "nginx",
		"nodejs":        "nodejs",
		"sdk":           "sdk",
		"remote_plugin": "plugin",
		"z":             "",
		"":              "",
	}
	for typ, want := range cases {
		if got := moduleLogDir(typ); got != want {
			t.Errorf("moduleLogDir(%q) = %q, want %q", typ, got, want)
		}
	}
}

// An unscoped or non-agent entity must never fleet-scan the (huge) log store:
// the guard collapses to `filter false`.
func TestAgentLogsSpecUnscopedIsInert(t *testing.T) {
	spec := Lookup("agentlogs")
	tf := Timeframe{Label: "2h"}
	for _, s := range []Scope{
		{Timeframe: tf},
		{Timeframe: tf, Entity: &Entity{ID: "SERVICE-1", Type: "SERVICE"}},
	} {
		q := spec.Query(s)
		if !strings.Contains(q, "filter false") {
			t.Errorf("unscopable agent-log query should be inert, got:\n%s", q)
		}
	}
}

func TestActiveGateKeyFacts(t *testing.T) {
	facts := KeyFacts("ACTIVEGATE")
	if len(facts) == 0 {
		t.Fatal("no key facts for ACTIVEGATE")
	}
	rec := map[string]any{
		"id":                        "ACTIVEGATE-1",
		"dt.active_gate.version":    "1.345.0",
		"dt.active_gate.group.name": "default",
		"purpose":                   "routing",
		"os.architecture":           "amd64",
		"is_containerized":          true,
	}
	labels := map[string]string{}
	for _, f := range facts {
		labels[f.Label] = f.Value(rec)
	}
	if labels["version"] != "1.345.0" {
		t.Errorf("version fact = %q", labels["version"])
	}
	if labels["group"] != "default" {
		t.Errorf("group fact = %q", labels["group"])
	}
	if labels["containerized"] != "yes" {
		t.Errorf("containerized fact = %q", labels["containerized"])
	}
	if labels["arch"] != "amd64" {
		t.Errorf("arch fact = %q", labels["arch"])
	}
}
