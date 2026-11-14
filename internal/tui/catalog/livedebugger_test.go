package catalog

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/dynatrace-oss/dtctl/pkg/proto/livedebugger"
)

func TestBreakpointsQuery(t *testing.T) {
	q := Lookup("bp").Query(Scope{Timeframe: Timeframe{Label: "2h", Dur: 2 * time.Hour}})
	for _, want := range []string{
		"fetch application.snapshots, from:now() - 2h",
		"by:{breakpoint.id}",
		"hits = count()",
		"`code.filepath` = takeAny(code.filepath)",
		"`code.line.number` = takeAny(code.line.number)",
		"`code.function` = takeAny(code.function)",
		"last_hit = max(timestamp)",
		"breakpoint.id = toString(breakpoint.id)",
		"| sort last_hit desc",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("breakpoints query missing %q:\n%s", want, q)
		}
	}
	if breakpointsSpec.EnterTarget != "snapshots" {
		t.Errorf("enter on a breakpoint must open its snapshots")
	}
	if got := breakpointsSpec.EnterArg(map[string]any{"breakpoint.id": "bp-0001"}); got != "bp-0001" {
		t.Errorf("EnterArg = %q", got)
	}
}

func TestSnapshotsQueryAndEcho(t *testing.T) {
	spec := Lookup("snap")
	if spec != snapshotsSpec {
		t.Fatalf("alias snap → %v", spec)
	}
	tf := Timeframe{Label: "2h", Dur: 2 * time.Hour}
	bare := spec.Query(Scope{Timeframe: tf})
	if strings.Contains(bare, "breakpoint.id") || !strings.Contains(bare, "| sort timestamp desc") {
		t.Errorf("bare snapshots query:\n%s", bare)
	}
	if spec.Echo(Scope{Timeframe: tf}, bare) != "" {
		t.Error("an unscoped snapshot list echoes its dtctl query")
	}
	s := Scope{Timeframe: tf, Arg: "bp-0001"}
	if q := spec.Query(s); !strings.Contains(q, `filter breakpoint.id == toUid("bp-0001")`) {
		t.Errorf("breakpoint-scoped query:\n%s", q)
	}
	if got := spec.Echo(s, ""); got != "dtctl get snapshots bp-0001 --decode-snapshots" {
		t.Errorf("echo = %q", got)
	}
	host := &Entity{ID: "HOST-0001", Type: "HOST"}
	if spec.Echo(Scope{Timeframe: tf, Arg: "bp-0001", Entity: host}, "") != "" {
		t.Error("an entity-narrowed list is not what dtctl get snapshots returns")
	}
}

func TestSnapshotLocationAndEntity(t *testing.T) {
	rec := map[string]any{"code.filepath": "src/main/java/org/example/OrderController.java", "code.line.number": float64(306)}
	if got := snapshotLocation(rec); got != "OrderController.java:306" {
		t.Errorf("location = %q", got)
	}
	e := snapshotEntity(map[string]any{"dt.smartscape.process": "PROCESS-0001", "process.executable.name": "java"})
	if e == nil || e.Type != "PROCESS" || e.ID != "PROCESS-0001" || e.Name != "java" {
		t.Errorf("entity = %+v", e)
	}
	if snapshotEntity(map[string]any{}) != nil {
		t.Error("a record with no entity reference has no entity")
	}
}

func TestLiveDebuggerState(t *testing.T) {
	for in, want := range map[any]string{true: "on", "true": "on", false: "off", "false": "off", nil: ""} {
		if got := liveDebuggerState(map[string]any{"dt.agent.live_debugger.enabled": in}); got != want {
			t.Errorf("%v → %q, want %q", in, got, want)
		}
	}
}

// TestDecodeSnapshotsLiftsFrame: the hit frame's locals and the traceback
// are lifted to top-level fields with a table summary; the remainder stays
// under snapshot.parsed.
func TestDecodeSnapshotsLiftsFrame(t *testing.T) {
	recs := DecodeSnapshots([]map[string]any{{
		"snapshot.id": "snap-0001",
		"parsed_snapshot": map[string]any{"rookout": map[string]any{
			"frame": map[string]any{
				"function": "send", "filename": "Sender.java", "line": float64(62),
				"locals": map[string]any{"count": float64(42), "name": "alpha"},
			},
			"traceback": []any{
				map[string]any{"function": "send", "filename": "Sender.java", "line": float64(62)},
				map[string]any{"function": "run", "filename": "Worker.java", "line": float64(17)},
			},
		}},
	}})
	rec := recs[0]
	if _, ok := rec["parsed_snapshot"]; ok {
		t.Error("parsed_snapshot must be replaced by the lifted fields")
	}
	locals, _ := rec[SnapshotVariablesKey].(map[string]any)
	if locals["count"] != float64(42) || locals["name"] != "alpha" {
		t.Errorf("variables = %v", rec[SnapshotVariablesKey])
	}
	if got := rec[SnapshotStackKey]; got != "send() at Sender.java:62\nrun() at Worker.java:17" {
		t.Errorf("stacktrace = %q", got)
	}
	if got := rec[SnapshotSummaryKey]; got != "2 locals · 2 frames" {
		t.Errorf("summary = %q", got)
	}
	frame := rec["snapshot.parsed"].(map[string]any)["rookout"].(map[string]any)["frame"].(map[string]any)
	if _, ok := frame["locals"]; ok || frame["function"] != "send" {
		t.Errorf("remaining frame = %v", frame)
	}
}

// TestDecodeSnapshotsPayload runs a synthetic protobuf payload through dtctl's
// decoder: the encoded fields are dropped, the decoded tree is kept.
func TestDecodeSnapshotsPayload(t *testing.T) {
	strs := []map[string]uint32{{"": 0}, {"root": 1}, {"java.lang.String": 2}, {"hello": 3}}
	value := &livedebugger.Variant2{
		VariantTypeMaxDepth:      uint32(livedebugger.Variant_VARIANT_STRING) << 1,
		OriginalTypeIndexInCache: 2, BytesIndexInCache: 3, OriginalSize: 5,
	}
	ns := &livedebugger.Variant2{
		VariantTypeMaxDepth:   uint32(livedebugger.Variant_VARIANT_NAMESPACE) << 1,
		AttributeNamesInCache: []uint32{1},
		AttributeValues:       []*livedebugger.Variant2{value},
	}
	payload, err := proto.Marshal(&livedebugger.AugReportMessage{Arguments2: ns})
	if err != nil {
		t.Fatal(err)
	}
	strMap, err := json.Marshal(strs)
	if err != nil {
		t.Fatal(err)
	}
	rec := DecodeSnapshots([]map[string]any{{
		"snapshot.id":         "snap-0001",
		"snapshot.data":       base64.StdEncoding.EncodeToString(payload),
		"snapshot.string_map": string(strMap),
	}})[0]
	if _, ok := rec["snapshot.data"]; ok {
		t.Error("the encoded payload must be dropped once decoded")
	}
	parsed, _ := rec["snapshot.parsed"].(map[string]any)
	if parsed["root"] != "hello" {
		t.Errorf("snapshot.parsed = %v", rec["snapshot.parsed"])
	}

	bad := DecodeSnapshots([]map[string]any{{"snapshot.data": "not base64!"}})[0]
	if Str(bad, "snapshot.decode_error") == "" {
		t.Errorf("a broken payload keeps a decode error: %v", bad)
	}
}
