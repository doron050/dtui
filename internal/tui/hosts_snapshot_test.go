package tui

import "testing"

// hostRecord mirrors a smartscapeNodes "HOST" record from a OneAgent-fed
// tenant (the hosts query keeps every field but k8s/aws/azure objects and
// references): Smartscape id + lifetime, string-typed core/memory counts,
// ip arrays, OS_TYPE_/HYPERVISOR_TYPE_ enums, and primary_tags.* keys.
func hostRecord(id, name string, extra map[string]any) map[string]any {
	rec := map[string]any{
		"id": id, "id_classic": id, "type": "HOST", "name": name,
		"host.name":               name,
		"os.type":                 "OS_TYPE_LINUX",
		"os.architecture":         "x86",
		"os.version":              "Ubuntu 22.04.3 LTS (Jammy Jellyfish) (kernel 6.2.0-1017-aws)",
		"cores":                   "2",
		"logical_cores":           "4",
		"memory":                  "16612761600",
		"host.physical.memory":    "16612761600",
		"host.has_public_traffic": false,
		"dt.security_context":     []any{},
		"lifetime": map[string]any{
			"start": "2026-06-15T15:39:00.000000000Z",
			"end":   "2026-07-15T11:55:00.000000000Z",
		},
	}
	for k, v := range extra {
		rec[k] = v
	}
	return rec
}

func TestSnapshotHostsTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		hostRecord("HOST-0000000000000B01", "ip-10-0-1-17", map[string]any{
			"ip":                       []any{"203.0.113.10", "10.0.1.17"},
			"public_ip":                []any{"203.0.113.10"},
			"host.ip":                  []any{"10.0.1.17"},
			"mac":                      []any{"02:00:00:00:00:01"},
			"private_dns_name":         "ip-10-0-1-17.ec2.internal",
			"cloud.provider":           "aws",
			"aws.account.id":           "123456789012",
			"aws.region":               "us-east-1",
			"aws.availability_zone":    "us-east-1b",
			"aws.resource.id":          "i-0123456789abcdef0",
			"host.type":                "t3a.xlarge",
			"hypervisor.type":          "HYPERVISOR_TYPE_AWS_NITRO",
			"k8s.cluster.name":         "demo-cluster",
			"k8s.node.name":            "ip-10-0-1-17",
			"primary_tags.team":        "checkout",
			"primary_tags.environment": "production",
			"__enrich.cpu":             []any{5.8, 5.7, 5.3, 5.2, 5.0, 5.2, 5.3, 5.1, 5.2, 5.4, 6.9, 5.2},
			"__enrich.mem":             []any{15.9, 15.8, 15.8, 15.8, 15.7, 15.8, 15.8, 15.9, 15.8, 16.4, 16.1, 15.8},
		}),
		hostRecord("HOST-0000000000000B02", "ip-10-0-2-41", map[string]any{
			"ip":                    []any{"10.0.2.41"},
			"host.ip":               []any{"10.0.2.41"},
			"cloud.provider":        "aws",
			"aws.region":            "us-east-1",
			"aws.availability_zone": "us-east-1a",
			"aws.resource.id":       "i-0fedcba9876543210",
			"host.type":             "m5.2xlarge",
			"hypervisor.type":       "HYPERVISOR_TYPE_AWS_NITRO",
			"logical_cores":         "8",
			"cores":                 "4",
			"memory":                "33285996544",
			"primary_tags.team":     "payments",
			"__enrich.cpu":          []any{41.0, 44.5, 52.3, 61.0, 72.4, 78.9, 83.1, 79.6, 70.2, 64.8, 58.1, 62.7},
			"__enrich.mem":          []any{48.2, 48.9, 50.1, 51.4, 52.0, 53.3, 54.8, 55.1, 55.0, 54.2, 53.9, 54.4},
		}),
		hostRecord("HOST-0000000000000B03", "build-agent-03", map[string]any{
			"os.type":         "OS_TYPE_WINDOWS",
			"os.version":      "Windows Server 2022 Datacenter 21H2, ver. 10.0.20348",
			"ip":              []any{"192.168.10.23"},
			"host.ip":         []any{"192.168.10.23"},
			"hypervisor.type": "HYPERVISOR_TYPE_VMWARE",
			"logical_cores":   "16",
			"cores":           "8",
			"memory":          "68719476736",
			"lifetime": map[string]any{
				"start": "2026-07-13T08:00:00.000000000Z",
				"end":   "2026-07-15T11:55:00.000000000Z",
			},
			// An agent that stopped reporting: the enrichment series is all gaps.
			"__enrich.cpu": []any{nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
			"__enrich.mem": []any{nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
		}),
	}

	a := testApp(t, "hosts")
	seedRows(t, a, rows)
	assertSnapshots(t, "hosts", a)
}
