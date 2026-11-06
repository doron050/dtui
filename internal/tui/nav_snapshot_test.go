package tui

import (
	"fmt"
	"testing"
)

// navCensus mirrors the overview's census result: one row per node type with
// the count as a string (Grail summarize counts are strings), count desc.
func navCensus() []map[string]any {
	return []map[string]any{
		{"type": "PROCESS", "count": "99547"},
		{"type": "CONTAINER", "count": "81405"},
		{"type": "AZURE_MICROSOFT_DBFORPOSTGRESQL_FLEXIBLESERVERS_CONFIGURATIONS", "count": "57055"},
		{"type": "K8S_POD", "count": "47857"},
		{"type": "K8S_SECRET", "count": "28813"},
		{"type": "SERVICE", "count": "9120"},
		{"type": "HOST", "count": "2731"},
		{"type": "K8S_NODE", "count": "1144"},
		{"type": "K8S_CLUSTER", "count": "78"},
	}
}

// navSchema mirrors the type-level schema aggregate (source_type, verb,
// target_type, string count).
func navSchema() []map[string]any {
	edge := func(src, verb, dst, n string) map[string]any {
		return map[string]any{"source_type": src, "type": verb, "target_type": dst, "count": n}
	}
	return []map[string]any{
		edge("PROCESS", "calls", "PROCESS", "288610"),
		edge("PROCESS", "runs_on", "HOST", "99506"),
		edge("CONTAINER", "belongs_to", "K8S_CLUSTER", "79501"),
		edge("CONTAINER", "belongs_to", "K8S_NAMESPACE", "79501"),
		edge("CONTAINER", "is_part_of", "K8S_POD", "79501"),
		edge("PROCESS", "runs_on", "CONTAINER", "61230"),
		edge("PROCESS", "belongs_to", "PROCESS_GROUP", "58117"),
		edge("SERVICE", "runs_on", "PROCESS", "12044"),
		edge("CONTAINER", "runs_on", "HOST", "9934"),
		edge("HOST", "runs_on", "AWS_EC2_INSTANCE", "1630"),
	}
}

// TestSnapshotNavOverview pins the navigator's front page: the type census
// with the schema pane for the highlighted type (PROCESS, the busiest).
func TestSnapshotNavOverview(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "nav")
	v := seedNav(t, a, navCensus())
	v.Update(dataMsg{owner: navSchemaOwner{v}, seq: v.seq, records: navSchema()})
	v.Update(dataMsg{owner: navProblemsOwner{v}, seq: v.probSeq})
	assertSnapshots(t, "nav-overview", a)
}

// TestSnapshotNavBrowser pins the type browser: HOST instances in the
// TypeInstancesQuery shape (id, name, display, type), with the highlighted
// host's preview filled from a node record.
func TestSnapshotNavBrowser(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "problems")
	press(a, key(":"))
	press(a, key("nav host"))
	press(a, key("enter"))
	names := []string{
		"ip-10-0-4-30.ec2.internal", "ip-10-0-4-12.ec2.internal", "ip-10-0-5-201.ec2.internal",
		"aks-system-31337-vmss000004", "gke-demo-pool-1-a1b2c3d4-x9k2", "win-build-agent-07",
	}
	rows := make([]map[string]any, len(names))
	for i, n := range names {
		rows[i] = map[string]any{"id": fmt.Sprintf("HOST-0000000000000B%02d", i+1), "name": n, "display": n, "type": "HOST"}
	}
	v := seedNav(t, a, rows)
	v.Update(dataMsg{owner: navProblemsOwner{v}, seq: v.probSeq})
	v.detail["HOST-0000000000000B01"] = navHostDetail()
	assertSnapshots(t, "nav-browser", a)
}

// navHostDetail mirrors the preview's DetailQuery record for an AWS-hosted
// OneAgent host (string counts, ip arrays, enum-typed os/hypervisor, the
// runs_on.aws_ec2_instance reference).
func navHostDetail() map[string]any {
	return hostRecord("HOST-0000000000000B01", "ip-10-0-4-30.ec2.internal", map[string]any{
		"os.version":                       "Amazon Linux 2 (kernel 5.10.234-225.910.amzn2.x86_64)",
		"cores":                            "8",
		"logical_cores":                    "16",
		"memory":                           "32212254720",
		"host.physical.memory":             "32212254720",
		"host.type":                        "c6i.4xlarge",
		"hypervisor.type":                  "HYPERVISOR_TYPE_AWS_NITRO",
		"cloud.provider":                   "aws",
		"aws.region":                       "us-east-1",
		"aws.availability_zone":            "us-east-1a",
		"ip":                               []any{"203.0.113.78", "10.0.4.30"},
		"host.ip":                          []any{"10.0.4.30"},
		"public_ip":                        []any{"203.0.113.78"},
		"maintenance.is_under_maintenance": true,
		"references":                       map[string]any{"runs_on.aws_ec2_instance": []any{"AWS_EC2_INSTANCE-0000000000000B91"}},
		"lifetime":                         map[string]any{"start": "2025-07-01T11:29:00.000000000Z", "end": "2026-07-15T11:45:00.000000000Z"},
	})
}

// TestSnapshotNavWalk pins walk mode around a host: the ego edges grouped by
// direction + verb (processes and containers running on it, the EC2 instance
// it runs on), names resolved, and the root's preview.
func TestSnapshotNavWalk(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "problems")
	press(a, key(":"))
	press(a, key("nav HOST-0000000000000B01"))
	press(a, key("enter"))
	const root = "HOST-0000000000000B01"
	edges := []map[string]any{edgeRec(root, "HOST", "runs_on", "AWS_EC2_INSTANCE-0000000000000B91", "AWS_EC2_INSTANCE")}
	names := []map[string]any{
		{"id": root, "name": "ip-10-0-4-30.ec2.internal", "type": "HOST"},
		{"id": "AWS_EC2_INSTANCE-0000000000000B91", "name": "i-0123456789abcdef0", "type": "AWS_EC2_INSTANCE"},
	}
	procs := []string{"java orders-api", "node storefront-web", "OneAgent network monitor", "containerd",
		"kubelet", "python3 report-worker", "envoy", "nginx"}
	for i, p := range procs {
		id := fmt.Sprintf("PROCESS-0000000000000C%02d", i+1)
		edges = append(edges, edgeRec(id, "PROCESS", "runs_on", root, "HOST"))
		names = append(names, map[string]any{"id": id, "name": p, "type": "PROCESS"})
	}
	for i, c := range []string{"orders-api", "storefront-web"} {
		id := fmt.Sprintf("CONTAINER-0000000000000D%02d", i+1)
		edges = append(edges, edgeRec(id, "CONTAINER", "runs_on", root, "HOST"))
		names = append(names, map[string]any{"id": id, "name": c, "type": "CONTAINER"})
	}
	v := seedNav(t, a, edges)
	v.Update(dataMsg{owner: navNameOwner{v}, seq: v.seq, records: names})
	v.Update(dataMsg{owner: navProblemsOwner{v}, seq: v.probSeq})
	v.detail[root] = navHostDetail()
	assertSnapshots(t, "nav-walk", a)
}
