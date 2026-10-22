package catalog

import "testing"

func TestResourceNamespace(t *testing.T) {
	// Unambiguous resource-context namespaces sink; the record's own payload
	// namespaces and dt.* plumbing stay put (not resource).
	resource := []string{
		"host", "k8s", "cloud", "container", "service", "os", "process",
		"deployment", "telemetry", "faas", "device", "browser", "os_service",
		"aws", "azure", "gcp",
		"java", "dotnet", "nodejs", "go", "php",
	}
	notResource := []string{
		"", "dt", // plumbing — its own tier, never resource here
		"http",        // signal-dominant (http.request.method / status_code)
		"cics", "zos", // niche + genuinely mixed → stay visible
		"log", "db", "span", "exception", "error", "url", "messaging",
		"gen_ai", "network", "user", "trace", "request",
		"unknown_custom_otlp_resource_attr", // conservative default: not demoted
	}
	for _, ns := range resource {
		if !ResourceNamespace(ns) {
			t.Errorf("ResourceNamespace(%q) = false, want true", ns)
		}
	}
	for _, ns := range notResource {
		if ResourceNamespace(ns) {
			t.Errorf("ResourceNamespace(%q) = true, want false", ns)
		}
	}
}
