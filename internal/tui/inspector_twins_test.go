package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// ADR-0016: the record inspector (d) shows only real Grail keys. The
// app-derived subscription name lives under a __-hidden key and is surfaced
// only in curated views (peek + enter), so in the raw record dump it must not
// appear at all — neither as a Grail-looking key nor under its display label.
// The real GUID key stays visible.
func TestInspectorRecordModeHidesDerivedSubscriptionName(t *testing.T) {
	rec := map[string]any{
		"id":                        "AZURE_MICROSOFT_COMPUTE_DISKS-1",
		"type":                      "AZURE_MICROSOFT_COMPUTE_DISKS",
		"azure.subscription":        "22222222-3333-4444-5555-666666666666",
		catalog.SubscriptionNameKey: "Dynatrace-SaaS-Prod",
	}
	v := newInspectorView(&dataSource{}, "disk", rec) // record mode: no KeyFacts
	v.resize(120, 40)
	body := ansi.Strip(strings.Join(v.lines, "\n"))

	if strings.Contains(body, "azure.subscription.name") {
		t.Errorf("record dump must not show the synthetic key as a Grail key:\n%s", body)
	}
	if strings.Contains(body, "Azure subscription name") || strings.Contains(body, "Dynatrace-SaaS-Prod") {
		t.Errorf("record dump must not surface the app-derived name at all:\n%s", body)
	}
	// The real GUID key is a genuine Grail field — still shown.
	if !strings.Contains(body, "azure.subscription") || !strings.Contains(body, "22222222-3333-4444-5555-666666666666") {
		t.Errorf("real azure.subscription GUID key missing:\n%s", body)
	}
}

// A span carries the same entities under both the current-era dt.smartscape.*
// and the legacy dt.entity.* fields. The links block shows each target once,
// preferring the smartscape field; the legacy twin is not hoisted as a
// duplicate link but still reads below in its dt.* namespace group.
func TestInspectorPrefersSmartscapeEntityTwin(t *testing.T) {
	rec := map[string]any{
		"span.name":                        "GET /checkout",
		"http.request.method":              "GET",
		"trace.id":                         "0123456789abcdef0123456789abcdef",
		"dt.entity.service":                "SERVICE-0000000000000B01",
		"dt.smartscape.service":            "SERVICE-0000000000000B01",
		"dt.entity.process_group_instance": "PROCESS_GROUP_INSTANCE-0000000000000C01",
		"dt.smartscape.process":            "PROCESS-0000000000000C01",
	}
	v := newInspectorView(&dataSource{}, "span", rec)
	v.resize(120, 40) // first resize builds the view

	keys := map[string]bool{}
	for _, r := range v.rows {
		keys[r.key] = true
	}

	// Every field still reads somewhere — the legacy twin is only kept out of
	// the links block, not dropped.
	for _, want := range []string{
		"dt.smartscape.service", "dt.smartscape.process",
		"dt.entity.service", "dt.entity.process_group_instance",
	} {
		if !keys[want] {
			t.Errorf("%s missing from inspector rows", want)
		}
	}

	// The smartscape ref is hoisted into the links block near the top; its
	// legacy twin reads below in the dt.* namespace group. So the links header
	// and the smartscape ref both precede the legacy twin.
	body := ansi.Strip(strings.Join(v.lines, "\n"))
	linksAt := strings.Index(body, "links")
	if linksAt < 0 {
		t.Fatalf("no links section rendered:\n%s", body)
	}
	smartAt := strings.Index(body, "dt.smartscape.service")
	legacyAt := strings.Index(body, "dt.entity.service")
	if !(linksAt < smartAt && smartAt < legacyAt) {
		t.Errorf("want links(%d) < dt.smartscape.service(%d) < dt.entity.service(%d)", linksAt, smartAt, legacyAt)
	}
}
