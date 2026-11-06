package tui

import "testing"

// Modules mirror the fleetModulesQuery summarize over ONEAGENT_METADATA
// FLEET_EVENTs: host, optional k8s namespace, module name/type/version/
// health/injection mode/id, and the latest timestamp (SEEN).
func TestSnapshotModulesTable(t *testing.T) {
	pinSnapshotClock(t)

	mod := func(host, ns, name, typ, health, version, mode, id, ts string) map[string]any {
		rec := map[string]any{
			"host.name": host, "dt.agent.module.name": name, "dt.agent.module.type": typ,
			"dt.agent.module.health": health, "dt.agent.module.version": version,
			"dt.agent.module.injection_mode": mode, "dt.agent.module.id": id, "timestamp": ts,
			"dt.agent.installer_version": version, "os.type": "OS_TYPE_LINUX", "os.name": "Linux",
			"dt.smartscape.host":     "HOST-0000000000000B11",
			"dt.smartscape.process":  "PROCESS-0000000000000C31",
			"dt.smartscape.oneagent": "ONEAGENT-0000000000000B21",
		}
		if ns != "" {
			rec["k8s.namespace.name"] = ns
		}
		return rec
	}
	rows := []map[string]any{
		mod("ip-10-0-1-17", "", "Apache Tomcat orders-api", "java", "Critical", "1.325.47.20251127-161245", "full", "8f2c1e0a9b7d6c54", "2026-07-15T11:49:54.263000000Z"),
		mod("ip-10-0-1-17", "", "Apache Tomcat billing", "java", "Critical", "1.325.47.20251127-161245", "full", "8f2c1e0a9b7d6c55", "2026-07-15T11:49:51.020000000Z"),
		mod("aks-user-27182-vmss00000a", "checkout", "SpringBoot cart-api", "java", "Healthy", "1.327.12.20260108-093312", "runtime", "1a2b3c4d5e6f7081", "2026-07-15T11:49:39.103000000Z"),
		mod("aks-user-27182-vmss00000a", "checkout", "node storefront-web", "nodejs", "Healthy", "1.327.12.20260108-093312", "runtime", "1a2b3c4d5e6f7082", "2026-07-15T11:48:12.554000000Z"),
		mod("win-build-agent-07", "", "IIS app pool DefaultAppPool", "dotnet", "Warning", "1.321.30.20250911-120501", "full", "9d8c7b6a59483726", "2026-07-15T11:41:02.000000000Z"),
	}

	a := testApp(t, "modules")
	seedRows(t, a, rows)
	assertSnapshots(t, "modules", a)
}
