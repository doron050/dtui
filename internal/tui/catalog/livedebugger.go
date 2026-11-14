package catalog

import (
	"fmt"
	"strings"

	"github.com/dynatrace-oss/dtctl/pkg/output"
)

// Live Debugger: snapshots captured by non-breaking breakpoints land in the
// application.snapshots Grail table (one record per hit, keyed by the uid
// breakpoint.id — dtctl's `get snapshots` filters it with toUid()). dtui reads
// them through DQL only. Breakpoint configuration lives behind the Live
// Debugger GraphQL API, whose workspace lookup (getOrCreateUserWorkspaceV2)
// may create a workspace — a write, so it stays out of dtui (ADR-0011). The
// breakpoints view is therefore derived from the snapshots: it lists the
// breakpoints that fired in the timeframe, never the disabled or silent ones.

var breakpointsSpec = &Spec{
	Name:    "breakpoints",
	Aliases: []string{"bp", "breakpoint"},
	Kind:    KindSignal,
	Desc:    "Live Debugger breakpoints that fired (from snapshots) — enter lists the hits",
	Query: func(s Scope) string {
		var b strings.Builder
		fmt.Fprintf(&b, "fetch application.snapshots, from:%s", s.Timeframe.DQL())
		if f := ScopeSignalFilter(s); f != "" {
			fmt.Fprintf(&b, "\n| filter %s", f)
		}
		b.WriteString("\n| summarize {hits = count(), last_hit = max(timestamp), " +
			"`code.filepath` = takeAny(code.filepath), `code.line.number` = takeAny(code.line.number), " +
			"`code.function` = takeAny(code.function), hosts = countDistinct(host.name)}, by:{breakpoint.id}")
		b.WriteString("\n| fieldsAdd breakpoint.id = toString(breakpoint.id)")
		b.WriteString("\n| sort last_hit desc\n| limit 500")
		return b.String()
	},
	Columns: []Column{
		{Title: "LOCATION", Width: 40, Value: snapshotLocation},
		{Title: "FUNCTION", Field: "code.function"},
		{Title: "HITS", Width: 7, Right: true, Value: func(rec map[string]any) string { return FormatValue(rec["hits"]) },
			Sort: func(rec map[string]any) any { return rec["hits"] }},
		{Title: "HOSTS", Width: 5, Right: true, Value: func(rec map[string]any) string { return FormatValue(rec["hosts"]) }},
		{Title: "LAST HIT", Width: 12, Value: func(rec map[string]any) string { return FormatTime(Str(rec, "last_hit")) },
			Sort: func(rec map[string]any) any { return Str(rec, "last_hit") }},
		{Title: "BREAKPOINT ID", Width: 32, Field: "breakpoint.id"},
	},
	EnterTarget: "snapshots",
	EnterArg:    func(rec map[string]any) string { return Str(rec, "breakpoint.id") },
	// A breakpoint's hits must be the same population the row counted: the
	// pinned entity and any search/facet narrowing carry into the snapshots.
	EnterInheritsScope: true,
	Drills:             map[string]string{},
}

var snapshotsSpec = &Spec{
	Name:    "snapshots",
	Aliases: []string{"snap", "snapshot", "livedebugger", "ld"},
	Kind:    KindSignal,
	Desc:    "Live Debugger snapshots — decoded variables and stack per hit",
	Query: func(s Scope) string {
		var b strings.Builder
		fmt.Fprintf(&b, "fetch application.snapshots, from:%s", s.Timeframe.DQL())
		if s.Arg != "" {
			fmt.Fprintf(&b, "\n| filter breakpoint.id == toUid(%q)", s.Arg)
		}
		if f := ScopeSignalFilter(s); f != "" {
			fmt.Fprintf(&b, "\n| filter %s", f)
		}
		b.WriteString("\n| sort timestamp desc\n| limit 200")
		return b.String()
	},
	Columns: []Column{
		{Title: "TIME", Width: 12, Value: func(rec map[string]any) string { return FormatTime(Str(rec, "timestamp")) },
			Sort: func(rec map[string]any) any { return Str(rec, "timestamp") }},
		{Title: "LOCATION", Width: 34, Value: snapshotLocation},
		{Title: "FUNCTION", Width: 24, Field: "code.function"},
		{Title: "HOST", Width: 20, Value: fieldsFirst("k8s.pod.name", "host.name")},
		{Title: "THREAD", Width: 16, Field: "thread.name"},
		{Title: "SUMMARY", Width: 22, Field: SnapshotSummaryKey},
		{Title: "MESSAGE", Field: "snapshot.message"},
	},
	Entity: snapshotEntity,
	Trace:  fieldsFirst("trace.id", "trace_id"),
	Decode: DecodeSnapshots,
	Echo: func(s Scope, _ string) string {
		if s.Arg == "" || s.Entity != nil || len(s.Entities) > 0 {
			return ""
		}
		return fmt.Sprintf("dtctl get snapshots %s --decode-snapshots", s.Arg)
	},
	Drills: map[string]string{"l": "logs", "v": "events", "s": "trace"},
}

// snapshotLocation renders the breakpoint location as File.java:306.
func snapshotLocation(rec map[string]any) string {
	file := Str(rec, "code.filepath")
	if i := strings.LastIndexAny(file, `/\`); i >= 0 {
		file = file[i+1:]
	}
	if line := FormatValue(rec["code.line.number"]); line != "" && file != "" {
		return file + ":" + line
	}
	return file
}

// snapshotEntity resolves the runtime entity a snapshot was captured in,
// trying the source-entity convention first, then the process and host
// references. nil when the record carries none.
func snapshotEntity(rec map[string]any) *Entity {
	if e := signalSourceEntity(rec); e != nil {
		return e
	}
	for _, ref := range []struct{ field, typ, name string }{
		{"dt.smartscape.process", "PROCESS", "process.executable.name"},
		{"dt.smartscape.host", "HOST", "host.name"},
	} {
		if id := Str(rec, ref.field); id != "" {
			return &Entity{ID: id, Name: Str(rec, ref.name), Type: ref.typ}
		}
	}
	return nil
}

// Keys DecodeSnapshots lifts out of the decoded payload. They are undotted so
// the inspector lists them first, ahead of the namespaced record fields;
// "stacktrace" matches the inspector's callstack rendering (top frame first,
// the rest behind a keypress).
const (
	SnapshotVariablesKey = "variables"
	SnapshotStackKey     = "stacktrace"
	SnapshotSummaryKey   = "__snapshot.summary"
)

// DecodeSnapshots decodes the protobuf snapshot.data payloads with dtctl's
// decoder (variant wrappers simplified to plain values) and lifts the hit
// frame's locals and the traceback into top-level fields. Records without a
// payload pass through unchanged; a payload that fails to decode keeps
// dtctl's snapshot.decode_error.
func DecodeSnapshots(records []map[string]any) []map[string]any {
	out := output.DecodeSnapshotRecords(records, true)
	for _, rec := range out {
		parsed, ok := rec["parsed_snapshot"].(map[string]any)
		if !ok {
			continue
		}
		delete(rec, "parsed_snapshot")
		rookout, _ := parsed["rookout"].(map[string]any)
		if rookout == nil {
			rec["snapshot.parsed"] = parsed
			continue
		}
		var summary []string
		if frame, _ := rookout["frame"].(map[string]any); frame != nil {
			if locals, ok := frame["locals"].(map[string]any); ok {
				rec[SnapshotVariablesKey] = locals
				summary = append(summary, fmt.Sprintf("%d locals", len(locals)))
				delete(frame, "locals")
			}
		}
		if tb, ok := rookout["traceback"].([]any); ok && len(tb) > 0 {
			frames := make([]string, 0, len(tb))
			for _, f := range tb {
				frames = append(frames, formatFrame(f))
			}
			rec[SnapshotStackKey] = strings.Join(frames, "\n")
			summary = append(summary, fmt.Sprintf("%d frames", len(tb)))
			delete(rookout, "traceback")
		}
		rec[SnapshotSummaryKey] = strings.Join(summary, " · ")
		rec["snapshot.parsed"] = parsed
	}
	return out
}

// formatFrame renders one traceback frame as "fn() at File.java:42".
func formatFrame(f any) string {
	m, ok := f.(map[string]any)
	if !ok {
		return FormatValue(f)
	}
	loc := Str(m, "filename")
	if line := FormatValue(m["line"]); line != "" && loc != "" {
		loc += ":" + line
	}
	fn := Str(m, "function")
	switch {
	case fn != "" && loc != "":
		return fn + "() at " + loc
	case fn != "":
		return fn + "()"
	}
	return loc
}
