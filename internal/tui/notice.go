package tui

import (
	"strings"

	"github.com/dynatrace-oss/dtctl/pkg/exec"
)

// queryNotice is a distilled, user-facing summary of a Grail query
// notification. Grail fails silently — a scan-limit hit or fetch timeout comes
// back as an empty result with exit 0 (docs/dev/learnings.md) — so without this
// the user cannot tell a genuinely empty answer from a truncated one. The text
// leads with what happened and the one lever that helps most: narrow the
// timeframe (the 't' picker).
type queryNotice struct {
	text string // banner text, incl. the mitigation hint
	warn bool   // WARNING/ERROR severity: the result may be PARTIAL/incomplete
}

// noticeFor picks the most actionable notice from a query's notifications, or
// reports false when none warrant surfacing. dtctl's exec package owns the
// same mapping for its CLI/agent output but keeps it unexported; the category
// set (scan limit, result limit, fetch timeout, sampling) mirrors
// exec.classifyNotification so the TUI and CLI stay in step.
func noticeFor(ns []exec.QueryNotification) (queryNotice, bool) {
	for _, n := range ns {
		switch strings.ToUpper(n.Severity) {
		case "WARNING", "WARN", "ERROR":
		default:
			continue // INFO and below are noise in a one-line banner
		}
		switch classifyNotice(n.NotificationType, n.Message) {
		case noticeScanLimit:
			return queryNotice{warn: true,
				text: "partial result — Grail scan limit reached; narrow the timeframe (t) to scan less"}, true
		case noticeResultLimit:
			return queryNotice{warn: true,
				text: "result truncated to the row limit — narrow the timeframe (t) or add a filter"}, true
		case noticeTimeout:
			return queryNotice{warn: true,
				text: "fetch timed out — result may be incomplete; narrow the timeframe (t)"}, true
		case noticeSampling:
			return queryNotice{text: "results are sampled (approximate)"}, true
		}
	}
	return queryNotice{}, false
}

// partialNotice reports whether the notifications say the result may be
// incomplete (scan limit, truncation, or timeout) — the callers that must not
// react to an empty result as if the data simply weren't there.
func partialNotice(ns []exec.QueryNotification) bool {
	n, ok := noticeFor(ns)
	return ok && n.warn
}

// Notice categories, mirroring exec's notif* set (unexported there).
const (
	noticeNone = iota
	noticeScanLimit
	noticeResultLimit
	noticeTimeout
	noticeSampling
)

// classifyNotice maps a notification to a category by its type, falling back to
// a message-text match — not every deployment stamps a stable type, which is
// why the live-observed message ("stopped after N gigabytes of data were
// scanned") is a second signal.
func classifyNotice(notificationType, message string) int {
	switch notificationType {
	case "SCAN_LIMIT_GBYTES":
		return noticeScanLimit
	case "RESULT_LIMIT_RECORDS", "RESULT_LIMIT_BYTES":
		return noticeResultLimit
	case "FETCH_TIMEOUT":
		return noticeTimeout
	case "SAMPLING_APPLIED":
		return noticeSampling
	}
	msg := strings.ToLower(message)
	switch {
	case strings.Contains(msg, "scan") && strings.Contains(msg, "gigabyte"):
		return noticeScanLimit
	case strings.Contains(msg, "result has been limited") || strings.Contains(msg, "limited to"):
		return noticeResultLimit
	case strings.Contains(msg, "timed out") || strings.Contains(msg, "timeout"):
		return noticeTimeout
	}
	return noticeNone
}
