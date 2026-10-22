package catalog

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Fleet management: the observability edge itself — the OneAgents and
// ActiveGates a tenant runs, rather than the workloads they watch. Both are
// first-class Smartscape node types (validated live: smartscapeNodes
// "ONEAGENT" / "ACTIVEGATE"), so one lensed entity view browses them the same
// way the hosts view browses HOST.
//
// The OneAgent node record is deliberately thin (version, mode, zone). The
// richer, per-module operational state — which modules are healthy, the
// installed agent version — arrives as FLEET_EVENT / ONEAGENT_METADATA event
// records, one per agent module, re-emitted on a heartbeat (validated live).
// The oneagents lens folds that latest state onto each row through the same
// batched enrichment the hosts view uses for CPU sparklines: a scoped events
// summarize, deduplicated to the latest emission per module, keyed by
// dt.smartscape.oneagent. ActiveGates carry their state on the node itself
// (modules, group, purpose), so they need no enrichment.
var fleetSpec = &Spec{
	Name:    "fleet",
	Aliases: []string{"agents", "oneagents", "activegates", "edge"},
	Kind:    KindEntity,
	Desc:    "Edge components: OneAgents & ActiveGates (Smartscape)",
	Query: func(s Scope) string {
		switch lensAt(fleetLenses, s.Lens).Name {
		case "activegates":
			return "smartscapeNodes \"ACTIVEGATE\"\n| fieldsRemove references\n| sort name asc\n| limit 500"
		case "modules":
			// The fleet-view lens is always fleet-wide; the per-agent view is
			// the scoped `modules` tab on a OneAgent detail page.
			return fleetModulesQuery(Scope{Timeframe: s.Timeframe})
		default:
			return "smartscapeNodes \"ONEAGENT\"\n| fieldsRemove references\n| sort name asc\n| limit 500"
		}
	},
	Lenses:  fleetLenses,
	Columns: oneagentColumns,
	Entity:  fleetEntity,
	// Module rows (modules lens) are not nodes — enter opens the module detail
	// page (a static record tab + the module's agent logs) instead of
	// navigating into the process and refetching its node.
	Inspect: IsFleetModuleRow,
	// FLEET_EVENT is the natural signal: 'v' opens this agent's fleet events
	// (module metadata, online notices), scoped by dt.smartscape.oneagent /
	// dt.smartscape.activegate through SignalFilter. 'l' opens the agent's own
	// log files (agentLogFilter) — an edge node's combined logs, a module row's
	// per-process deep-monitoring logs.
	Drills: map[string]string{"l": "agentlogs", "v": "events"},
	Enrich: &EnrichSpec{
		// Only OneAgents carry ONEAGENT_METADATA; leaving ActiveGate rows
		// unkeyed skips the (empty) enrichment query entirely on that lens.
		Key: func(rec map[string]any) string {
			if Str(rec, "type") == "ONEAGENT" {
				return Str(rec, "id")
			}
			return ""
		},
		By:     "dt.smartscape.oneagent",
		Series: []string{"modules", "unhealthy", "version"},
		Query:  fleetOneAgentStateQuery,
	},
}

// modulesSpec is the OneAgent module inventory as a view in its own right:
// fleet-wide at the top level (:modules) and — via containmentTabs — the
// scoped "modules" tab on a OneAgent detail page, listing that agent's
// injected modules. It shares fleetModulesQuery (and its columns/entity) with
// the fleet view's modules lens; the only difference is that here the query
// composes the pinned OneAgent/host scope.
var modulesSpec = &Spec{
	Name:    "modules",
	Aliases: []string{"mod", "mods"},
	Kind:    KindSignal,
	Desc:    "OneAgent modules — injected deep-monitoring modules (latest state)",
	Query:   fleetModulesQuery,
	Columns: fleetModuleStateColumns,
	Entity:  fleetEntity,
	// A module is not a node: enter opens the module detail page (static record
	// tab + agent logs), same as the fleet view's module rows.
	Inspect: IsFleetModuleRow,
	// A module stands for the process it monitors — 'v' opens that process's
	// events; 'l' opens the module's own log files, scoped by host + the module's
	// subdirectory under /var/log/dynatrace/oneagent (agentLogFilter), never the
	// process's application logs.
	Drills: map[string]string{"l": "agentlogs", "v": "events"},
}

// fleetLenses split the fleet view by edge-component type. OneAgents are the
// default (there are orders of magnitude more of them); ActiveGates swap in
// their own column set — the node record shapes differ too much to share.
var fleetLenses = []Lens{
	{Name: "oneagents", Desc: "OneAgents — per-host monitoring agents"},
	{Name: "activegates", Desc: "ActiveGates — the tenant's data-collection gateways", Columns: activegateColumns},
	{Name: "modules", Desc: "Agent modules — latest health & version per injection", Columns: fleetModuleStateColumns},
}

// fleetEntity resolves the entity a fleet row stands for. Node rows
// (oneagents/activegates lenses) carry their own Smartscape id and type and
// open their detail page on enter. A module row is the agent's monitoring of
// one process; it is not itself a Smartscape node, so enter opens the module
// detail page (IsFleetModuleRow drives Spec.Inspect) — a static record tab
// (process/host/agent as navigable links) plus the module's own agent logs.
// The entity it resolves to is that monitored process, so the logs/events tabs,
// 'v' drill, and Selection scope by it while the row's Name stays the module's.
func fleetEntity(rec map[string]any) *Entity {
	if id := Str(rec, "id"); id != "" {
		typ := Str(rec, "type")
		if typ == "" {
			typ = "ONEAGENT"
		}
		return &Entity{ID: id, Name: Str(rec, "name"), Type: typ}
	}
	if p := Str(rec, "dt.smartscape.process"); p != "" {
		// A module row scopes its OWN agent logs by host + log subdirectory, not
		// by this process id (see Entity.ModuleHost / moduleLogDir): the metadata
		// snapshot's process rarely matches the process that stamps the module's
		// log files. Carrying the process id still anchors the events tab / 'v'
		// drill (the process's change events) and the Selection scope.
		return &Entity{
			ID:           p,
			Name:         Str(rec, "dt.agent.module.name"),
			Type:         "PROCESS",
			ModuleHost:   Str(rec, "dt.smartscape.host"),
			ModuleLogDir: moduleLogDir(Str(rec, "dt.agent.module.type")),
		}
	}
	return nil
}

// moduleLogDir maps an OneAgent module type (dt.agent.module.type) to its fixed
// log subdirectory under /var/log/dynatrace/oneagent/ (validated live on a tenant).
// A module writes to one such directory on its host regardless of which process
// the ONEAGENT_METADATA snapshot happened to name, so host + this subdir is the
// stable scope for the module's agent logs. An unmapped type returns "" — the
// logs tab then falls back to the (best-effort) process filter in agentLogFilter.
func moduleLogDir(moduleType string) string {
	switch moduleType {
	case "log_analytics":
		return "loganalytics"
	case "os":
		return "os"
	case "java":
		return "java"
	case "go":
		return "go"
	case "python":
		return "python"
	case "nginx":
		return "nginx"
	case "nodejs":
		return "nodejs"
	case "sdk":
		return "sdk"
	case "remote_plugin":
		return "plugin"
	}
	return ""
}

// IsFleetModuleRow reports whether a fleet/modules row is a summarized module
// instance (no Smartscape node id of its own) rather than a OneAgent/ActiveGate
// node row — the rows that open the module detail page (a static record tab +
// the module's agent logs) instead of navigating into a node.
func IsFleetModuleRow(rec map[string]any) bool { return Str(rec, "id") == "" }

var oneagentColumns = []Column{
	{Title: "NAME", Field: "name"},
	{Title: "VERSION", Width: 22, Value: oneagentVersion},
	{Title: "MODE", Width: 13, Value: func(rec map[string]any) string {
		return prettyMonitoringMode(Str(rec, "dt.agent.monitoring_mode"))
	}},
	{Title: "MODULES", Width: 8, Right: true, Value: oneagentModules, Class: oneagentModulesClass,
		Sort: func(rec map[string]any) any { return enrichInt(rec, "unhealthy") }},
	{Title: "ZONE", Width: 12, Field: "dt.network_zone.id"},
	{Title: "CLUSTER", Width: 20, Field: "k8s.cluster.name"},
	{Title: "SEEN", Width: 5, Right: true, Value: lifetimeAge},
}

var activegateColumns = []Column{
	{Title: "NAME", Field: "name"},
	{Title: "VERSION", Width: 22, Value: fieldsFirst("dt.active_gate.version", "version")},
	{Title: "GROUP", Width: 20, Value: fieldsFirst("dt.active_gate.group.name", "dt.activegate_group")},
	{Title: "PURPOSE", Width: 9, Field: "purpose"},
	{Title: "MODULES", Width: 28, Value: func(rec map[string]any) string { return FormatValue(rec["modules"]) }},
	{Title: "ZONE", Width: 12, Field: "dt.network_zone.id"},
	{Title: "OS", Width: 7, Value: func(rec map[string]any) string {
		return strings.TrimPrefix(Str(rec, "os.type"), "OS_TYPE_")
	}},
	{Title: "SEEN", Width: 5, Right: true, Value: lifetimeAge},
}

// fleetOneAgentStateQuery batches the latest FLEET_EVENT metadata for a set of
// OneAgents. ONEAGENT_METADATA is one record per module, re-emitted on a
// heartbeat, so it deduplicates to one row per module before rolling up to
// per-agent counts. Like fleetModulesQuery it avoids a global `sort` (the scan
// limit trap) — takeAny over a fleetStateWindow-capped window is the module's
// current state.
func fleetOneAgentStateQuery(tf Timeframe, keys []string) string {
	return fmt.Sprintf(
		"fetch events, from:%s\n"+
			"| filter event.kind == \"FLEET_EVENT\" and event.type == \"ONEAGENT_METADATA\"\n"+
			"| filter in(dt.smartscape.oneagent, {%s})\n"+
			"| summarize { health = takeAny(dt.agent.module.health), ver = takeAny(dt.agent.installer_version) }, by:{dt.smartscape.oneagent, dt.agent.module.id}\n"+
			"| summarize { modules = count(), unhealthy = countIf(health != \"Healthy\"), version = takeAny(ver) }, by:{dt.smartscape.oneagent}",
		fleetStateWindow(tf), idList(keys))
}

// fleetStateWindow caps a relative fleet-state window at 30m (bounding the
// fetch events scan) while honoring an absolute window verbatim — a fleet view
// pinned to a problem's timespan should read that exact window. 30m spans ~6
// heartbeats (validated live: modules re-emit ~every 5min), so every live
// module appears while the scan stays a few million records — cheap now that
// the queries stream rather than sort (see fleetModulesQuery).
func fleetStateWindow(tf Timeframe) string {
	if tf.Absolute() {
		return tf.DQL()
	}
	if tf.Dur == 0 || tf.Dur > 30*time.Minute {
		return "now() - 30m"
	}
	return tf.DQL()
}

// oneagentVersion prefers the installed agent version from the latest
// FLEET_EVENT metadata (the authoritative full-agent version), falling back to
// the module version the node record carries when enrichment has not landed.
func oneagentVersion(rec map[string]any) string {
	if v := Str(rec, EnrichKey("version")); v != "" {
		return v
	}
	return Str(rec, "dt.agent.module.version")
}

// oneagentModules renders the healthy-module fraction from enrichment
// ("5/7"); empty until (or unless) FLEET_EVENT metadata has been folded in.
func oneagentModules(rec map[string]any) string {
	total := Str(rec, EnrichKey("modules"))
	if total == "" {
		return ""
	}
	t, _ := strconv.Atoi(total)
	bad, _ := strconv.Atoi(Str(rec, EnrichKey("unhealthy")))
	return fmt.Sprintf("%d/%d", t-bad, t)
}

// oneagentModulesClass warns when any module is unhealthy — the signal a fleet
// operator scans for. It reads the rendered "healthy/total" cell: a fraction
// below the whole means at least one module is not Healthy.
func oneagentModulesClass(val string) string {
	healthy, total, ok := strings.Cut(val, "/")
	if !ok {
		return ""
	}
	if healthy != total {
		return "warn"
	}
	return "ok"
}

// enrichInt reads an enrichment count as a number for sorting (unhealthy
// agents sort to the top); NaN-free so a missing value sorts last.
func enrichInt(rec map[string]any, alias string) any {
	if v := Str(rec, EnrichKey(alias)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return nil
}

// prettyMonitoringMode renders the monitoring-mode enum readably
// ("FULL_STACK" → "full-stack").
func prettyMonitoringMode(mode string) string {
	switch mode {
	case "FULL_STACK":
		return "full-stack"
	case "INFRASTRUCTURE":
		return "infra-only"
	case "DISCOVERY":
		return "discovery"
	case "":
		return ""
	default:
		return mode
	}
}

// fleetModuleFilter narrows the FLEET_EVENT stream to the per-module OneAgent
// metadata (validated live: event.type ONEAGENT_METADATA is one record per
// agent module). It backs both the events view's fleet lens (the raw stream)
// and the fleet view's modules lens (the deduplicated latest state). The
// ActiveGate online notices are deliberately excluded — their state lives on
// the ActiveGate node, not in a per-module feed.
const fleetModuleFilter = `event.kind == "FLEET_EVENT" and event.type == "ONEAGENT_METADATA"`

// fleetModuleColumns curate the ONEAGENT_METADATA stream one row per emission:
// which module (and its instance id), on which host, its health, version, and
// how it was injected.
var fleetModuleColumns = []Column{
	eventTimeColumn,
	{Title: "HOST", Field: "host.name", Width: 22},
	{Title: "MODULE", Field: "dt.agent.module.name"},
	{Title: "MODULE ID", Field: "dt.agent.module.id", Width: 16},
	{Title: "TECH", Field: "dt.agent.module.type", Width: 8},
	{Title: "HEALTH", Width: 9, Field: "dt.agent.module.health", Class: classAgentHealth},
	{Title: "VERSION", Width: 22, Field: "dt.agent.module.version"},
	{Title: "INJECT", Width: 7, Field: "dt.agent.module.injection_mode"},
}

// classAgentHealth colors a module health word (validated live: Healthy /
// Critical …): Healthy reads OK, Critical/Failed as an error, anything else in
// between warns.
func classAgentHealth(val string) string {
	switch val {
	case "Healthy", "INFO", "OK":
		return "ok"
	case "":
		return ""
	case "Critical", "Failed", "Error":
		return "error"
	default:
		return "warn"
	}
}

// fleetModuleState is the ONEAGENT_METADATA field set folded onto each module
// row, keyed under its ORIGINAL name (dot paths intact) so the record inspector
// — enter/d on a module row — groups the fields by namespace exactly like the
// raw fleet event. A module is not a Smartscape node, so this summarized record
// IS its detail page; carrying the full event (agent + deployment + placement +
// os + the smartscape link ids) is what makes that detail rich. Each field
// takes an arbitrary in-window emission (takeAny — stable across the ~5min
// heartbeat) except the lifecycle timestamps: takeMax(timestamp) is the newest
// heartbeat (freshness), takeMin the earliest observed first-seen/startup.
var fleetModuleState = []struct{ field, agg string }{
	{"timestamp", "takeMax"},
	{"host.name", "takeAny"},
	{"dt.agent.module.name", "takeAny"},
	{"dt.agent.module.type", "takeAny"},
	{"dt.agent.module.version", "takeAny"},
	{"dt.agent.module.health", "takeAny"},
	{"dt.agent.module.injection_mode", "takeAny"},
	{"dt.agent.module.first_seen", "takeMin"},
	{"dt.agent.module.startup_timestamp", "takeMin"},
	{"dt.agent.installer_version", "takeAny"},
	{"dt.agent.monitoring_mode", "takeAny"},
	{"dt.agent.live_debugger.enabled", "takeAny"},
	{"dt.agent.module.deployment.orchestration_tech", "takeAny"},
	{"dt.agent.module.deployment.orchestrator_id", "takeAny"},
	{"dt.agent.module.deployment.script_version", "takeAny"},
	{"dt.agent.module.deployment.standalone", "takeAny"},
	{"dt.agent.module.deployment.fips", "takeAny"},
	{"dt.process_group.id", "takeAny"},
	{"dt.smartscape.process", "takeAny"},
	{"dt.smartscape.host", "takeAny"},
	{"dt.smartscape.activegate", "takeAny"},
	{"dt.active_gate.id", "takeAny"},
	{"k8s.cluster.name", "takeAny"},
	{"k8s.namespace.name", "takeAny"},
	{"dt.host_group.id", "takeAny"},
	{"dt.network_zone.id", "takeAny"},
	{"os.name", "takeAny"},
	{"os.type", "takeAny"},
	{"os.architecture", "takeAny"},
}

// fleetModuleSummarize renders the dedup summarize: one row per module instance
// keyed by {oneagent, module.id}, each field reconstructed under its original
// name (backticked alias — DQL needs it for a dotted output name).
func fleetModuleSummarize() string {
	parts := make([]string, len(fleetModuleState))
	for i, f := range fleetModuleState {
		parts[i] = fmt.Sprintf("`%s` = %s(%s)", f.field, f.agg, f.field)
	}
	return "summarize { " + strings.Join(parts, ", ") + " }, by:{dt.smartscape.oneagent, dt.agent.module.id}"
}

// fleetModulesQuery lists each injected module's latest known state. The unit
// is the module instance: dt.agent.module.id identifies one module load (tied
// to a process launch — a short-lived process relaunching yields several ids,
// and one process can carry several modules injected at once), so the query
// deduplicates by {oneagent, module.id} — never collapsing distinct modules on
// the same process. It carries dt.smartscape.process so a row drills to the
// process it monitors. When the scope pins a OneAgent (or its host) the feed
// narrows there first — the same query drives the fleet-wide lens and the
// scoped per-agent tab.
//
// Deliberately NOT `sort timestamp desc | takeFirst`: the global sort forces
// Grail to order the whole scanned window and blows the 10s scan limit on a
// busy tenant (validated live on a tenant — a scoped 30m sort+takeFirst timed out
// incomplete, while this streaming form returns fleet-wide in a few seconds
// over 1.2M records). See fleetModuleState for the fold semantics.
func fleetModulesQuery(s Scope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "fetch events, from:%s\n| filter %s", fleetStateWindow(s.Timeframe), fleetModuleFilter)
	if f := fleetModuleScopeFilter(s); f != "" {
		fmt.Fprintf(&b, "\n| filter %s", f)
	}
	fmt.Fprintf(&b, "\n| %s", fleetModuleSummarize())
	b.WriteString("\n| sort `dt.agent.module.health` asc, `host.name` asc\n| limit 1000")
	return b.String()
}

// fleetModuleScopeFilter narrows the module feed to a pinned OneAgent (or the
// host it runs on) — the ONEAGENT_METADATA records carry both smartscape ids
// (validated live). Any other entity type is not a module scope, so the feed
// stays fleet-wide (and CanScope honestly reports the pin as inapplicable).
func fleetModuleScopeFilter(s Scope) string {
	if s.Entity == nil {
		return ""
	}
	switch s.Entity.Type {
	case "ONEAGENT":
		return fmt.Sprintf("dt.smartscape.oneagent == toSmartscapeId(%q)", s.Entity.ID)
	case "HOST":
		return fmt.Sprintf("dt.smartscape.host == toSmartscapeId(%q)", s.Entity.ID)
	}
	return ""
}

// fleetModuleStateColumns render the deduplicated module inventory: one row per
// injected module instance, its host, namespace (K8s workloads), technology,
// health, version, injection mode, and its module id (the instance identity —
// the same module name recurs per process launch, so the id is what
// distinguishes the rows). Sorted unhealthy-first, so the health column and
// the newest heartbeat (SEEN) are the two an operator scans.
var fleetModuleStateColumns = []Column{
	{Title: "HOST", Field: "host.name", Width: 22},
	{Title: "NAMESPACE", Field: "k8s.namespace.name", Width: 16},
	{Title: "MODULE", Field: "dt.agent.module.name"},
	{Title: "TECH", Field: "dt.agent.module.type", Width: 8},
	{Title: "HEALTH", Field: "dt.agent.module.health", Width: 9, Class: classAgentHealth},
	{Title: "VERSION", Field: "dt.agent.module.version", Width: 18},
	{Title: "INJECT", Field: "dt.agent.module.injection_mode", Width: 7},
	{Title: "MODULE ID", Field: "dt.agent.module.id", Width: 16},
	{Title: "SEEN", Width: 5, Right: true, Value: func(rec map[string]any) string { return Age(Str(rec, "timestamp")) }},
}

// IsEdgeComponent reports whether a type is an observability edge component
// (OneAgent, ActiveGate) rather than a monitored workload. Their detail pages
// drop the workload metrics tab that structurally never matches them — a
// OneAgent/ActiveGate is no metric dimension (validated live on a tenant). They DO
// carry logs, but not the injected-workload kind: their signal is the agent's
// own log files (see agentLogsSpec), scoped by host/process + the
// /var/log/dynatrace path, not by the generic entity SignalFilter (which found
// nothing because agent logs carry no smartscape.oneagent/activegate field).
func IsEdgeComponent(t string) bool {
	return t == "ONEAGENT" || t == "ACTIVEGATE"
}

// Agent-log path prefixes. Agent log monitoring (when enabled on a tenant)
// ingests the Dynatrace components' OWN log files. Every such file lives
// under /var/log/dynatrace/ (validated live on a tenant: the only trees there are
// oneagent/, gateway/, synthetic/, sc2/). This path prefix is the load-bearing
// anti-conflation key: it excludes both the injected workloads' application
// logs (which live under the app's own paths) AND other vendors' agents that
// merely match "agent" (/var/log/amazon/ssm/…, network-policy-agent.log).
const (
	dynatraceLogPrefix = "/var/log/dynatrace/"
	oneagentLogPrefix  = "/var/log/dynatrace/oneagent/"
)

// agentLogsSpec surfaces the edge components' own log files — the OneAgent and
// ActiveGate self-logs, NOT the app/infra logs of the processes they monitor.
// The generic logs view can't reach them: agent logs carry only host / process
// scope fields (dt.smartscape.host, dt.smartscape.process) plus the
// /var/log/dynatrace log.source, never a dt.smartscape.oneagent/activegate
// field, so SignalFilter(OneAgent) matches nothing. agentLogFilter composes the
// right scope per entity type; the path prefix keeps it strictly agent logs.
var agentLogsSpec = &Spec{
	Name:    "agentlogs",
	Aliases: []string{"agentlog"},
	Kind:    KindSignal,
	Desc:    "Agent log files — OneAgent/ActiveGate self-logs (agent log monitoring)",
	Query: func(s Scope) string {
		var b strings.Builder
		fmt.Fprintf(&b, "fetch logs, from:%s", s.Timeframe.DQL())
		if f := agentLogFilter(s.Entity); f != "" {
			fmt.Fprintf(&b, "\n| filter %s", f)
		} else {
			// Agent logs are voluminous; an unscopable entity yields nothing
			// rather than a fleet-wide scan (which would blow the scan limit).
			b.WriteString("\n| filter false")
		}
		b.WriteString(LogTail(s))
		return b.String()
	},
	RawReadable: true,
	Columns:     agentLogColumns,
	Entity:      signalSourceEntity,
	// 'a' analyzes the agent logs into Davis patterns, like the generic logs view.
	Drills: map[string]string{"a": "patterns"},
}

// agentLogFilter renders the DQL condition that scopes agent logs to one edge
// component (validated live on a tenant). Each arm pairs an entity-scope filter with
// a log.source path prefix so ONLY the agent's own files match:
//
//   - ONEAGENT: the host it monitors, carrying every module's log. The
//     ONEAGENT id shares its suffix with that HOST id (validated live:
//     2319/2319 agents), so ONEAGENT-<x> → HOST-<x>. All /var/log/dynatrace/
//     oneagent/** files on that host = the OneAgent's combined logs.
//   - PROCESS: a fleet module row scopes by its host + fixed log subdirectory
//     (Entity.ModuleHost / ModuleLogDir), NOT by the resolved process id. A
//     module's log files are stamped with the module's own agent process — a
//     PGI that churns on relaunch and only rarely matches the process the
//     ONEAGENT_METADATA snapshot named (~36% match rate, validated live on a tenant),
//     so a process filter shows nothing most of the time. Host + subdir is the
//     stable join that reaches every module type (log_analytics, os, java, …).
//     A real PROCESS entity (no module fields) or a module type with no known
//     subdir falls back to the best-effort process filter.
//   - ACTIVEGATE: matched by hostname → log host.name. Agent logs carry only
//     the host running the AG (never dt.active_gate.id), so this resolves
//     standalone/dedicated ActiveGates (node name == host name); containerized
//     ActiveGates (pod name != k8s-node host.name) have no reliable node→host
//     link and honestly show nothing. Everything under /var/log/dynatrace/
//     except oneagent/ is the AG's own logs (gateway/, synthetic/, sc2/).
func agentLogFilter(e *Entity) string {
	if e == nil {
		return ""
	}
	switch e.Type {
	case "ONEAGENT":
		host := "HOST-" + strings.TrimPrefix(e.ID, "ONEAGENT-")
		return fmt.Sprintf("dt.smartscape.host == toSmartscapeId(%q) and startsWith(log.source, %q)",
			host, oneagentLogPrefix)
	case "PROCESS":
		// A fleet module row: scope by host + the module's fixed log subdirectory.
		// The module's log files are stamped with the module's own agent process,
		// a churny PGI the metadata snapshot's process id rarely matches — so the
		// process filter below is empty ~64% of the time (validated live on a tenant).
		// Host + subdir is the stable join (trade-off: a tech module shows every
		// same-tech module's logs on the host, since one host can run several).
		if e.ModuleHost != "" && e.ModuleLogDir != "" {
			return fmt.Sprintf("dt.smartscape.host == toSmartscapeId(%q) and startsWith(log.source, %q)",
				e.ModuleHost, oneagentLogPrefix+e.ModuleLogDir+"/")
		}
		// Fallback (real PROCESS entity, or a module type with no known subdir):
		// the deep-monitoring logs stamped with this process, best-effort.
		return fmt.Sprintf("dt.smartscape.process == toSmartscapeId(%q) and startsWith(log.source, %q)",
			e.ID, oneagentLogPrefix)
	case "ACTIVEGATE":
		return fmt.Sprintf("host.name == %q and startsWith(log.source, %q) and not startsWith(log.source, %q)",
			e.Name, dynatraceLogPrefix, oneagentLogPrefix)
	}
	return ""
}

// agentLogColumns curate the agent-log stream: time, level, the file it came
// from (the path tail after /var/log/dynatrace/, which names the module —
// oneagent/java/…, gateway/…), and the line. Mirrors the generic logs view but
// leads with the source file, since which agent module emitted a line is the
// first thing an operator triaging a OneAgent scans.
var agentLogColumns = []Column{
	logTimeColumn,
	logLevelColumn,
	{Title: "FILE", Width: 34, Value: agentLogFile},
	{Title: "CONTENT", Field: "content"},
}

// agentLogFile labels a row with the log file's path under /var/log/dynatrace/
// (e.g. "oneagent/java/ruxitagent_…"), the module-identifying part of the path.
func agentLogFile(rec map[string]any) string {
	return strings.TrimPrefix(Str(rec, "log.source"), dynatraceLogPrefix)
}
