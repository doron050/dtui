package tui

import "testing"

// TestSnapshotHostDetail pins the entity page every drill-down opens, on its
// details tab: identity header, the pulse line (problems, the list row's
// enrichment sparks, age), the vitals block, and the curated key facts. The
// page-level queries are answered the way a quiet OneAgent host answers them:
// no active problems, and the cpu/memory vitals reporting (disk absent).
func TestSnapshotHostDetail(t *testing.T) {
	pinSnapshotClock(t)

	a := testApp(t, "hosts")
	seedRows(t, a, []map[string]any{hostRecord("HOST-0000000000000B01", "ip-10-0-1-17", map[string]any{
		"ip":                       []any{"203.0.113.10", "10.0.1.17"},
		"host.ip":                  []any{"10.0.1.17"},
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
	}})
	dv.Update(dataMsg{owner: vitalsOwner{dv}, seq: dv.vitalsSeq, records: []map[string]any{{
		"cpu": []any{5.8, 5.7, 5.3, 5.2, 5.0, 5.2, 5.3, 5.1, 5.2, 5.4, 6.9, 5.2},
		"mem": []any{15.9, 15.8, 15.8, 15.8, 15.7, 15.8, 15.8, 15.9, 15.8, 16.4, 16.1, 15.8},
	}}})
	assertSnapshots(t, "host-detail", a)
}
