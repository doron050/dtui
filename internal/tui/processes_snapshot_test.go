package tui

import "testing"

// processRecord mirrors a smartscapeNodes "PROCESS" record from a
// OneAgent-fed tenant: process-group name, host, port array, the
// process.metadata map (command line, pod), and the
// process.software_technologies.os detection list processTech reads.
func processRecord(id, name string, techs []string, containerized bool, meta map[string]any, extra map[string]any) map[string]any {
	detected := make([]any, len(techs))
	for i, tech := range techs {
		detected[i] = map[string]any{"edition": "", "type": tech, "version": ""}
	}
	rec := map[string]any{
		"id": id, "id_classic": "PROCESS_GROUP_INSTANCE-" + id[len("PROCESS-"):], "type": "PROCESS",
		"name":                             name,
		"dt.process_group.detected_name":   name,
		"dt.process_group.id":              "PROCESS_GROUP-" + id[len("PROCESS-"):],
		"host.name":                        "ip-10-0-1-17",
		"process.bitness":                  "64",
		"process.containerized":            containerized,
		"process.has_public_traffic":       false,
		"process.software_technologies.os": detected,
		"process.metadata":                 meta,
		"dt.security_context":              []any{},
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

func TestSnapshotProcessesTable(t *testing.T) {
	pinSnapshotClock(t)

	podMeta := func(cmd, pod, ns, container string) map[string]any {
		return map[string]any{
			"COMMAND_LINE_ARGS":         cmd,
			"KUBERNETES_FULL_POD_NAME":  pod,
			"KUBERNETES_NAMESPACE":      ns,
			"KUBERNETES_CONTAINER_NAME": container,
			"KUBERNETES_BASE_POD_NAME":  container,
		}
	}
	rows := []map[string]any{
		processRecord("PROCESS-0000000000000C01", "CoreDNS coredns-*", []string{"CONTAINERD", "GO", "CORE_DNS"}, true,
			podMeta("/coredns -conf /etc/coredns/Corefile", "coredns-5d78c9869d-abcde", "kube-system", "coredns"),
			map[string]any{
				"port":         []any{"53", "8080", "8181", "9153"},
				"__enrich.cpu": []any{0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.2, 0.1, 0.1, 0.1},
				"__enrich.mem": []any{0.3, 0.3, 0.3, 0.3, 0.3, 0.3, 0.3, 0.3, 0.3, 0.3, 0.3, 0.3},
			}),
		processRecord("PROCESS-0000000000000C02", "Linux System", []string{"CONTAINERD", "LINUX_SYSTEM"}, false,
			map[string]any{"EXE_NAME": "systemd"},
			map[string]any{
				"__enrich.cpu": []any{0.1, 0.1, 0.2, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
				"__enrich.mem": []any{0.9, 1.0, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1},
			}),
		processRecord("PROCESS-0000000000000C03", "OneAgent log analytics", []string{"CONTAINERD", "DYNATRACE"}, true,
			podMeta("oneagentloganalytics", "oneagent-xk2pq", "dynatrace", "oneagent"),
			map[string]any{
				"lifetime":     map[string]any{"start": "2026-07-13T09:00:00.000000000Z", "end": "2026-07-15T11:55:00.000000000Z"},
				"__enrich.cpu": []any{0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0},
				"__enrich.mem": []any{0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4},
			}),
		processRecord("PROCESS-0000000000000C04", "Short-lived processes", nil, false, nil,
			map[string]any{
				"__enrich.cpu": []any{0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
				"__enrich.mem": []any{0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
			}),
		processRecord("PROCESS-0000000000000C05", "SpringBoot orders-api com.example.orders.Application orders-api-*", []string{"CONTAINERD", "JAVA", "APACHE_TOMCAT"}, true,
			podMeta("-jar /app/orders-api.jar", "orders-api-7c9d8f6b5-q4wzt", "orders", "orders-api"),
			map[string]any{
				"port":         []any{"8080"},
				"__enrich.cpu": []any{0.2, 0.2, 0.1, 0.2, 0.3, 0.2, 0.2, 0.1, 0.2, 0.2, 0.4, 0.2},
				"__enrich.mem": []any{1.8, 1.8, 1.8, 1.8, 1.8, 1.8, 1.8, 1.8, 1.8, 1.8, 1.8, 1.8},
			}),
		processRecord("PROCESS-0000000000000C06", "nginx", []string{"NGINX"}, false,
			map[string]any{"COMMAND_LINE_ARGS": "-g daemon off;", "EXE_NAME": "nginx"},
			map[string]any{
				"port":         []any{"80", "443"},
				"__enrich.cpu": []any{0.0, 0.1, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0},
				"__enrich.mem": []any{0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
			}),
	}

	a := testApp(t, "processes")
	seedRows(t, a, rows)
	assertSnapshots(t, "processes", a)
}
