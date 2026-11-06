package tui

import "testing"

// Databases: no tenant exposes DB_INSTANCE_POSTGRES nodes yet, so the rows
// are shaped like the nearest live Smartscape database record (a managed
// Postgres instance node: name, db.system, cloud/account fields, lifetime) —
// db.instance.version is absent there, so VERSION renders blank. The active
// connections enrichment is keyed by id like the spec's EnrichSpec.
func TestSnapshotDatabasesTable(t *testing.T) {
	pinSnapshotClock(t)

	db := func(id, name, start string, active []any) map[string]any {
		rec := map[string]any{
			"id": id, "name": name, "type": "DB_INSTANCE_POSTGRES", "db.system": "postgresql",
			"cloud.provider": "aws", "aws.region": "eu-central-1", "aws.state": "available",
			"dt.security_context": []any{},
			"lifetime":            map[string]any{"start": start, "end": "2026-07-15T11:59:00.000000000Z"},
		}
		if active != nil {
			rec["__enrich.active"] = active
		}
		return rec
	}
	rows := []map[string]any{
		db("DB_INSTANCE_POSTGRES-0000000000000A81", "orders-db", "2026-06-05T06:18:00.000000000Z",
			[]any{4.0, 5.0, 4.0, 6.0, 9.0, 7.0, 5.0, 4.0, 5.0, 12.0, 6.0, nil}),
		db("DB_INSTANCE_POSTGRES-0000000000000A82", "orders-db-replica-1", "2026-06-05T06:18:00.000000000Z",
			[]any{1.0, 1.0, 2.0, 1.0, 1.0, 1.0, 2.0, 1.0, 1.0, 1.0, 1.0, nil}),
		db("DB_INSTANCE_POSTGRES-0000000000000A83", "inventory-pg-flex", "2026-04-28T13:07:00.000000000Z", nil),
	}

	a := testApp(t, "databases")
	seedRows(t, a, rows)
	assertSnapshots(t, "databases", a)
}
