package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/auth"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/internal/store"
)

type fakeAuditWriter struct {
	mu      sync.Mutex
	entries []domain.AuditEntry
}

func (f *fakeAuditWriter) Record(entry domain.AuditEntry) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, entry)
	return true
}

func (f *fakeAuditWriter) last() (domain.AuditEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.entries) == 0 {
		return domain.AuditEntry{}, false
	}
	return f.entries[len(f.entries)-1], true
}

type fakeAuditStore struct {
	items     []domain.AuditEntry
	status    store.ChainStatus
	retention int
}

func (f fakeAuditStore) QueryAudit(context.Context, store.AuditFilter) ([]domain.AuditEntry, error) {
	return f.items, nil
}
func (f fakeAuditStore) CountAudit(context.Context, store.AuditFilter) (int, error) {
	return len(f.items), nil
}
func (f fakeAuditStore) VerifyAuditChain(context.Context) (store.ChainStatus, error) {
	return f.status, nil
}
func (f fakeAuditStore) RetentionDays(context.Context) (int, error) {
	return f.retention, nil
}

func auditRouter(t *testing.T, writer *fakeAuditWriter, storeImpl AuditStore) http.Handler {
	t.Helper()
	transport := &fakeTransport{name: "prod", object: deploymentObject(), list: &unstructured.UnstructuredList{}}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	return NewRouter(Options{
		Registry:   registry,
		Auth:       fakeAuth{authenticated: true, identity: auth.Identity{UserID: 3, Subject: "alice", Email: "alice@example.com", PlatformAdmin: true}},
		Audit:      writer,
		AuditStore: storeImpl,
	})
}

func TestAuditRecordsWriteButNotList(t *testing.T) {
	writer := &fakeAuditWriter{}
	router := auditRouter(t, writer, nil)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/clusters/prod/workloads/apps/v1/deployments/web/restart?namespace=default", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart status = %d body=%s", rec.Code, rec.Body.String())
	}
	entry, ok := writer.last()
	if !ok {
		t.Fatal("restart was not audited")
	}
	if entry.Verb != "restart" || entry.ClusterName != "prod" || entry.Namespace != "default" ||
		entry.ObjectName != "web" || entry.ActorSubject != "alice" || entry.Result != "success" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.RequestID == "" {
		t.Fatal("audit entry missing requestId")
	}

	before := len(writer.entries)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters/prod/resources/_/v1/pods?namespace=default", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if len(writer.entries) != before {
		t.Fatal("plain list must not be audited")
	}
}

func TestAuditRecordsSecretRead(t *testing.T) {
	writer := &fakeAuditWriter{}
	router := auditRouter(t, writer, nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/clusters/prod/resources/_/v1/secrets/db?namespace=default", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	entry, ok := writer.last()
	if !ok || entry.Verb != "get" || entry.Resource != "secrets" {
		t.Fatalf("entry = %+v", entry)
	}
	if sensitive, _ := entry.Detail["sensitiveRead"].(bool); !sensitive {
		t.Fatalf("detail = %+v", entry.Detail)
	}
}

func TestAuditQueryAndVerify(t *testing.T) {
	writer := &fakeAuditWriter{}
	now := time.Now().UTC()
	storeImpl := fakeAuditStore{
		items: []domain.AuditEntry{{
			ID: 1, OccurredAt: now, Verb: "delete", Resource: "pods", Result: "success",
		}},
		status:    store.ChainStatus{Checked: 10, BrokenID: 4, BrokenKind: "hash"},
		retention: 180,
	}
	router := auditRouter(t, writer, storeImpl)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/audit-logs?from=2020-01-01T00:00:00Z", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["retentionWarning"] == nil || resp["total"].(float64) != 1 {
		t.Fatalf("resp = %+v", resp)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs/verify", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["intact"] != false || resp["brokenId"].(float64) != 4 {
		t.Fatalf("verify resp = %+v", resp)
	}
}

func TestAuditExportWritesCSVAndRecordsExport(t *testing.T) {
	writer := &fakeAuditWriter{}
	storeImpl := fakeAuditStore{items: []domain.AuditEntry{{
		ID: 1, OccurredAt: time.Now().UTC(), ActorSubject: "alice", Verb: "delete",
		Resource: "pods", Namespace: "default", ObjectName: "web", Result: "success", ResultCode: 200,
	}}}
	router := auditRouter(t, writer, storeImpl)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/audit-logs/export?format=csv", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "occurredAt") || !strings.Contains(rec.Body.String(), "web") {
		t.Fatalf("csv = %q", rec.Body.String())
	}
	entry, ok := writer.last()
	if !ok || entry.Verb != "export" || entry.Resource != "audit_logs" {
		t.Fatalf("export audit entry = %+v", entry)
	}
	if entry.Detail["count"].(int) != 1 {
		t.Fatalf("detail = %+v", entry.Detail)
	}
}
