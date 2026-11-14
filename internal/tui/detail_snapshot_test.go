package tui

import "testing"

// TestSnapshotHostDetail pins the entity page every drill-down opens, on its
// details tab: identity header, the pulse line (problems, the list row's
// enrichment sparks, age), the vitals block, and the curated key facts. The
// page-level queries are answered the way a quiet OneAgent host answers them:
// no active problems, and all five host vitals reporting.
func TestSnapshotHostDetail(t *testing.T) {
	pinSnapshotClock(t)

	a := testApp(t, "hosts")
	seedRows(t, a, []map[string]any{hostRecord("HOST-0000000000000B01", "ip-10-0-1-17", map[string]any{
		"ip":                       []any{"203.0.113.10", "10.0.1.17"},
		"host.ip":                  []any{"10.0.1.17"},
		"public_ip":                []any{"203.0.113.10"},
		"cloud.provider":           "aws",
		"aws.region":               "us-east-1",
		"aws.availability_zone":    "us-east-1b",
		"aws.resource.id":          "i-0123456789abcdef0",
		"host.type":                "t3a.xlarge",
		"hypervisor.type":          "HYPERVISOR_TYPE_AWS_NITRO",
		"primary_tags.team":        "checkout",
		"primary_tags.environment": "production",
		"__enrich.cpu":             []any{5.8, 5.7, 5.3, 5.2, 5.0, 5.2, 5.3, 5.1, 5.2, 5.4, 6.9, 5.2},
		"__enrich.mem":             []any{15.9, 15.8, 15.8, 15.8, 15.7, 15.8, 15.8, 15.9, 15.8, 16.4, 16.1, 15.8},
	})})
	press(a, key("enter"))
	dv := a.top().(*detailView)
	dv.Update(dataMsg{owner: pulseOwner{dv}, seq: dv.pulseSeq})
	dv.Update(dataMsg{owner: vitalsProbeOwner{dv}, seq: dv.vitalsSeq, records: []map[string]any{
		{"metric.key": "dt.host.cpu.usage"},
		{"metric.key": "dt.host.memory.usage"},
		{"metric.key": "dt.host.disk.used.percent"},
		{"metric.key": "dt.host.net.nic.bytes_rx"},
		{"metric.key": "dt.host.net.nic.bytes_tx"},
	}})
	dv.Update(dataMsg{owner: vitalsOwner{dv}, seq: dv.vitalsSeq, records: []map[string]any{{
		"cpu":    []any{5.8, 5.7, 5.3, 5.2, 5.0, 5.2, 5.3, 5.1, 5.2, 5.4, 6.9, 5.2},
		"mem":    []any{15.9, 15.8, 15.8, 15.8, 15.7, 15.8, 15.8, 15.9, 15.8, 16.4, 16.1, 15.8},
		"disk":   []any{52.7, 52.7, 52.8, 52.8, 52.8, 52.8, 52.8, 52.8, 52.8, 52.8, 52.8, 52.8},
		"net_rx": []any{24100.0, 25300.0, 26800.0, 31900.0, 29400.0, 22800.0, 25100.0, 26700.0, 27300.0, 24900.0, 30200.0, 31300.0},
		"net_tx": []any{88200.0, 90400.0, 93100.0, 102700.0, 98500.0, 86900.0, 89300.0, 91800.0, 92400.0, 88800.0, 95700.0, 98700.0},
	}}})
	assertSnapshots(t, "host-detail", a)
}
