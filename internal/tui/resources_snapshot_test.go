package tui

import "testing"

// Resources mirror the generic browser's smartscapeNodes "*" projection over
// AWS: name-poor nodes (empty name and display fall back to the id), the
// coalesced display for named ones, aws.region/account, `tags:aws`, lifetime.
func TestSnapshotResourcesTable(t *testing.T) {
	pinSnapshotClock(t)

	res := func(id, typ, name, region, account, start string) map[string]any {
		return map[string]any{
			"id": id, "type": typ, "name": name, "display": name,
			"aws.region": region, "aws.account.id": account, "cloud.provider": "aws",
			"cloud.acquisition.status": "OK", "dt.security_context": []any{},
			"tags:aws": map[string]any{"Name": name, "Environment": "dev"},
			"lifetime": map[string]any{"start": start, "end": "2026-07-15T11:48:00.000000000Z"},
		}
	}
	rows := []map[string]any{
		res("AWS_EC2_VOLUME-0000000000000A71", "AWS_EC2_VOLUME", "", "us-east-1", "000000000001", "2026-05-26T12:14:00.000000000Z"),
		res("AWS_APIGATEWAYV2_STAGE-0000000000000A72", "AWS_APIGATEWAYV2_STAGE", "$default", "us-east-1", "000000000002", "2026-05-26T08:39:00.000000000Z"),
		res("AWS_APIGATEWAYV2_STAGE-0000000000000A73", "AWS_APIGATEWAYV2_STAGE", "$default", "us-east-1", "000000000002", "2026-06-16T11:44:00.000000000Z"),
		res("AWS_LAMBDA_FUNCTION-0000000000000A74", "AWS_LAMBDA_FUNCTION", "orders-export-handler", "eu-central-1", "000000000002", "2026-06-05T15:42:00.000000000Z"),
		res("AWS_RDS_DBINSTANCE-0000000000000A75", "AWS_RDS_DBINSTANCE", "orders-db-replica-2", "eu-central-1", "000000000003", "2026-06-05T06:18:00.000000000Z"),
	}

	a := testApp(t, "resources")
	seedRows(t, a, rows)
	assertSnapshots(t, "resources", a)
}
