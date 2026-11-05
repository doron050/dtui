package tui

import "testing"

// containerRecord mirrors a smartscapeNodes "CONTAINER" record from a
// OneAgent-monitored K8s node: registry-qualified image name plus a separate
// version tag, containerd runtime, the k8s.* placement fields, and lifetime.
func containerRecord(id, name, image, version, ns, pod string, cpu, mem []any) map[string]any {
	return map[string]any{
		"id": id, "id_classic": "CONTAINER_GROUP_INSTANCE-" + id[len("CONTAINER-"):], "type": "CONTAINER",
		"name":                    name,
		"container.image.name":    image,
		"container.image.version": version,
		"container.image.digest":  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"container.runtime.name":  "containerd",
		"host.name":               "ip-10-0-1-17",
		"k8s.cluster.name":        "demo-cluster",
		"k8s.node.name":           "ip-10-0-1-17",
		"k8s.namespace.name":      ns,
		"k8s.pod.name":            pod,
		"aws.region":              "us-east-1",
		"dt.security_context":     []any{},
		"lifetime": map[string]any{
			"start": "2026-06-15T15:41:00.000000000Z",
			"end":   "2026-07-15T11:55:00.000000000Z",
		},
		"__enrich.cpu": cpu,
		"__enrich.mem": mem,
	}
}

func TestSnapshotContainersTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		containerRecord("CONTAINER-0000000000000E01", "activegate", "registry.example.invalid/linux/activegate", "1.349.62-raw",
			"dynatrace", "dynakube-activegate-0",
			[]any{41.0, 38.0, 44.0, 40.0, 39.0, 45.0, 42.0, 40.0}, []any{6.1e8, 6.2e8, 6.2e8, 6.3e8, 6.2e8, 6.4e8, 6.3e8, 6.3e8}),
		containerRecord("CONTAINER-0000000000000E02", "coredns", "docker.io/rancher/mirrored-coredns-coredns", "1.11.3",
			"kube-system", "coredns-5c6d7e8f9a-p9r8s",
			[]any{2.0, 2.0, 3.0, 2.0, 2.0, 2.0, 3.0, 2.0}, []any{2.1e7, 2.1e7, 2.1e7, 2.2e7, 2.1e7, 2.1e7, 2.1e7, 2.2e7}),
		containerRecord("CONTAINER-0000000000000E03", "order-service", "ghcr.io/example/shop/order-service", "2.14.0",
			"shop", "order-service-7d9f8b6c5d-x2k4q",
			[]any{120.0, 180.0, 240.0, 310.0, 280.0, 220.0, 190.0, 205.0}, []any{4.2e8, 4.4e8, 4.9e8, 5.3e8, 5.1e8, 4.8e8, 4.7e8, 4.7e8}),
		containerRecord("CONTAINER-0000000000000E04", "otel-collector", "otel/opentelemetry-collector-contrib", "0.119.0",
			"observability", "otel-collector-6b7c8d9e0f-q1w2e",
			[]any{15.0, 14.0, 16.0, 15.0, 17.0, 15.0, 14.0, 15.0}, []any{9.0e7, 9.1e7, 9.1e7, 9.2e7, 9.2e7, 9.1e7, 9.2e7, 9.3e7}),
	}
	a := testApp(t, "containers")
	seedRows(t, a, rows)
	assertSnapshots(t, "containers", a)
}
