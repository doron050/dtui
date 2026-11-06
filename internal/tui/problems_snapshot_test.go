package tui

import "testing"

// problemRecord mirrors a dt.davis.problems update-record: display_id doubles
// as event.id, both ID-era affected arrays (classic affected_entity_* plus
// smartscape.affected_entities, the latter absent on environment-wide metric
// events), single-element arrays for provider/impact, and event.end only once
// CLOSED. event.severity is present on some records and absent on others.
func problemRecord(id, status, category, name, start string, extra map[string]any) map[string]any {
	rec := map[string]any{
		"display_id":                       id,
		"event.id":                         id,
		"event.kind":                       "DAVIS_PROBLEM",
		"event.status":                     status,
		"event.status_transition":          "UPDATED",
		"event.category":                   category,
		"event.name":                       name,
		"event.start":                      start,
		"event.provider":                   []any{"METRIC_EVENTS"},
		"propagated_event_providers":       []any{"METRIC_EVENTS"},
		"dt.davis.event_ids":               []any{"-4711000000000000001_1752580920000"},
		"dt.davis.impact_level":            []any{"Environment"},
		"dt.davis.is_duplicate":            false,
		"dt.davis.is_frequent_event":       false,
		"dt.davis.mute.status":             "NOT_MUTED",
		"dt.analysis.ready":                true,
		"maintenance.is_under_maintenance": false,
		"affected_entity_ids":              []any{"ENVIRONMENT-0000000000000E01"},
		"affected_entity_names":            []any{"demo-environment"},
		"affected_entity_types":            []any{"dt.entity.environment"},
		"timestamp":                        "2026-07-15T11:57:27.852000000Z",
	}
	for k, v := range extra {
		if v == nil {
			delete(rec, k)
			continue
		}
		rec[k] = v
	}
	return rec
}

// k8sNodeProblem is the Kubernetes anomaly-detection shape: a K8S_NODE in
// both ID eras, the cluster as related entity, k8s.* context arrays, and the
// alerting profiles that matched.
func k8sNodeProblem() map[string]any {
	return problemRecord("P-26071542", "ACTIVE", "AVAILABILITY", "Kubernetes node not ready",
		"2026-07-15T11:42:00.000000000Z", map[string]any{
			"event.provider":               []any{"KUBERNETES_ANOMALY_DETECTION"},
			"propagated_event_providers":   []any{"KUBERNETES_ANOMALY_DETECTION"},
			"event.severity":               "2",
			"event.description":            "Node **aks-system-31337-vmss000004** in cluster **demo-aks-westeu** has been in NotReady state for more than 3 minutes. Pods scheduled on the node may be evicted. Last kubelet heartbeat: 2026-07-15T11:38:41Z.",
			"dt.davis.impact_level":        []any{"Infrastructure"},
			"dt.davis.event_ids":           []any{"-4711000000000000002_1752579720000", "-4711000000000000003_1752579780000"},
			"affected_entity_ids":          []any{"KUBERNETES_NODE-0000000000000F01"},
			"affected_entity_names":        []any{"aks-system-31337-vmss000004"},
			"affected_entity_types":        []any{"dt.entity.kubernetes_node"},
			"dt.entity.kubernetes_node":    []any{"KUBERNETES_NODE-0000000000000F01"},
			"dt.entity.kubernetes_cluster": []any{"KUBERNETES_CLUSTER-0000000000000F02"},
			"k8s.cluster.name":             []any{"demo-aks-westeu"},
			"k8s.cluster.uid":              []any{"00000000-0000-4000-8000-000000000f02"},
			"k8s.node.name":                []any{"aks-system-31337-vmss000004"},
			"entity_tags":                  []any{"env:prod"},
			"labels.alerting_profile":      []any{"Default", "platform-oncall", "k8s-infra"},
			"related_entity_ids":           []any{"KUBERNETES_CLUSTER-0000000000000F02"},
			"related_entity_names":         []any{"demo-aks-westeu"},
			"smartscape.affected_entities": []any{
				map[string]any{"id": "K8S_NODE-0000000000000F11", "name": "aks-system-31337-vmss000004", "type": "K8S_NODE"},
			},
			"smartscape.affected_entity.ids":   []any{"K8S_NODE-0000000000000F11"},
			"smartscape.affected_entity.types": []any{"K8S_NODE"},
			"smartscape.related_entities": []any{
				map[string]any{"id": "K8S_CLUSTER-0000000000000F12", "name": "demo-aks-westeu", "type": "K8S_CLUSTER"},
			},
		})
}

// problemRows is a problems page as a busy multi-cloud tenant returns it:
// environment-wide metric-event alerts next to entity-scoped K8s and service
// problems, one already CLOSED, sorted event.start desc.
func problemRows() []map[string]any {
	return []map[string]any{
		problemRecord("P-26071546", "ACTIVE", "CUSTOM_ALERT", "Ingest volume above forecast for log bucket default_logs",
			"2026-07-15T11:52:00.000000000Z", nil),
		k8sNodeProblem(),
		problemRecord("P-26071541", "CLOSED", "CUSTOM_ALERT", "Synthetic availability below 99% (HTTP monitors)",
			"2026-07-15T11:41:00.000000000Z", map[string]any{
				"event.status_transition":   "CLOSED",
				"event.end":                 "2026-07-15T11:46:00.000000000Z",
				"resolved_problem_duration": "300000000000",
			}),
		problemRecord("P-26071538", "ACTIVE", "ERROR", "Failure rate increase",
			"2026-07-15T11:20:00.000000000Z", map[string]any{
				"event.provider":        []any{"DAVIS_ANOMALY_DETECTION"},
				"event.severity":        "3",
				"dt.davis.impact_level": []any{"Services"},
				"affected_entity_ids":   []any{"SERVICE-0000000000000A01", "SERVICE-0000000000000A02", "SERVICE-0000000000000A03"},
				"affected_entity_names": []any{"OrderController", "storefront-web", "PaymentGateway"},
				"affected_entity_types": []any{"dt.entity.service", "dt.entity.service", "dt.entity.service"},
				"smartscape.affected_entities": []any{
					map[string]any{"id": "SERVICE-0000000000000A01", "name": "OrderController", "type": "SERVICE"},
					map[string]any{"id": "SERVICE-0000000000000A02", "name": "storefront-web", "type": "SERVICE"},
					map[string]any{"id": "SERVICE-0000000000000A03", "name": "PaymentGateway", "type": "SERVICE"},
				},
			}),
		problemRecord("P-26071533", "ACTIVE", "RESOURCE_CONTENTION", "Memory saturation",
			"2026-07-15T10:05:00.000000000Z", map[string]any{
				"event.provider":        []any{"ONEAGENT"},
				"dt.davis.impact_level": []any{"Infrastructure"},
				"affected_entity_ids":   []any{"HOST-0000000000000B07"},
				"affected_entity_names": []any{"ip-10-0-4-30.ec2.internal"},
				"affected_entity_types": []any{"dt.entity.host"},
				"smartscape.affected_entities": []any{
					map[string]any{"id": "HOST-0000000000000B07", "name": "ip-10-0-4-30.ec2.internal", "type": "HOST"},
				},
			}),
	}
}

func TestSnapshotProblemsTable(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "problems")
	seedRows(t, a, problemRows())
	assertSnapshots(t, "problems", a)
}

// TestSnapshotProblemPage pins the investigation front door on its overview
// tab: identity + status line, curated facts (k8s context, impact), the
// affected entity row, and Davis's description wrapped below.
func TestSnapshotProblemPage(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "problems")
	seedRows(t, a, []map[string]any{k8sNodeProblem()})
	press(a, key("enter"))
	if _, ok := a.top().(*problemView); !ok {
		t.Fatalf("top = %T, want *problemView", a.top())
	}
	assertSnapshots(t, "problem-page", a)
}

// TestSnapshotProblemEvidence pins the evidence tab: the constituent Davis
// events in the summarized shape DavisEventsSpec's query returns (one row per
// event.id; start/end/type/name/status/severity/rootcause/source*).
func TestSnapshotProblemEvidence(t *testing.T) {
	pinSnapshotClock(t)
	a := testApp(t, "problems")
	seedRows(t, a, []map[string]any{k8sNodeProblem()})
	press(a, key("enter"))
	pv := a.top().(*problemView)
	idx := -1
	for i, tab := range pv.tabs {
		if tab.name == "evidence" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("no evidence tab")
	}
	deliverView(pv, pv.setActive(idx))
	ev := pv.tabs[idx].view.(*tableView)
	ev.Update(dataMsg{owner: ev, seq: ev.seq, records: []map[string]any{
		{
			"event.id": "-4711000000000000003_1752579780000", "start": "2026-07-15T11:43:00.000000000Z",
			"type": "KUBERNETES_NODE_NOT_READY", "name": "Kubernetes node not ready", "status": "ACTIVE",
			"severity": "2", "rootcause": true,
			"source_id": "K8S_NODE-0000000000000F11", "source_type": "K8S_NODE",
			"description": "Node has been in NotReady state for more than 3 minutes.",
		},
		{
			"event.id": "-4711000000000000002_1752579720000", "start": "2026-07-15T11:42:00.000000000Z",
			"end": "2026-07-15T11:48:44.303000000Z", "type": "PROCESS_RESTART", "name": "Process restart",
			"status": "CLOSED", "rootcause": false,
			"source_id": "PROCESS-0000000000000C21", "source_type": "PROCESS",
			"description": "kubelet process restarted 3 times in 5 minutes.",
		},
	}})
	assertSnapshots(t, "problem-evidence", a)
}
