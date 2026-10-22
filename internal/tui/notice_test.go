package tui

import (
	"strings"
	"testing"

	"github.com/dynatrace-oss/dtctl/pkg/exec"
)

func TestClassifyNoticeByType(t *testing.T) {
	cases := map[string]int{
		"SCAN_LIMIT_GBYTES":       noticeScanLimit,
		"RESULT_LIMIT_RECORDS":    noticeResultLimit,
		"RESULT_LIMIT_BYTES":      noticeResultLimit,
		"FETCH_TIMEOUT":           noticeTimeout,
		"SAMPLING_APPLIED":        noticeSampling,
		"QUERY_CONSUMPTION_LIMIT": noticeNone, // no dedicated banner
		"":                        noticeNone,
	}
	for typ, want := range cases {
		if got := classifyNotice(typ, ""); got != want {
			t.Errorf("classifyNotice(%q) = %d, want %d", typ, got, want)
		}
	}
}

// The live scan-limit warning carries no stable type on some deployments, so
// the message text is the fallback signal (validated live on a large tenant).
func TestClassifyNoticeByMessageFallback(t *testing.T) {
	msg := "Your execution was stopped after 500 gigabytes of data were scanned."
	if got := classifyNotice("", msg); got != noticeScanLimit {
		t.Errorf("scan-limit message classified as %d, want scan limit", got)
	}
	if got := classifyNotice("", "The result has been limited to 1000 records"); got != noticeResultLimit {
		t.Errorf("result-limit message classified as %d, want result limit", got)
	}
}

func TestNoticeForSkipsInfoSeverity(t *testing.T) {
	if _, ok := noticeFor([]exec.QueryNotification{
		{Severity: "INFO", NotificationType: "SCAN_LIMIT_GBYTES"},
	}); ok {
		t.Error("INFO-severity notifications must not raise a banner")
	}
}

func TestNoticeForScanLimitIsPartial(t *testing.T) {
	n, ok := noticeFor([]exec.QueryNotification{
		{Severity: "WARNING", NotificationType: "SCAN_LIMIT_GBYTES",
			Message: "stopped after 500 gigabytes of data were scanned"},
	})
	if !ok || !n.warn {
		t.Fatalf("scan-limit warning should be a partial-result notice: %+v ok=%v", n, ok)
	}
	if !strings.Contains(n.text, "narrow the timeframe") {
		t.Errorf("notice must offer the mitigation, got %q", n.text)
	}
	if !partialNotice([]exec.QueryNotification{
		{Severity: "WARNING", NotificationType: "SCAN_LIMIT_GBYTES"},
	}) {
		t.Error("partialNotice should be true for a scan-limit warning")
	}
}

func TestNoticeForSamplingIsNotPartial(t *testing.T) {
	n, ok := noticeFor([]exec.QueryNotification{
		{Severity: "WARNING", NotificationType: "SAMPLING_APPLIED"},
	})
	if !ok || n.warn {
		t.Errorf("sampling is approximate, not partial: %+v ok=%v", n, ok)
	}
	if partialNotice([]exec.QueryNotification{{Severity: "WARNING", NotificationType: "SAMPLING_APPLIED"}}) {
		t.Error("sampling must not count as a partial result")
	}
}

func scanLimitNotices() []exec.QueryNotification {
	return []exec.QueryNotification{{
		Severity:         "WARNING",
		NotificationType: "SCAN_LIMIT_GBYTES",
		Message:          "stopped after 500 gigabytes of data were scanned",
	}}
}

// A scan-limit notification on the visible view's own result must reach the
// footer — Grail returns it as an empty success, so this banner is the only
// signal the user gets that the answer is incomplete.
func TestScanLimitNoticeSurfacedInFooter(t *testing.T) {
	a := testApp(t, "problems")
	top := a.top()

	a.Update(dataMsg{owner: top}) // clean result: no banner
	if strings.Contains(a.View(), "scan limit") {
		t.Fatal("no notice expected before a scan-limit result")
	}

	a.Update(dataMsg{owner: top, notices: scanLimitNotices()})
	if !strings.Contains(a.View(), "scan limit") {
		t.Fatal("scan-limit notice must appear in the footer")
	}

	a.Update(dataMsg{owner: top}) // later clean result on the same view
	if strings.Contains(a.View(), "scan limit") {
		t.Error("a clean result must clear the notice")
	}
}

// A secondary or background query must not clear the visible view's warning.
func TestNoticeIgnoresNonTopOwner(t *testing.T) {
	a := testApp(t, "problems")
	a.Update(dataMsg{owner: a.top(), notices: scanLimitNotices()})
	a.Update(dataMsg{owner: dictOwner{}}) // a different owner, clean
	if !strings.Contains(a.View(), "scan limit") {
		t.Error("a foreign owner's result must not clear the top view's notice")
	}
}

func TestNoticeClearedOnNavigate(t *testing.T) {
	a := testApp(t, "problems")
	a.Update(dataMsg{owner: a.top(), notices: scanLimitNotices()})
	if !strings.Contains(a.View(), "scan limit") {
		t.Fatal("precondition: notice should be showing")
	}
	a.navigate(newInspectorView(a.ds, "x", map[string]any{}), false)
	if strings.Contains(a.View(), "scan limit") {
		t.Error("leaving the view must drop its notice")
	}
}
