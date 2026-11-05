package tui

import "testing"

// vulnRecord mirrors one row of the vulnerabilities list query: the
// summarize-by-vulnerability.id rollup of VULNERABILITY-level
// security.events state reports, with vulnSummarizeAliases' keys and the
// affected_entities.count rollup (a string, as Grail returns it).
func vulnRecord(id, displayID, level string, score float64, stack, tech, exposure, exploit string, fix bool, cve, title string) map[string]any {
	return map[string]any{
		"vulnerability.id": id,
		"display_id":       displayID,
		"title":            title,
		"level":            level,
		"score":            score,
		"cvss":             9.8,
		"status":           "OPEN",
		"muted":            "NOT_MUTED",
		"stack":            stack,
		"tech":             tech,
		"cve":              []any{cve},
		"exposure":         exposure,
		"exploit":          exploit,
		"fix":              fix,
		"code_location":    nil,
		"affected":         "1",
		"url":              "https://tenant.example.invalid/ui/security/problem/" + id,
	}
}

func TestSnapshotVulnerabilitiesTable(t *testing.T) {
	pinSnapshotClock(t)

	rows := []map[string]any{
		vulnRecord("1000000000000000001", "S-101", "CRITICAL", 9.1, "CODE_LIBRARY", "JAVA", "PUBLIC_NETWORK", "AVAILABLE", true,
			"CVE-2026-10001", "Remote code execution in example-json deserializer"),
		vulnRecord("1000000000000000002", "S-102", "HIGH", 7.6, "CODE_LIBRARY", "JAVA", "NOT_DETECTED", "NOT_AVAILABLE", true,
			"CVE-2026-10002", "HTTP/2 request headers not validated in example-http server"),
		vulnRecord("1000000000000000003", "S-103", "HIGH", 7.6, "CODE_LIBRARY", "JAVA", "NOT_DETECTED", "AVAILABLE", true,
			"CVE-2026-10003", "Authentication bypass by capture-replay in example-auth digest authenticator"),
		vulnRecord("1000000000000000004", "S-104", "HIGH", 7.1, "CODE_LIBRARY", "JAVA", "NOT_DETECTED", "NOT_AVAILABLE", false,
			"CVE-2026-10004", "Stack overflow on deeply nested input in example-core parser"),
		vulnRecord("1000000000000000005", "S-105", "MEDIUM", 6.5, "CONTAINER_ORCHESTRATION", "KUBERNETES", "NOT_AVAILABLE", "NOT_AVAILABLE", true,
			"CVE-2026-10005", "Container may run with a writable root filesystem"),
		vulnRecord("1000000000000000006", "S-106", "MEDIUM", 5.3, "CONTAINER_ORCHESTRATION", "KUBERNETES", "NOT_AVAILABLE", "NOT_AVAILABLE", false,
			"CVE-2026-10006", "Service account token auto-mounted into workload pods"),
		vulnRecord("1000000000000000007", "S-107", "LOW", 3.7, "CODE_LIBRARY", "JAVA", "NOT_DETECTED", "NOT_AVAILABLE", true,
			"CVE-2026-10007", "Information exposure through example-logging serialization"),
	}

	a := testApp(t, "vulnerabilities")
	seedRows(t, a, rows)
	assertSnapshots(t, "vulnerabilities", a)
}
