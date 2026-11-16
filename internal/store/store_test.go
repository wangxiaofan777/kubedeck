package store

import (
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/domain"
)

func TestResolveDriver(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		explicit string
		want     string
		wantErr  bool
	}{
		{name: "postgres url", url: "postgres://u:p@h:5432/db", want: DriverPostgres},
		{name: "postgresql url", url: "postgresql://u:p@h:5432/db", want: DriverPostgres},
		{name: "mysql url", url: "mysql://u:p@h:3306/db", want: DriverMySQL},
		{name: "keyword dsn defaults postgres", url: "host=h dbname=db", want: DriverPostgres},
		{name: "explicit mysql with native dsn", url: "u:p@tcp(h:3306)/db", explicit: "mysql", want: DriverMySQL},
		{name: "explicit postgres", url: "postgres://u:p@h/db", explicit: "Postgres", want: DriverPostgres},
		{name: "unsupported driver", url: "mysql://u:p@h/db", explicit: "sqlite", wantErr: true},
		{name: "conflicting driver", url: "mysql://u:p@h/db", explicit: "postgres", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveDriver(tc.url, tc.explicit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveDriver(%q, %q) = %q, want error", tc.url, tc.explicit, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveDriver(%q, %q): %v", tc.url, tc.explicit, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveDriver(%q, %q) = %q, want %q", tc.url, tc.explicit, got, tc.want)
			}
		})
	}
}

func TestCanonicalAuditDetail(t *testing.T) {
	got, err := CanonicalAuditDetail([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("CanonicalAuditDetail: %v", err)
	}
	if got != `{"a":1,"b":2}` {
		t.Fatalf("canonical = %q", got)
	}
	if got, err := CanonicalAuditDetail(nil); err != nil || got != "{}" {
		t.Fatalf("nil detail = %q, %v", got, err)
	}
}

func TestComputeAuditHashIsStable(t *testing.T) {
	e := entryForHash()
	first := ComputeAuditHash(e, `{"a":1}`)
	second := ComputeAuditHash(e, `{"a":1}`)
	if first != second || len(first) != 64 {
		t.Fatalf("hash = %q / %q", first, second)
	}
	e.RequestID = "changed"
	if ComputeAuditHash(e, `{"a":1}`) == first {
		t.Fatal("hash must change when a field changes")
	}
}

func entryForHash() domain.AuditEntry {
	actorID, clusterID := int64(7), int64(9)
	return domain.AuditEntry{
		OccurredAt:   time.Date(2026, 11, 10, 12, 0, 0, 123456000, time.UTC),
		RequestID:    "req-1",
		ActorID:      &actorID,
		ActorSubject: "alice",
		ActorEmail:   "alice@example.com",
		ClusterID:    &clusterID,
		ClusterName:  "prod",
		Verb:         "patch",
		APIGroup:     "apps",
		Resource:     "deployments",
		Namespace:    "default",
		ObjectName:   "web",
		Result:       "success",
		ResultCode:   200,
		LatencyMS:    12,
		ClientIP:     "10.0.0.8",
		UserAgent:    "kubedeck-test",
		BodyHash:     "sha256:deadbeef",
		PrevHash:     "prev",
	}
}
