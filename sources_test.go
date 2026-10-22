package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dynatrace-oss/dtctl/pkg/resources/analyzer"
	"github.com/dynatrace-oss/dtctl/pkg/resources/anomalydetector"
	"github.com/dynatrace-oss/dtctl/pkg/resources/document"
	"github.com/dynatrace-oss/dtctl/pkg/resources/iam"
	"github.com/dynatrace-oss/dtctl/pkg/resources/segment"
	"github.com/dynatrace-oss/dtctl/pkg/resources/slo"
	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// --- segment source ---------------------------------------------------------

type fakeSegmentAPI struct {
	list *segment.FilterSegmentList
	err  error
}

func (f *fakeSegmentAPI) List() (*segment.FilterSegmentList, error) { return f.list, f.err }

func TestSegmentSourceMapsAndSorts(t *testing.T) {
	src := segmentSource(&fakeSegmentAPI{list: &segment.FilterSegmentList{
		FilterSegments: []segment.FilterSegment{
			{UID: "u2", Name: "zeta"},
			{UID: "u1", Name: "Alpha", Description: "team A",
				Variables: &segment.Variables{Type: "query", Value: "fetch x"}},
			{UID: "u3", Name: "beta",
				Variables: &segment.Variables{Type: "other", Value: "ignored"}},
		},
	}})
	opts, err := src(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 3 || opts[0].Name != "Alpha" || opts[1].Name != "beta" || opts[2].Name != "zeta" {
		t.Fatalf("sort order wrong: %+v", opts)
	}
	if !opts[0].HasVariables || opts[0].VariablesQuery != "fetch x" || opts[0].Description != "team A" {
		t.Fatalf("query variables not mapped: %+v", opts[0])
	}
	if !opts[1].HasVariables || opts[1].VariablesQuery != "" {
		t.Fatalf("non-query variables must badge but carry no query: %+v", opts[1])
	}

	if _, err := segmentSource(&fakeSegmentAPI{err: errors.New("HTTP 500")})(context.Background()); err == nil {
		t.Fatal("list error must surface")
	}
}

// --- SLO source --------------------------------------------------------------

type fakeSLOAPI struct {
	list    *slo.SLOList
	listErr error
	evals   map[string]*slo.EvaluationResponse
	evalErr map[string]error
	polls   map[string][]*slo.EvaluationResponse // token → response sequence
}

func (f *fakeSLOAPI) List(string, int64) (*slo.SLOList, error) { return f.list, f.listErr }

func (f *fakeSLOAPI) Evaluate(id string) (*slo.EvaluationResponse, error) {
	if err := f.evalErr[id]; err != nil {
		return nil, err
	}
	return f.evals[id], nil
}

func (f *fakeSLOAPI) PollEvaluation(token string, _ int) (*slo.EvaluationResponse, error) {
	seq := f.polls[token]
	if len(seq) == 0 {
		return nil, errors.New("unexpected poll for " + token)
	}
	next := seq[0]
	f.polls[token] = seq[1:]
	return next, nil
}

func floatPtr(f float64) *float64 { return &f }

func TestSLOSourceMergesEvaluationsAndDegrades(t *testing.T) {
	api := &fakeSLOAPI{
		list: &slo.SLOList{SLOs: []slo.SLO{
			{ID: "slo-1", Name: "checkout availability", Tags: []string{"team:web"},
				Criteria:  []slo.Criteria{{Target: 99.5, Warning: floatPtr(99.9), TimeframeFrom: "now-7d"}},
				CustomSli: map[string]any{"indicator": "timeseries sli"}},
			{ID: "slo-2", Name: "broken"},
		}},
		evals: map[string]*slo.EvaluationResponse{
			"slo-1": {EvaluationResults: []slo.EvaluationResult{
				{Status: "SUCCESS", Value: floatPtr(99.7), ErrorBudget: floatPtr(0.2)}}},
		},
		evalErr: map[string]error{"slo-2": errors.New("evaluation exploded")},
	}
	records, err := sloSource(api)(context.Background(), catalog.Scope{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	ok := records[0]
	if ok["name"] != "checkout availability" || ok["target"] != 99.5 || ok["warning"] != 99.9 ||
		ok["window"] != "7d" || ok["indicator"] != "timeseries sli" {
		t.Fatalf("definition not flattened: %+v", ok)
	}
	if ok["status"] != "SUCCESS" || ok["sli"] != 99.7 || ok["errorBudget"] != 0.2 {
		t.Fatalf("evaluation not merged: %+v", ok)
	}
	// The failed evaluation degrades to blank cells, never an error.
	if _, has := records[1]["status"]; has {
		t.Fatalf("failed evaluation must leave the record blank: %+v", records[1])
	}
	if records[1]["name"] != "broken" {
		t.Fatalf("definition row lost: %+v", records[1])
	}
}

func TestSLOEvaluationPollsThroughTokenRotation(t *testing.T) {
	api := &fakeSLOAPI{
		evals: map[string]*slo.EvaluationResponse{
			"slo-1": {EvaluationToken: "t1"}, // long-poll: no results yet
		},
		polls: map[string][]*slo.EvaluationResponse{
			"t1": {{EvaluationToken: "t2"}}, // rotated token, still no results
			"t2": {{EvaluationResults: []slo.EvaluationResult{{Status: "FAILURE"}}}},
		},
	}
	rec := map[string]any{}
	mergeSLOEvaluation(context.Background(), api, rec, "slo-1")
	if rec["status"] != "FAILURE" {
		t.Fatalf("poll chain did not deliver the result: %+v", rec)
	}
}

// --- detector source ---------------------------------------------------------

type fakeDetectorAPI struct {
	detectors []anomalydetector.AnomalyDetector
	err       error
}

func (f *fakeDetectorAPI) List(anomalydetector.ListOptions) ([]anomalydetector.AnomalyDetector, error) {
	return f.detectors, f.err
}

func TestDetectorSourceFlattens(t *testing.T) {
	src := detectorSource(&fakeDetectorAPI{detectors: []anomalydetector.AnomalyDetector{{
		ObjectID: "obj-1", Title: "CPU saturation", Enabled: true,
		AnalyzerShort: "StaticThreshold", EventType: "RESOURCE",
		Value: map[string]any{"raw": true},
	}}})
	records, err := src(context.Background(), catalog.Scope{}, "")
	if err != nil {
		t.Fatal(err)
	}
	rec := records[0]
	if rec["title"] != "CPU saturation" || rec["enabled"] != "true" ||
		rec["analyzer"] != "StaticThreshold" || rec["objectId"] != "obj-1" {
		t.Fatalf("detector not flattened: %+v", rec)
	}
	if _, ok := rec["value"].(map[string]any); !ok {
		t.Fatalf("raw settings value must ride along for the inspector: %+v", rec)
	}
}

// --- dashboard source --------------------------------------------------------

type fakeDashboardAPI struct {
	gotFilter document.DocumentFilters
	list      *document.DocumentList
	err       error
}

func (f *fakeDashboardAPI) List(filters document.DocumentFilters) (*document.DocumentList, error) {
	f.gotFilter = filters
	return f.list, f.err
}

func TestDashboardSourceFlattensAndSorts(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	api := &fakeDashboardAPI{list: &document.DocumentList{
		Documents: []document.DocumentMetadata{
			{ID: "d-old", Name: "Old board", Type: "dashboard", Owner: "user-1",
				ModificationInfo: document.ModificationInfo{LastModifiedTime: older}},
			{ID: "d-new", Name: "New board", Type: "dashboard", Owner: "user-2", IsPrivate: true,
				Description: "fleet health", Version: 7,
				Labels: []string{"prod", "team-a"}, Access: []string{"read", "write"},
				ShareInfo:        &document.ShareInfo{IsShared: true},
				ModificationInfo: document.ModificationInfo{CreatedTime: older, LastModifiedTime: newer}},
		},
	}}
	records, err := dashboardSource(api, whoamiFails)(context.Background(), catalog.Scope{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if api.gotFilter.Type != "dashboard" {
		t.Fatalf("source must filter to dashboards, got %q", api.gotFilter.Type)
	}
	// Tags (UI labels) are omitted by the API unless requested.
	if !slices.Contains(api.gotFilter.AddFields, "labels") {
		t.Fatalf("source must request the labels add-field, got %+v", api.gotFilter.AddFields)
	}
	// ChunkSize must be set, or List returns the first page only (~20 docs).
	if api.gotFilter.ChunkSize == 0 {
		t.Fatalf("source must set ChunkSize to paginate, got 0 (first page only)")
	}
	// The default (all) lens sends no owner filter and never resolves the user.
	if api.gotFilter.Owner != "" {
		t.Fatalf("all lens must not set an owner filter, got %q", api.gotFilter.Owner)
	}
	// Most-recently-modified first.
	if records[0]["id"] != "d-new" || records[1]["id"] != "d-old" {
		t.Fatalf("dashboards not sorted newest-first: %+v", records)
	}
	rec := records[0]
	if rec["name"] != "New board" || rec["type"] != "dashboard" ||
		rec["owner"] != "user-2" || rec["private"] != "true" ||
		rec["shared"] != "true" || rec["version"] != "7" ||
		rec["description"] != "fleet health" {
		t.Fatalf("dashboard not flattened: %+v", rec)
	}
	if tags, _ := rec["tags"].([]any); len(tags) != 2 || tags[0] != "prod" {
		t.Fatalf("dashboard tags (labels) not surfaced: %+v", rec["tags"])
	}
	if acc, _ := rec["access"].([]any); len(acc) != 2 || acc[0] != "read" {
		t.Fatalf("dashboard access levels not surfaced: %+v", rec["access"])
	}
	if !strings.HasPrefix(catalog.Str(rec, "modified"), "2026-06-01") {
		t.Fatalf("modified not RFC3339: %+v", rec["modified"])
	}
	if !strings.HasPrefix(catalog.Str(rec, "created"), "2026-01-01") {
		t.Fatalf("created not RFC3339: %+v", rec["created"])
	}
}

// whoamiFails stands in for CurrentUserID on the "all" lens, where it must
// never be called — invoking it fails the test.
func whoamiFails() (string, error) {
	return "", errors.New("whoami must not be called on the all lens")
}

// TestDashboardSourceMineLensFiltersByOwner: the "mine" lens resolves the
// current user and sends the document API an owner==me filter; the "all" lens
// (default) sends none. The user id is resolved once and memoized across calls.
func TestDashboardSourceMineLensFiltersByOwner(t *testing.T) {
	api := &fakeDashboardAPI{list: &document.DocumentList{}}
	calls := 0
	whoami := func() (string, error) { calls++; return "me-uuid", nil }
	src := dashboardSource(api, whoami)

	// mine lens → owner filter set, user resolved.
	if _, err := src(context.Background(), catalog.Scope{Lens: catalog.DashLensMine}, ""); err != nil {
		t.Fatal(err)
	}
	if api.gotFilter.Owner != "me-uuid" {
		t.Fatalf("mine lens must filter by owner==me, got %q", api.gotFilter.Owner)
	}
	// all lens → no owner filter, no re-resolution needed.
	if _, err := src(context.Background(), catalog.Scope{Lens: catalog.DashLensAll}, ""); err != nil {
		t.Fatal(err)
	}
	if api.gotFilter.Owner != "" {
		t.Fatalf("all lens must clear the owner filter, got %q", api.gotFilter.Owner)
	}
	// A second mine call reuses the memoized id (whoami called exactly once).
	if _, err := src(context.Background(), catalog.Scope{Lens: catalog.DashLensMine}, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("current user must be resolved once and memoized, got %d calls", calls)
	}
}

// TestDashboardSourceMineLensSurfacesWhoamiError: when the current user can't
// be resolved (e.g. a token lacking the metadata scope), the mine lens errors
// rather than silently listing every dashboard — and the error is not cached,
// so a later attempt retries.
func TestDashboardSourceMineLensSurfacesWhoamiError(t *testing.T) {
	api := &fakeDashboardAPI{list: &document.DocumentList{}}
	calls := 0
	whoami := func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("HTTP 403")
		}
		return "me-uuid", nil
	}
	src := dashboardSource(api, whoami)

	if _, err := src(context.Background(), catalog.Scope{Lens: catalog.DashLensMine}, ""); err == nil {
		t.Fatal("mine lens must surface the whoami failure, not list everything")
	}
	if api.gotFilter.Owner != "" {
		t.Fatalf("a failed resolution must not leak an owner filter, got %q", api.gotFilter.Owner)
	}
	// Not cached: the retry resolves and filters.
	if _, err := src(context.Background(), catalog.Scope{Lens: catalog.DashLensMine}, ""); err != nil {
		t.Fatal(err)
	}
	if api.gotFilter.Owner != "me-uuid" {
		t.Fatalf("retry after error must resolve the owner, got %q", api.gotFilter.Owner)
	}
}

// --- user (IAM) source -------------------------------------------------------

type fakeUserAPI struct {
	gotUUIDs []string
	resp     *iam.UserListResponse
	err      error
}

func (f *fakeUserAPI) ListUsers(_ string, uuids []string, _ int64) (*iam.UserListResponse, error) {
	f.gotUUIDs = uuids
	return f.resp, f.err
}

func TestUserSourceResolvesNames(t *testing.T) {
	api := &fakeUserAPI{resp: &iam.UserListResponse{Results: []iam.User{
		{UID: "u-1", Name: "Ada", Surname: "Lovelace", Email: "ada@x.io"},
		{UID: "u-2", Email: "svc@x.io"}, // no name → email
		{UID: "u-3"},                    // nothing → uid
	}}}
	records, err := userSource(api)(context.Background(), catalog.Scope{}, "u-1, u-2 ,u-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(api.gotUUIDs) != 3 || api.gotUUIDs[0] != "u-1" || api.gotUUIDs[2] != "u-3" {
		t.Fatalf("uuids not parsed from the comma-joined arg: %+v", api.gotUUIDs)
	}
	want := map[string]string{"u-1": "Ada Lovelace (ada@x.io)", "u-2": "svc@x.io", "u-3": "u-3"}
	for _, rec := range records {
		id := catalog.Str(rec, "id")
		if got := catalog.Str(rec, "name"); got != want[id] {
			t.Errorf("%s name = %q, want %q", id, got, want[id])
		}
	}
	// Empty arg resolves nothing without calling the API.
	if recs, _ := userSource(api)(context.Background(), catalog.Scope{}, "  "); recs != nil {
		t.Errorf("blank arg should yield no records, got %+v", recs)
	}
}

// --- log-pattern source --------------------------------------------------------

type fakeAnalyzerAPI struct {
	gotName  string
	gotInput map[string]any
	result   *analyzer.ExecuteResult
	err      error
}

func (f *fakeAnalyzerAPI) ExecuteAndWait(_ context.Context, name string, input map[string]interface{}, _ int) (*analyzer.ExecuteResult, error) {
	f.gotName, f.gotInput = name, input
	return f.result, f.err
}

func TestLogPatternSourceComposesAndSorts(t *testing.T) {
	api := &fakeAnalyzerAPI{result: &analyzer.ExecuteResult{Result: &analyzer.AnalyzerResult{
		ExecutionStatus: "COMPLETED",
		Output: []map[string]any{
			{"pattern": "quiet", "numberOfMatches": float64(3)},
			{"pattern": "busy", "numberOfMatches": "1200"}, // Grail stringifies longs
			{"pattern": "middling", "numberOfMatches": float64(40)},
		},
	}}}
	scope := catalog.Scope{Timeframe: catalog.Timeframe{Label: "2h"}}
	records, err := logPatternSource(api)(context.Background(), scope, "fetch logs\n| sort timestamp desc")
	if err != nil {
		t.Fatal(err)
	}
	if api.gotName != catalog.LogPatternAnalyzer {
		t.Fatalf("analyzer = %q", api.gotName)
	}
	if q, _ := api.gotInput["logQuery"].(string); !strings.HasPrefix(q, "fetch logs") {
		t.Fatalf("composed logQuery = %q", q)
	}
	if got := []string{records[0]["pattern"].(string), records[1]["pattern"].(string), records[2]["pattern"].(string)}; got[0] != "busy" || got[1] != "middling" || got[2] != "quiet" {
		t.Fatalf("patterns not sorted by matches (string counts must not sort as 0): %v", got)
	}

	if _, err := logPatternSource(api)(context.Background(), scope, "   "); err == nil {
		t.Fatal("empty dql must refuse")
	}
	api.result = &analyzer.ExecuteResult{Result: &analyzer.AnalyzerResult{ExecutionStatus: "FAILED"}}
	if _, err := logPatternSource(api)(context.Background(), scope, "fetch logs"); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("non-completed execution must error, got %v", err)
	}
}
