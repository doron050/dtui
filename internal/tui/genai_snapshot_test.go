package tui

import "testing"

// GenAI nodes mirror smartscapeNodes GENAI_*: models carry
// gen_ai.provider.name, services carry dt.service.name, sorted type then name.
func TestSnapshotGenAITable(t *testing.T) {
	pinSnapshotClock(t)

	node := func(id, typ, name, provider, service, start string) map[string]any {
		rec := map[string]any{
			"id": id, "type": typ, "name": name, "dt.security_context": []any{},
			"lifetime": map[string]any{"start": start, "end": "2026-07-15T11:48:00.000000000Z"},
		}
		if provider != "" {
			rec["gen_ai.provider.name"] = provider
		}
		if service != "" {
			rec["dt.service.name"] = service
		}
		return rec
	}
	rows := []map[string]any{
		node("GENAI_AGENT-0000000000000A91", "GENAI_AGENT", "travel-assistant", "", "travel-agent-api", "2026-07-08T15:26:00.000000000Z"),
		node("GENAI_MODEL-0000000000000A92", "GENAI_MODEL", "anthropic.claude-3-5-sonnet-20240620-v1:0", "aws.bedrock", "", "2026-07-08T15:26:00.000000000Z"),
		node("GENAI_MODEL-0000000000000A93", "GENAI_MODEL", "gpt-4o-mini", "openai", "", "2026-06-24T10:39:00.000000000Z"),
		node("GENAI_MODEL-0000000000000A94", "GENAI_MODEL", "text-embedding-3-small", "openai", "", "2026-06-10T10:13:00.000000000Z"),
		node("GENAI_PROVIDER-0000000000000A95", "GENAI_PROVIDER", "openai", "openai", "", "2026-05-29T11:43:00.000000000Z"),
		node("GENAI_SERVICE-0000000000000A96", "GENAI_SERVICE", "travel-agent-api", "", "travel-agent-api", "2026-07-08T15:14:00.000000000Z"),
	}

	a := testApp(t, "genai")
	seedRows(t, a, rows)
	assertSnapshots(t, "genai", a)
}
