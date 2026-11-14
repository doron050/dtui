package tui

import "testing"

// TestSnapshotActiveGatesTable pins the fleet view's ActiveGates lens. The
// record mirrors smartscapeNodes "ACTIVEGATE": dt.active_gate.* identity and
// version, network zone, the modules array, OS enum, and lifetime.
func TestSnapshotActiveGatesTable(t *testing.T) {
	pinSnapshotClock(t)

	ag := func(id, name, version, group string, modules []any, containerized bool) map[string]any {
		return map[string]any{
			"id": id, "type": "ACTIVEGATE", "name": name,
			"dt.active_gate.id":         "0x" + id[len(id)-8:],
			"dt.active_gate.version":    version,
			"dt.active_gate.group.name": group,
			"dt.network_zone.id":        "default",
			"modules":                   modules,
			"is_containerized":          containerized,
			"is_fips":                   false,
			"addresses":                 []any{name, "10.42.0.19"},
			"os.type":                   "OS_TYPE_LINUX",
			"os.architecture":           "amd64",
			"os.version":                "Red Hat Enterprise Linux 9.4 (Plow) (kernel: 6.2.0-1017-aws)",
			"startup_time":              "2026-07-14T15:36:01.026000000Z",
			"dt.security_context":       []any{},
			"lifetime": map[string]any{
				"start": "2026-06-15T15:38:00.000000000Z",
				"end":   "2026-07-15T11:57:00.000000000Z",
			},
		}
	}
	rows := []map[string]any{
		ag("ACTIVEGATE-0000000000000F01", "dynakube-activegate-0", "1.349.62.20260627-101708", "default",
			[]any{"DEBUGGING", "KUBERNETES", "ODIN_COLLECTOR", "AGENT_ROUTING"}, true),
		ag("ACTIVEGATE-0000000000000F02", "ag-eu-central-01", "1.347.40.20260601-093000", "eu-central",
			[]any{"AGENT_ROUTING", "SYNTHETIC", "EXTENSIONS_V2"}, false),
	}
	// Only some ActiveGate records carry purpose (docs/dev/learnings.md).
	rows[1]["purpose"] = "routing"
	a := testApp(t, "fleet")
	press(a, key("]"))
	seedRows(t, a, rows)
	assertSnapshots(t, "activegates", a)
}
