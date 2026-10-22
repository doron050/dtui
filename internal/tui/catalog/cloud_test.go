package catalog

import (
	"strings"
	"testing"
)

func TestResourcesQueryUsesArg(t *testing.T) {
	spec := Lookup("resources")
	q := spec.Query(Scope{Timeframe: Timeframe{Label: "2h"}, Arg: "AWS_EC2_INSTANCE"})
	if !strings.Contains(q, `smartscapeNodes "AWS_EC2_INSTANCE"`) {
		t.Errorf("resources query ignores Arg:\n%s", q)
	}
	if !strings.Contains(q, "`tags:aws`[`Name`]") {
		t.Errorf("resources query missing Name-tag display fallback:\n%s", q)
	}
	if got := spec.Query(Scope{Timeframe: Timeframe{Label: "2h"}}); !strings.Contains(got, `smartscapeNodes "*"`) {
		t.Errorf("resources query without Arg should browse all types:\n%s", got)
	}
}
func TestPrettyType(t *testing.T) {
	if got := prettyType("AWS_EC2_INSTANCE"); got != "ec2 instance" {
		t.Errorf("prettyType = %q", got)
	}
	if got := prettyType("K8S_POD"); got != "pod" {
		t.Errorf("prettyType = %q", got)
	}
	// Azure: the redundant AZURE_MICROSOFT_ provider prefix is dropped.
	if got := prettyType("AZURE_MICROSOFT_COMPUTE_DISKS"); got != "compute disks" {
		t.Errorf("prettyType azure = %q", got)
	}
	if got := prettyType("AZURE_SOMETHING"); got != "something" {
		t.Errorf("prettyType azure no-microsoft = %q", got)
	}
}

func TestAzureCensusSpec(t *testing.T) {
	spec := Lookup("azure")
	if spec == nil {
		t.Fatal(`Lookup("azure") is nil — azureSpec not registered`)
	}
	q := spec.Query(Scope{})
	if !strings.Contains(q, `startsWith(type, "AZURE_")`) {
		t.Errorf("azure census not filtered to AZURE_:\n%s", q)
	}
	if !strings.Contains(q, "summarize count = count(), by:{type}") {
		t.Errorf("azure census not a type census:\n%s", q)
	}
	if spec.EnterTarget != "resources" {
		t.Errorf("azure census EnterTarget = %q, want resources", spec.EnterTarget)
	}
}

func TestResourcesScopeColumnsProviderAware(t *testing.T) {
	spec := Lookup("resources")
	// AZURE_ browse → Azure column set: REGION=azure.location, SUBSCRIPTION=name.
	az := spec.ScopeColumns(Scope{Arg: "AZURE_MICROSOFT_COMPUTE_DISKS"})
	if az == nil {
		t.Fatal("ScopeColumns nil for AZURE_ type")
	}
	titles := map[string]string{}
	for _, c := range az {
		titles[c.Title] = c.Field
	}
	if titles["REGION"] != "azure.location" {
		t.Errorf("azure REGION field = %q, want azure.location", titles["REGION"])
	}
	if titles["SUBSCRIPTION"] != SubscriptionNameKey {
		t.Errorf("azure SUBSCRIPTION field = %q, want %q", titles["SUBSCRIPTION"], SubscriptionNameKey)
	}
	if _, ok := titles["ACCOUNT"]; ok {
		t.Error("azure columns should not carry an ACCOUNT column")
	}
	// AWS (and everything else) → nil, falling through to the static AWS columns.
	if cols := spec.ScopeColumns(Scope{Arg: "AWS_EC2_INSTANCE"}); cols != nil {
		t.Error("ScopeColumns should be nil for AWS (uses static Columns)")
	}
	// Subscription-less Azure metadata types (regions, AZs, tenant) drop the
	// SUBSCRIPTION column — it would be blank by construction.
	for _, typ := range []string{
		"AZURE_MICROSOFT_RESOURCES_LOCATIONS",
		"AZURE_MICROSOFT_RESOURCES_LOCATIONS_AVAILABILITYZONES",
		"AZURE_MICROSOFT_RESOURCES_TENANTS",
	} {
		for _, c := range spec.ScopeColumns(Scope{Arg: typ}) {
			if c.Title == "SUBSCRIPTION" {
				t.Errorf("%s should not carry a SUBSCRIPTION column", typ)
			}
		}
	}
	// The subscriptions node itself DOES carry a subscription — keep the column.
	var subsNodeHasCol bool
	for _, c := range spec.ScopeColumns(Scope{Arg: "AZURE_MICROSOFT_RESOURCES_SUBSCRIPTIONS"}) {
		if c.Title == "SUBSCRIPTION" {
			subsNodeHasCol = true
		}
	}
	if !subsNodeHasCol {
		t.Error("AZURE_MICROSOFT_RESOURCES_SUBSCRIPTIONS should keep the SUBSCRIPTION column")
	}
	// The static default keeps the AWS account column.
	var hasAccount bool
	for _, c := range spec.Columns {
		if c.Title == "ACCOUNT" && c.Field == "aws.account.id" {
			hasAccount = true
		}
	}
	if !hasAccount {
		t.Error("static Columns lost the AWS ACCOUNT column")
	}
}

func TestResourcesDisplayCoalesceMultiCloud(t *testing.T) {
	q := Lookup("resources").Query(Scope{Arg: "AZURE_MICROSOFT_COMPUTE_DISKS"})
	for _, want := range []string{"`tags:aws`[`Name`]", "azure.resource.name", "aws.arn", "azure.resource.id"} {
		if !strings.Contains(q, want) {
			t.Errorf("display coalesce missing %q:\n%s", want, q)
		}
	}
}

func TestAzureSubscriptionsQueryAndParse(t *testing.T) {
	q := AzureSubscriptionsQuery()
	if !strings.Contains(q, `smartscapeNodes "AZURE_MICROSOFT_RESOURCES_SUBSCRIPTIONS"`) {
		t.Errorf("subscriptions query wrong source:\n%s", q)
	}
	if strings.Contains(q, "dt.entity") {
		t.Errorf("subscriptions query must use smartscapeNodes only, not dt.entity:\n%s", q)
	}
	got := ParseAzureSubscriptions([]map[string]any{
		{"azure.subscription": "guid-1", "name": "Prod"},
		{"azure.subscription": "guid-2", "name": "Dev"},
		{"azure.subscription": "guid-3"},            // missing name → skipped
		{"name": "orphan"},                          // missing guid → skipped
		{"azure.subscription": "", "name": "blank"}, // empty guid → skipped
	})
	if len(got) != 2 || got["guid-1"] != "Prod" || got["guid-2"] != "Dev" {
		t.Errorf("ParseAzureSubscriptions = %#v", got)
	}
}
