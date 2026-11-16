package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/store"
	"github.com/kubedeck/kubedeck/internal/store/storetest"
)

// TestStore 用真实 PostgreSQL 执行跨引擎一致性套件。
// 未设置 KUBEDECK_TEST_DATABASE_URL 时跳过。
func TestStore(t *testing.T) {
	url := os.Getenv("KUBEDECK_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KUBEDECK_TEST_DATABASE_URL not set")
	}
	if !strings.Contains(url, "kubedeck_test") {
		t.Fatalf("refusing to run destructive tests against %q: database name must contain kubedeck_test", url)
	}
	storetest.Run(t, storetest.Harness{
		NewStore:           newTestStore(url),
		ExpectedMigrations: expectedMigrations(t),
		FallbackPartition:  "audit_logs_default",
		Exec:               pgExec,
		QueryInt:           pgQueryInt,
		QueryString:        pgQueryString,
		BreakAppendOnly:    pgBreakAppendOnly,
		PartitionOf:        pgPartitionOf,
		EnsurePartition:    pgEnsurePartition,
	})
}

func newTestStore(url string) func(t *testing.T) store.Store {
	return func(t *testing.T) store.Store {
		t.Helper()
		ctx := context.Background()
		st, err := Open(ctx, url)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		t.Cleanup(st.Close)
		if _, err := st.Pool().Exec(ctx,
			`DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`); err != nil {
			t.Fatalf("reset schema: %v", err)
		}
		if err := st.Migrate(ctx); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		return st
	}
}

func expectedMigrations(t *testing.T) []string {
	t.Helper()
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, strings.TrimSuffix(path.Base(file), ".sql"))
	}
	return out
}

// rebind 把测试中的 `?` 占位符转换为 PostgreSQL 的 $n。
func rebind(query string) string {
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func pgExec(t *testing.T, st store.Store, query string, args ...any) error {
	t.Helper()
	_, err := st.(*Store).Pool().Exec(context.Background(), rebind(query), args...)
	return err
}

func pgQueryInt(t *testing.T, st store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.(*Store).Pool().QueryRow(context.Background(), rebind(query), args...).Scan(&n); err != nil {
		t.Fatalf("query int %q: %v", query, err)
	}
	return n
}

func pgQueryString(t *testing.T, st store.Store, query string, args ...any) string {
	t.Helper()
	var s string
	if err := st.(*Store).Pool().QueryRow(context.Background(), rebind(query), args...).Scan(&s); err != nil {
		t.Fatalf("query string %q: %v", query, err)
	}
	return s
}

func pgBreakAppendOnly(t *testing.T, st store.Store) func() {
	t.Helper()
	pool := st.(*Store).Pool()
	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE audit_logs DISABLE TRIGGER audit_logs_append_only`); err != nil {
		t.Fatalf("disable append-only trigger: %v", err)
	}
	return func() {
		if _, err := pool.Exec(context.Background(),
			`ALTER TABLE audit_logs ENABLE TRIGGER audit_logs_append_only`); err != nil {
			t.Fatalf("enable append-only trigger: %v", err)
		}
	}
}

func pgPartitionOf(t *testing.T, st store.Store, id int64) string {
	t.Helper()
	return pgQueryString(t, st, `SELECT tableoid::regclass::text FROM audit_logs WHERE id = ?`, id)
}

func pgEnsurePartition(t *testing.T, st store.Store, ts time.Time) {
	t.Helper()
	if _, err := st.(*Store).Pool().Exec(context.Background(),
		`SELECT ensure_audit_partition($1)`, ts.UTC()); err != nil {
		t.Fatalf("ensure audit partition %s: %v", ts, err)
	}
}
