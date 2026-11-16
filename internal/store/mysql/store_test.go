package mysql

import (
	"context"
	"database/sql"
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

// TestStore 用真实 MySQL 执行跨引擎一致性套件。
// 未设置 KUBEDECK_TEST_MYSQL_URL 时跳过。
func TestStore(t *testing.T) {
	url := os.Getenv("KUBEDECK_TEST_MYSQL_URL")
	if url == "" {
		t.Skip("KUBEDECK_TEST_MYSQL_URL not set")
	}
	if !strings.Contains(url, "kubedeck_test") {
		t.Fatalf("refusing to run destructive tests against %q: database name must contain kubedeck_test", url)
	}
	storetest.Run(t, storetest.Harness{
		NewStore:           newTestStore(url),
		ExpectedMigrations: expectedMigrations(t),
		FallbackPartition:  auditPartitionMax,
		Exec:               mysqlExec,
		QueryInt:           mysqlQueryInt,
		QueryString:        mysqlQueryString,
		BreakAppendOnly:    mysqlBreakAppendOnly,
		PartitionOf:        mysqlPartitionOf,
		EnsurePartition:    mysqlEnsurePartition,
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
		if err := resetSchema(ctx, st.DB()); err != nil {
			t.Fatalf("reset schema: %v", err)
		}
		if err := st.Migrate(ctx); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		return st
	}
}

func resetSchema(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 0`); err != nil {
		return err
	}
	for _, name := range tables {
		quoted := "`" + strings.ReplaceAll(name, "`", "``") + "`"
		if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoted); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 1`); err != nil {
		return err
	}
	return nil
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

func mysqlExec(t *testing.T, st store.Store, query string, args ...any) error {
	t.Helper()
	_, err := st.(*Store).DB().ExecContext(context.Background(), query, args...)
	return err
}

func mysqlQueryInt(t *testing.T, st store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.(*Store).DB().QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("query int %q: %v", query, err)
	}
	return n
}

func mysqlQueryString(t *testing.T, st store.Store, query string, args ...any) string {
	t.Helper()
	var s string
	if err := st.(*Store).DB().QueryRowContext(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("query string %q: %v", query, err)
	}
	return s
}

func mysqlBreakAppendOnly(t *testing.T, st store.Store) func() {
	t.Helper()
	db := st.(*Store).DB()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DROP TRIGGER IF EXISTS audit_logs_no_update`); err != nil {
		t.Fatalf("drop append-only trigger: %v", err)
	}
	return func() {
		if _, err := db.ExecContext(ctx, `
			CREATE TRIGGER audit_logs_no_update BEFORE UPDATE ON audit_logs
			FOR EACH ROW
			SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_logs is append-only: UPDATE is not allowed'`); err != nil {
			t.Fatalf("restore append-only trigger: %v", err)
		}
	}
}

func mysqlPartitionOf(t *testing.T, st store.Store, id int64) string {
	t.Helper()
	parts, err := st.AuditPartitions(context.Background())
	if err != nil {
		t.Fatalf("AuditPartitions: %v", err)
	}
	for _, name := range parts {
		if name == auditPartitionMax {
			continue
		}
		query := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs PARTITION (`%s`) WHERE id = ?", name)
		if n := mysqlQueryInt(t, st, query, id); n > 0 {
			return name
		}
	}
	return ""
}

func mysqlEnsurePartition(t *testing.T, st store.Store, ts time.Time) {
	t.Helper()
	month := time.Date(ts.UTC().Year(), ts.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	name := auditPartitionName(month)
	upper := month.AddDate(0, 1, 0)
	stmt := fmt.Sprintf(
		`ALTER TABLE audit_logs REORGANIZE PARTITION %s INTO (`+
			`PARTITION %s VALUES LESS THAN ('%s'), `+
			`PARTITION %s VALUES LESS THAN (MAXVALUE))`,
		auditPartitionMax, name, upper.Format("2006-01-02 15:04:05.000000"), auditPartitionMax)
	if _, err := st.(*Store).DB().ExecContext(context.Background(), stmt); err != nil {
		t.Fatalf("ensure audit partition %s: %v", name, err)
	}
}
