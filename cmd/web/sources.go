//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dynatrace-oss/dtctl/pkg/resources/analyzer"
	"github.com/dynatrace-oss/dtctl/pkg/resources/anomalydetector"
	"github.com/dynatrace-oss/dtctl/pkg/resources/document"
	"github.com/dynatrace-oss/dtctl/pkg/resources/iam"
	"github.com/dynatrace-oss/dtctl/pkg/resources/segment"
	"github.com/dynatrace-oss/dtctl/pkg/resources/slo"
	"github.com/dynatrace-oss/dtctl/sdk/session"
	"github.com/dynatrace-oss/dtui/internal/tui"
	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
)

// tuiSources wires the TUI's API-backed views (catalog.Spec.API) to dtctl's
// resource handlers. All sources are read-only; construction here keeps
// internal/tui free of HTTP, mirroring how main.go passes the DQL executor.
//
// NOTE: this is copied verbatim from the repo-root sources.go. A follow-up
// should extract the shared source constructors into an internal package so the
// native main and cmd/web share one implementation instead of this duplicate.
func tuiSources(c *session.Client) map[string]tui.Source {
	return map[string]tui.Source{
		"slos":              sloSource(slo.NewHandler(c)),
		"anomaly-detectors": detectorSource(anomalydetector.NewHandler(c)),
		"log-patterns":      logPatternSource(analyzer.NewHandler(c)),
		"dashboards":        dashboardSource(document.NewHandler(c), c.CurrentUserID),
		"resolve-users":     userSource(iam.NewHandler(c)),
	}
}

type sloAPI interface {
	List(filter string, chunkSize int64) (*slo.SLOList, error)
	Evaluate(id string) (*slo.EvaluationResponse, error)
	PollEvaluation(token string, timeoutMs int) (*slo.EvaluationResponse, error)
}

type detectorAPI interface {
	List(opts anomalydetector.ListOptions) ([]anomalydetector.AnomalyDetector, error)
}

type analyzerAPI interface {
	ExecuteAndWait(ctx context.Context, name string, input map[string]interface{}, maxWaitSeconds int) (*analyzer.ExecuteResult, error)
}

type segmentAPI interface {
	List() (*segment.FilterSegmentList, error)
}

type dashboardAPI interface {
	List(filters document.DocumentFilters) (*document.DocumentList, error)
}

func dashboardSource(h dashboardAPI, whoami func() (string, error)) tui.Source {
	var (
		mu     sync.Mutex
		userID string
	)
	resolveMe := func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if userID != "" {
			return userID, nil
		}
		id, err := whoami()
		if err != nil {
			return "", err
		}
		userID = id
		return id, nil
	}
	return func(_ context.Context, scope catalog.Scope, _ string) ([]map[string]any, error) {
		filters := document.DocumentFilters{
			Type:      "dashboard",
			AddFields: []string{"labels", "shareInfo.isShared"},
			ChunkSize: 500,
		}
		if scope.Lens == catalog.DashLensMine {
			id, err := resolveMe()
			if err != nil {
				return nil, fmt.Errorf("mine lens: %w", err)
			}
			filters.Owner = id
		}
		list, err := h.List(filters)
		if err != nil {
			return nil, err
		}
		docs := list.Documents
		sort.SliceStable(docs, func(i, j int) bool {
			return docs[i].ModificationInfo.LastModifiedTime.After(docs[j].ModificationInfo.LastModifiedTime)
		})
		records := make([]map[string]any, 0, len(docs))
		for _, m := range docs {
			rec := map[string]any{
				"id":          m.ID,
				"name":        m.Name,
				"type":        m.Type,
				"owner":       m.Owner,
				"description": m.Description,
				"private":     strconv.FormatBool(m.IsPrivate),
				"tags":        anySlice(m.Labels),
				"access":      anySlice(m.Access),
			}
			if m.Version > 0 {
				rec["version"] = strconv.Itoa(m.Version)
			}
			if m.ShareInfo != nil {
				rec["shared"] = strconv.FormatBool(m.ShareInfo.IsShared)
			}
			if !m.ModificationInfo.CreatedTime.IsZero() {
				rec["created"] = m.ModificationInfo.CreatedTime.Format(time.RFC3339)
			}
			if !m.ModificationInfo.LastModifiedTime.IsZero() {
				rec["modified"] = m.ModificationInfo.LastModifiedTime.Format(time.RFC3339)
			}
			records = append(records, rec)
		}
		return records, nil
	}
}

type userAPI interface {
	ListUsers(partialString string, uuids []string, chunkSize int64) (*iam.UserListResponse, error)
}

func userSource(h userAPI) tui.Source {
	return func(_ context.Context, _ catalog.Scope, arg string) ([]map[string]any, error) {
		uuids := splitNonEmpty(arg)
		if len(uuids) == 0 {
			return nil, nil
		}
		resp, err := h.ListUsers("", uuids, int64(len(uuids)))
		if err != nil {
			return nil, err
		}
		records := make([]map[string]any, 0, len(resp.Results))
		for _, u := range resp.Results {
			records = append(records, map[string]any{"id": u.UID, "name": userDisplayName(u)})
		}
		return records, nil
	}
}

func userDisplayName(u iam.User) string {
	if name := strings.TrimSpace(u.Name + " " + u.Surname); name != "" {
		if u.Email != "" {
			return name + " (" + u.Email + ")"
		}
		return name
	}
	if u.Email != "" {
		return u.Email
	}
	return u.UID
}

func splitNonEmpty(csv string) []string {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func segmentSource(h segmentAPI) tui.SegmentLister {
	return func(_ context.Context) ([]tui.SegmentOption, error) {
		list, err := h.List()
		if err != nil {
			return nil, err
		}
		opts := make([]tui.SegmentOption, 0, len(list.FilterSegments))
		for _, s := range list.FilterSegments {
			opt := tui.SegmentOption{
				UID:          s.UID,
				Name:         s.Name,
				Description:  s.Description,
				HasVariables: s.Variables != nil,
			}
			if s.Variables != nil && s.Variables.Type == "query" {
				opt.VariablesQuery = s.Variables.Value
			}
			opts = append(opts, opt)
		}
		sort.SliceStable(opts, func(i, j int) bool {
			return strings.ToLower(opts[i].Name) < strings.ToLower(opts[j].Name)
		})
		return opts, nil
	}
}

const sloEvalConcurrency = 6

const sloEvalTimeout = 12 * time.Second

func sloSource(h sloAPI) tui.Source {
	return func(ctx context.Context, scope catalog.Scope, dql string) ([]map[string]any, error) {
		list, err := h.List("", 400)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 2*sloEvalTimeout)
		defer cancel()
		records := make([]map[string]any, len(list.SLOs))
		var wg sync.WaitGroup
		sem := make(chan struct{}, sloEvalConcurrency)
		for i := range list.SLOs {
			s := list.SLOs[i]
			rec := map[string]any{
				"id":          s.ID,
				"name":        s.Name,
				"description": s.Description,
				"tags":        anySlice(s.Tags),
			}
			if len(s.Criteria) > 0 {
				rec["target"] = s.Criteria[0].Target
				if s.Criteria[0].Warning != nil {
					rec["warning"] = *s.Criteria[0].Warning
				}
				rec["window"] = strings.TrimPrefix(s.Criteria[0].TimeframeFrom, "now-")
			}
			if ts := slo.DecodeVersionTimestamp(s.Version); ts != nil {
				rec["modified"] = ts.Format(time.RFC3339)
			}
			if indicator, ok := s.CustomSli["indicator"].(string); ok {
				rec["indicator"] = indicator
			}
			records[i] = rec
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				mergeSLOEvaluation(ctx, h, rec, s.ID)
			}()
		}
		wg.Wait()
		return records, nil
	}
}

func mergeSLOEvaluation(ctx context.Context, h sloAPI, rec map[string]any, id string) {
	resp, err := h.Evaluate(id)
	if err != nil {
		return
	}
	token := resp.EvaluationToken
	deadline := time.Now().Add(sloEvalTimeout)
	for len(resp.EvaluationResults) == 0 {
		remaining := time.Until(deadline)
		if token == "" || remaining <= 0 || ctx.Err() != nil {
			return
		}
		pollMs := int(remaining / time.Millisecond)
		if pollMs > 5000 {
			pollMs = 5000
		}
		next, err := h.PollEvaluation(token, pollMs)
		if err != nil {
			return
		}
		if next.EvaluationToken != "" {
			token = next.EvaluationToken
		}
		resp = next
	}
	res := resp.EvaluationResults[0]
	rec["status"] = res.Status
	if res.Value != nil {
		rec["sli"] = *res.Value
	}
	if res.ErrorBudget != nil {
		rec["errorBudget"] = *res.ErrorBudget
	}
	if res.Message != "" {
		rec["message"] = res.Message
	}
}

func detectorSource(h detectorAPI) tui.Source {
	return func(ctx context.Context, scope catalog.Scope, dql string) ([]map[string]any, error) {
		detectors, err := h.List(anomalydetector.ListOptions{})
		if err != nil {
			return nil, err
		}
		records := make([]map[string]any, 0, len(detectors))
		for _, d := range detectors {
			records = append(records, map[string]any{
				"title":       d.Title,
				"enabled":     strconv.FormatBool(d.Enabled),
				"analyzer":    d.AnalyzerShort,
				"eventType":   d.EventType,
				"source":      d.Source,
				"description": d.Description,
				"objectId":    d.ObjectID,
				"value":       d.Value,
			})
		}
		return records, nil
	}
}

const logPatternTimeout = 120

func logPatternSource(h analyzerAPI) tui.Source {
	return func(ctx context.Context, scope catalog.Scope, dql string) ([]map[string]any, error) {
		if strings.TrimSpace(dql) == "" {
			return nil, fmt.Errorf("log-pattern extraction needs a logs query")
		}
		input := map[string]any{
			"logQuery":         catalog.LogPatternInput(dql),
			"numberOfExamples": catalog.LogPatternExamples,
			"generalParameters": map[string]any{
				"timeframe": map[string]any{
					"startTime": "now-" + scope.Timeframe.Label,
					"endTime":   "now",
				},
			},
		}
		result, err := h.ExecuteAndWait(ctx, catalog.LogPatternAnalyzer, input, logPatternTimeout)
		if err != nil {
			return nil, err
		}
		if result.Result == nil {
			return nil, fmt.Errorf("analyzer returned no result")
		}
		if result.Result.ExecutionStatus != "COMPLETED" {
			return nil, fmt.Errorf("analyzer execution %s", strings.ToLower(result.Result.ExecutionStatus))
		}
		records := make([]map[string]any, 0, len(result.Result.Output))
		for _, item := range result.Result.Output {
			records = append(records, map[string]any(item))
		}
		sort.SliceStable(records, func(i, j int) bool {
			return patternMatches(records[i]) > patternMatches(records[j])
		})
		return records, nil
	}
}

func patternMatches(rec map[string]any) float64 {
	f, _ := catalog.FloatValue(rec["numberOfMatches"])
	return f
}

func anySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
