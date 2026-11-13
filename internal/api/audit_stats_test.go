package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/internal/store"
)

func TestParseAuditStatsFilter(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs/stats", nil)
	filter, err := parseAuditStatsFilter(req, now)
	if err != nil {
		t.Fatalf("parseAuditStatsFilter: %v", err)
	}
	if !filter.To.Equal(now) || filter.To.Sub(filter.From) != defaultAuditStatsWindow {
		t.Fatalf("filter = %+v, want default 7-day window", filter)
	}

	cases := []string{
		"?from=2026-11-13T12:00:00Z&to=2026-11-13T12:00:00Z",
		"?from=2026-08-01T00:00:00Z&to=2026-11-13T00:00:00Z",
		"?from=not-a-time",
	}
	for _, query := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs/stats"+query, nil)
		if _, err := parseAuditStatsFilter(req, now); err == nil {
			t.Fatalf("%s: expected error", query)
		}
	}
}

func TestAuditStatsEndpoint(t *testing.T) {
	stats := store.AuditStats{
		Total: 10, Success: 9, Failed: 1,
		ByVerb:     []store.AuditCount{{Key: "patch", Count: 6}},
		ByResource: []store.AuditCount{{Key: "deployments", Count: 5}},
		ByActor:    []store.AuditCount{{Key: "alice", Count: 10}},
		ByDay:      []store.AuditDayCount{{Day: "2026-11-13", Total: 10, Failed: 1}},
	}
	router := auditRouter(t, nil, fakeAuditStore{stats: stats})
	rec := doRequest(t, router, http.MethodGet, "/api/v1/admin/audit-logs/stats")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body store.AuditStats
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 10 || body.ByVerb[0].Key != "patch" || body.ByDay[0].Day != "2026-11-13" {
		t.Fatalf("stats = %+v", body)
	}
}

func TestAuditStatsEndpointAdminOnly(t *testing.T) {
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: &fakeTransport{name: "prod"}}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry:   registry,
		Auth:       fakeAuth{authenticated: true, identity: auth.Identity{UserID: 9, Subject: "viewer"}},
		AuditStore: fakeAuditStore{},
	})
	rec := doRequest(t, router, http.MethodGet, "/api/v1/admin/audit-logs/stats")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
