// Package storetest 提供跨数据库引擎的仓储一致性测试套件。
//
// 两个引擎的测试入口各自提供 Harness（重置数据库、原始 SQL、分区/篡改用例钩子），
// 本包内的用例只依赖 store.Store 接口，保证行为契约一致。
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/crypto"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/store"
)

// Harness 是引擎专属的测试环境适配。
type Harness struct {
	// NewStore 重置数据库 schema 并返回已迁移的仓储实例。
	NewStore func(t *testing.T) store.Store
	// ExpectedMigrations 是该引擎应有的迁移版本列表（按文件名排序）。
	ExpectedMigrations []string
	// FallbackPartition 是审计兜底分区名（PG: audit_logs_default，MySQL: audit_logs_max）。
	FallbackPartition string
	// Exec 执行原始 SQL，`?` 占位符由实现转换为引擎语法。
	Exec func(t *testing.T, st store.Store, query string, args ...any) error
	// QueryInt 执行返回单整数的查询。
	QueryInt func(t *testing.T, st store.Store, query string, args ...any) int
	// QueryString 执行返回单字符串的查询。
	QueryString func(t *testing.T, st store.Store, query string, args ...any) string
	// BreakAppendOnly 临时绕过只追加保护，返回恢复函数。
	BreakAppendOnly func(t *testing.T, st store.Store) func()
	// PartitionOf 返回指定审计记录所在的物理分区名。
	PartitionOf func(t *testing.T, st store.Store, id int64) string
	// EnsurePartition 为指定时间创建月分区（仅用于保留期测试，需在其它分区之前调用）。
	EnsurePartition func(t *testing.T, st store.Store, ts time.Time)
}

// Run 执行全部一致性用例。
func Run(t *testing.T, h Harness) {
	t.Run("MigrateIsIdempotent", func(t *testing.T) { testMigrateIsIdempotent(t, h) })
	t.Run("UserUpsertAndListing", func(t *testing.T) { testUserUpsertAndListing(t, h) })
	t.Run("ClusterCRUDAndStatusRefresh", func(t *testing.T) { testClusterCRUDAndStatusRefresh(t, h) })
	t.Run("ClusterVisibilityFollowsBindings", func(t *testing.T) { testClusterVisibilityFollowsBindings(t, h) })
	t.Run("CredentialEncryptionAndRotation", func(t *testing.T) { testCredentialEncryptionAndRotation(t, h) })
	t.Run("SetClusterAccessReplacesBindings", func(t *testing.T) { testSetClusterAccessReplacesBindings(t, h) })
	t.Run("CreateClusterWithCredentialAtomic", func(t *testing.T) { testCreateClusterWithCredentialAtomic(t, h) })
	t.Run("BootstrapAdmin", func(t *testing.T) { testBootstrapAdmin(t, h) })
	t.Run("Settings", func(t *testing.T) { testSettings(t, h) })
	t.Run("RetentionDaysFailsClosed", func(t *testing.T) { testRetentionDaysFailsClosed(t, h) })
	t.Run("AuditChain", func(t *testing.T) { testAuditChain(t, h) })
	t.Run("AuditAppendOnly", func(t *testing.T) { testAuditAppendOnly(t, h) })
	t.Run("AuditPartitionsAndRetention", func(t *testing.T) { testAuditPartitionsAndRetention(t, h) })
	t.Run("AuditStats", func(t *testing.T) { testAuditStats(t, h) })
}

func mustUser(t *testing.T, st store.Store, subject string) domain.User {
	t.Helper()
	u, err := st.UpsertUser(context.Background(), domain.User{
		Subject: subject, Email: subject + "@example.com", DisplayName: subject,
	})
	if err != nil {
		t.Fatalf("UpsertUser(%s): %v", subject, err)
	}
	return u
}

func mustCluster(t *testing.T, st store.Store, name string, createdBy *int64) domain.Cluster {
	t.Helper()
	c, err := st.CreateCluster(context.Background(), domain.Cluster{
		Name:          name,
		DisplayName:   name,
		Server:        "https://" + name + ".example.com:6443",
		ServerVersion: "v1.37.2",
		Status:        domain.ClusterHealthy,
		CreatedBy:     createdBy,
	})
	if err != nil {
		t.Fatalf("CreateCluster(%s): %v", name, err)
	}
	return c
}

func testMigrateIsIdempotent(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	versions, err := st.AppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("AppliedMigrations: %v", err)
	}
	if !slices.Equal(versions, h.ExpectedMigrations) {
		t.Fatalf("applied migrations = %v, want %v", versions, h.ExpectedMigrations)
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	versions, _ = st.AppliedMigrations(ctx)
	if len(versions) != len(h.ExpectedMigrations) {
		t.Fatalf("migrations duplicated: %v", versions)
	}

	roles, err := st.Roles(ctx)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	if len(roles) != 4 {
		t.Fatalf("seeded roles = %d, want 4", len(roles))
	}

	withChecksum := h.QueryInt(t, st, `SELECT count(*) FROM schema_migrations WHERE checksum <> ''`)
	if withChecksum != len(h.ExpectedMigrations) {
		t.Fatalf("migrations with checksum = %d, want %d", withChecksum, len(h.ExpectedMigrations))
	}
}

func testUserUpsertAndListing(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	u := mustUser(t, st, "alice")
	if u.ID == 0 || u.Source != "oidc" || u.LastLoginAt == nil {
		t.Fatalf("unexpected user: %+v", u)
	}

	updated, err := st.UpsertUser(ctx, domain.User{
		Subject: "alice", Email: "alice@new.example.com", DisplayName: "Alice A.",
	})
	if err != nil {
		t.Fatalf("second UpsertUser: %v", err)
	}
	if updated.ID != u.ID {
		t.Fatalf("upsert should keep id: got %d, want %d", updated.ID, u.ID)
	}
	if updated.Email != "alice@new.example.com" || updated.DisplayName != "Alice A." {
		t.Fatalf("upsert did not refresh profile: %+v", updated)
	}

	n, err := st.CountPlatformAdmins(ctx)
	if err != nil || n != 0 {
		t.Fatalf("CountPlatformAdmins = %d, %v; want 0", n, err)
	}
	if err := st.SetPlatformAdmin(ctx, u.ID, true); err != nil {
		t.Fatalf("SetPlatformAdmin: %v", err)
	}
	if n, _ := st.CountPlatformAdmins(ctx); n != 1 {
		t.Fatalf("CountPlatformAdmins = %d, want 1", n)
	}

	users, err := st.ListUsers(ctx, 10, 0)
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers = %d, %v; want 1", len(users), err)
	}

	if _, err := st.UserBySubject(ctx, "nobody"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UserBySubject missing = %v, want ErrNotFound", err)
	}
}

func testClusterCRUDAndStatusRefresh(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "admin")

	c := mustCluster(t, st, "prod", &owner.ID)
	if c.Status != domain.ClusterHealthy || c.ServerVersion != "v1.37.2" {
		t.Fatalf("unexpected cluster: %+v", c)
	}

	if _, err := st.CreateCluster(ctx, domain.Cluster{Name: "prod", Server: "https://other:6443"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate cluster error = %v, want ErrConflict", err)
	}

	if err := st.UpdateClusterStatus(ctx, c.ID, domain.ClusterDegraded, "certificate expired", "v1.38.0", time.Now()); err != nil {
		t.Fatalf("UpdateClusterStatus: %v", err)
	}
	got, err := st.ClusterByName(ctx, "prod")
	if err != nil {
		t.Fatalf("ClusterByName: %v", err)
	}
	if got.Status != domain.ClusterDegraded || got.ServerVersion != "v1.38.0" || got.Message != "certificate expired" {
		t.Fatalf("status refresh failed: %+v", got)
	}
	if err := st.UpdateClusterStatus(ctx, c.ID, domain.ClusterHealthy, "", "", time.Now()); err != nil {
		t.Fatalf("UpdateClusterStatus without version: %v", err)
	}
	got, _ = st.ClusterByName(ctx, "prod")
	if got.ServerVersion != "v1.38.0" {
		t.Fatalf("empty version must keep previous value, got %q", got.ServerVersion)
	}

	clusters, err := st.Clusters(ctx)
	if err != nil || len(clusters) != 1 {
		t.Fatalf("Clusters = %d, %v; want 1", len(clusters), err)
	}

	if err := st.DeleteCluster(ctx, c.ID); err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}
	if _, err := st.ClusterByID(ctx, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ClusterByID after delete = %v, want ErrNotFound", err)
	}
}

func testClusterVisibilityFollowsBindings(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	admin := mustUser(t, st, "admin")
	alice := mustUser(t, st, "alice")

	prod := mustCluster(t, st, "prod", &admin.ID)
	mustCluster(t, st, "staging", &admin.ID)

	if _, err := st.CreateRoleBinding(ctx, alice.ID, prod.ID, "default", "developer", &admin.ID); err != nil {
		t.Fatalf("CreateRoleBinding: %v", err)
	}

	visible, err := st.ClustersForUser(ctx, alice.ID)
	if err != nil {
		t.Fatalf("ClustersForUser: %v", err)
	}
	if len(visible) != 1 || visible[0].Name != "prod" {
		t.Fatalf("alice should only see prod, got %+v", visible)
	}

	bob := mustUser(t, st, "bob")
	visible, err = st.ClustersForUser(ctx, bob.ID)
	if err != nil || len(visible) != 0 {
		t.Fatalf("bob visible = %+v, %v; want empty", visible, err)
	}

	bindings, err := st.BindingsForUser(ctx, alice.ID)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("BindingsForUser = %+v, %v", bindings, err)
	}
	if bindings[0].ClusterName != "prod" || bindings[0].Namespace != "default" || bindings[0].RoleCode != "developer" {
		t.Fatalf("unexpected binding: %+v", bindings[0])
	}

	if err := st.DeleteRoleBinding(ctx, bindings[0].ID); err != nil {
		t.Fatalf("DeleteRoleBinding: %v", err)
	}
	if visible, _ := st.ClustersForUser(ctx, alice.ID); len(visible) != 0 {
		t.Fatalf("alice should see nothing after revoke, got %+v", visible)
	}

	if _, err := st.CreateRoleBinding(ctx, alice.ID, prod.ID, "*", "viewer", &admin.ID); err != nil {
		t.Fatalf("bind viewer: %v", err)
	}
	if _, err := st.CreateRoleBinding(ctx, alice.ID, prod.ID, "*", "viewer", &admin.ID); err != nil {
		t.Fatalf("repeat bind viewer: %v", err)
	}
	all, _ := st.BindingsForCluster(ctx, prod.ID)
	if len(all) != 1 {
		t.Fatalf("duplicate binding created %d rows", len(all))
	}
}

func testCredentialEncryptionAndRotation(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	admin := mustUser(t, st, "admin")
	c := mustCluster(t, st, "prod", &admin.ID)

	ring, err := crypto.NewRing(1, map[int][]byte{1: bytes32(1)})
	if err != nil {
		t.Fatalf("NewRing: %v", err)
	}
	secret := []byte("apiVersion: v1\nkind: Config\nusers:\n- name: admin\n  user:\n    token: super-secret\n")

	sealed, err := ring.Seal(secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := st.ReplaceCredential(ctx, domain.Credential{
		ClusterID: c.ID, Ciphertext: sealed.Ciphertext, Nonce: sealed.Nonce,
		KeyVersion: sealed.KeyVersion, AuthType: "kubeconfig", Fingerprint: crypto.Fingerprint(secret),
	}); err != nil {
		t.Fatalf("ReplaceCredential: %v", err)
	}

	stored, err := st.CredentialByClusterID(ctx, c.ID)
	if err != nil {
		t.Fatalf("CredentialByClusterID: %v", err)
	}
	if strings.Contains(string(stored.Ciphertext), "super-secret") {
		t.Fatal("ciphertext must not contain plaintext")
	}
	plain, err := ring.Open(crypto.Sealed{
		Nonce: stored.Nonce, Ciphertext: stored.Ciphertext, KeyVersion: stored.KeyVersion,
	})
	if err != nil {
		t.Fatalf("Open stored: %v", err)
	}
	if string(plain) != string(secret) {
		t.Fatal("decrypted credential mismatch")
	}

	ring2, err := crypto.NewRing(2, map[int][]byte{1: bytes32(1), 2: bytes32(2)})
	if err != nil {
		t.Fatalf("NewRing v2: %v", err)
	}
	rotated, err := ring2.Rotate(crypto.Sealed{
		Nonce: stored.Nonce, Ciphertext: stored.Ciphertext, KeyVersion: stored.KeyVersion,
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if err := st.ReplaceCredential(ctx, domain.Credential{
		ClusterID: c.ID, Ciphertext: rotated.Ciphertext, Nonce: rotated.Nonce,
		KeyVersion: rotated.KeyVersion, AuthType: "kubeconfig", Fingerprint: crypto.Fingerprint(secret),
	}); err != nil {
		t.Fatalf("ReplaceCredential rotated: %v", err)
	}

	after, _ := st.CredentialByClusterID(ctx, c.ID)
	if after.KeyVersion != 2 {
		t.Fatalf("key version = %d, want 2", after.KeyVersion)
	}
	if _, err := ring.Open(crypto.Sealed{
		Nonce: after.Nonce, Ciphertext: after.Ciphertext, KeyVersion: after.KeyVersion,
	}); !errors.Is(err, crypto.ErrUnknownKeyVersion) {
		t.Fatalf("old ring must not decrypt v2 credential, got %v", err)
	}
	if old, _ := st.CredentialsByKeyVersion(ctx, 1); len(old) != 0 {
		t.Fatalf("no credential should remain on v1, got %d", len(old))
	}
}

func testSetClusterAccessReplacesBindings(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	admin := mustUser(t, st, "admin")
	alice := mustUser(t, st, "alice")
	prod := mustCluster(t, st, "prod", &admin.ID)

	if _, err := st.CreateRoleBinding(ctx, alice.ID, prod.ID, "default", "viewer", &admin.ID); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	err := st.SetClusterAccess(ctx, alice.ID, prod.ID, []domain.RoleBindingInput{
		{Namespace: "default", RoleCode: "developer"},
		{Namespace: "kube-system", RoleCode: "viewer"},
	}, &admin.ID)
	if err != nil {
		t.Fatalf("SetClusterAccess: %v", err)
	}

	bindings, _ := st.BindingsForUser(ctx, alice.ID)
	if len(bindings) != 2 {
		t.Fatalf("bindings after replace = %d, want 2", len(bindings))
	}
	for _, b := range bindings {
		if b.Namespace == "default" && b.RoleCode != "developer" {
			t.Fatalf("default binding not replaced: %+v", b)
		}
	}

	err = st.SetClusterAccess(ctx, alice.ID, prod.ID, []domain.RoleBindingInput{
		{Namespace: "default", RoleCode: "developer"},
		{Namespace: "prod", RoleCode: "no-such-role"},
	}, &admin.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown role error = %v, want ErrNotFound", err)
	}
	bindings, _ = st.BindingsForUser(ctx, alice.ID)
	if len(bindings) != 2 {
		t.Fatalf("failed transaction must roll back, got %d bindings", len(bindings))
	}

	err = st.SetClusterAccess(ctx, alice.ID, prod.ID, []domain.RoleBindingInput{
		{Namespace: "default", RoleCode: "developer"},
		{Namespace: "default", RoleCode: "developer"},
		{Namespace: "", RoleCode: "viewer"},
		{Namespace: "*", RoleCode: "viewer"},
	}, &admin.ID)
	if err != nil {
		t.Fatalf("SetClusterAccess with duplicates: %v", err)
	}
	bindings, _ = st.BindingsForUser(ctx, alice.ID)
	if len(bindings) != 2 {
		t.Fatalf("duplicates should collapse to 2 bindings, got %d", len(bindings))
	}
}

func testCreateClusterWithCredentialAtomic(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	cred := domain.Credential{
		Ciphertext: []byte("cipher"), Nonce: []byte("nonce"),
		KeyVersion: 1, AuthType: "token", Fingerprint: "fp",
	}
	c, err := st.CreateClusterWithCredential(ctx, domain.Cluster{
		Name: "atomic", Server: "https://atomic.example.com:6443",
		ServerVersion: "v1.37.2", Status: domain.ClusterHealthy,
	}, cred)
	if err != nil {
		t.Fatalf("CreateClusterWithCredential: %v", err)
	}
	got, err := st.CredentialByClusterID(ctx, c.ID)
	if err != nil || got.Fingerprint != "fp" || string(got.Ciphertext) != "cipher" {
		t.Fatalf("credential = %+v err=%v", got, err)
	}
	if _, err := st.CreateClusterWithCredential(ctx, domain.Cluster{Name: "atomic", Server: "https://x"}, cred); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate err = %v, want conflict", err)
	}
	if err := st.UpdateClusterConnection(ctx, c.ID, "https://new.example.com:6443", "v1.38.0", "healthy", ""); err != nil {
		t.Fatalf("UpdateClusterConnection: %v", err)
	}
	updated, err := st.ClusterByID(ctx, c.ID)
	if err != nil || updated.Server != "https://new.example.com:6443" || updated.ServerVersion != "v1.38.0" {
		t.Fatalf("updated = %+v err=%v", updated, err)
	}
}

func testBootstrapAdmin(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	u, err := st.BootstrapAdmin(ctx, "root", "root@example.com", "Root")
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if !u.IsPlatformAdmin || u.Status != "active" || u.Source != "local" {
		t.Fatalf("unexpected bootstrap user: %+v", u)
	}
	if n, err := st.CountPlatformAdmins(ctx); err != nil || n != 1 {
		t.Fatalf("CountPlatformAdmins = %d, %v; want 1", n, err)
	}
	if _, err := st.BootstrapAdmin(ctx, "other", "", ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second BootstrapAdmin = %v, want ErrConflict", err)
	}
}

func testSettings(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	raw, err := st.GetSetting(ctx, domain.SettingAuditRetentionDays)
	if err != nil || strings.TrimSpace(string(raw)) != "180" {
		t.Fatalf("default retention = %q, %v; want 180", raw, err)
	}
	if err := st.SetSetting(ctx, domain.SettingAuditRetentionDays, 90); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if days, _ := st.RetentionDays(ctx); days != 90 {
		t.Fatalf("RetentionDays = %d, want 90", days)
	}
	if _, err := st.GetSetting(ctx, "no.such.key"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing setting error = %v, want ErrNotFound", err)
	}

	// 平台默认最低版本应随迁移落到 1.18（新库依次应用 init 与 version 迁移）。
	rawMin, err := st.GetSetting(ctx, domain.SettingMinK8sVersion)
	if err != nil {
		t.Fatalf("min k8s version: %v", err)
	}
	var minVersion string
	if err := json.Unmarshal(rawMin, &minVersion); err != nil || minVersion != "1.18" {
		t.Fatalf("default min k8s version = %q, %v; want \"1.18\"", rawMin, err)
	}
}

func testRetentionDaysFailsClosed(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	if days, err := st.RetentionDays(ctx); err != nil || days != 180 {
		t.Fatalf("default retention = %d, %v; want 180", days, err)
	}
	if err := st.SetSetting(ctx, domain.SettingAuditRetentionDays, "not-a-number"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if _, err := st.RetentionDays(ctx); err == nil {
		t.Fatal("invalid retention value must return an error")
	}
}

func testAuditChain(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	actor := mustUser(t, st, "alice")
	cluster := mustCluster(t, st, "prod", &actor.ID)

	var lastHash string
	for i := 0; i < 3; i++ {
		rec, err := st.AppendAudit(ctx, domain.AuditEntry{
			RequestID:    "req-" + string(rune('a'+i)),
			ActorID:      &actor.ID,
			ActorSubject: actor.Subject,
			ActorEmail:   actor.Email,
			ClusterID:    &cluster.ID,
			ClusterName:  cluster.Name,
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
			Detail:       map[string]any{"replicas": 3, "reason": "scale"},
		})
		if err != nil {
			t.Fatalf("AppendAudit %d: %v", i, err)
		}
		if rec.PrevHash != lastHash {
			t.Fatalf("prev hash = %q, want %q", rec.PrevHash, lastHash)
		}
		if len(rec.Hash) != 64 {
			t.Fatalf("hash %q should be 64 hex chars", rec.Hash)
		}
		lastHash = rec.Hash
	}

	status, err := st.VerifyAuditChain(ctx)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if status.Checked != 3 || status.BrokenID != 0 {
		t.Fatalf("chain status = %+v, want 3 checked and no break", status)
	}

	entries, err := st.QueryAudit(ctx, store.AuditFilter{ActorSubject: "alice", Resource: "deployments"})
	if err != nil || len(entries) != 3 {
		t.Fatalf("QueryAudit = %d, %v; want 3", len(entries), err)
	}
	if entries[0].Detail["reason"] != "scale" {
		t.Fatalf("detail round-trip failed: %v", entries[0].Detail)
	}
	if entries[0].ClientIP != "10.0.0.8" {
		t.Fatalf("client ip = %q", entries[0].ClientIP)
	}

	restore := h.BreakAppendOnly(t, st)
	if err := h.Exec(t, st, `UPDATE audit_logs SET body_hash = ? WHERE id = ?`, "sha256:tampered", entries[1].ID); err != nil {
		restore()
		t.Fatalf("tamper row: %v", err)
	}
	restore()

	status, err = st.VerifyAuditChain(ctx)
	if err != nil {
		t.Fatalf("VerifyAuditChain after tamper: %v", err)
	}
	if status.BrokenID != entries[1].ID || status.BrokenKind != "hash" {
		t.Fatalf("tamper not detected: %+v (want id %d)", status, entries[1].ID)
	}
}

func testAuditAppendOnly(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	rec, err := st.AppendAudit(ctx, domain.AuditEntry{ActorSubject: "alice", Verb: "delete", Resource: "pods"})
	if err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}

	if err := h.Exec(t, st, `UPDATE audit_logs SET result = 'failure' WHERE id = ?`, rec.ID); err == nil {
		t.Fatal("UPDATE on audit_logs must be rejected")
	}
	if err := h.Exec(t, st, `DELETE FROM audit_logs WHERE id = ?`, rec.ID); err == nil {
		t.Fatal("DELETE on audit_logs must be rejected")
	}
}

func testAuditPartitionsAndRetention(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	// 先创建 200 天前的分区（此时只有兜底分区），再追加旧记录。
	old := time.Now().UTC().AddDate(0, 0, -200)
	h.EnsurePartition(t, st, old)
	oldPart := auditPartitionName(old)
	if !contains(mustPartitions(t, st), oldPart) {
		t.Fatalf("old partition %q not created: %v", oldPart, mustPartitions(t, st))
	}
	rec, err := st.AppendAudit(ctx, domain.AuditEntry{
		OccurredAt: old, ActorSubject: "alice", Verb: "create", Resource: "configmaps",
	})
	if err != nil {
		t.Fatalf("AppendAudit old: %v", err)
	}
	if got := h.PartitionOf(t, st, rec.ID); got != oldPart {
		t.Fatalf("old row landed in %q, want %q", got, oldPart)
	}

	// 启动维护：创建当前月与未来月份分区。
	if _, err := st.EnsureAuditPartitions(ctx, 3); err != nil {
		t.Fatalf("EnsureAuditPartitions: %v", err)
	}
	parts := mustPartitions(t, st)
	if !contains(parts, h.FallbackPartition) {
		t.Fatalf("fallback partition %q missing: %v", h.FallbackPartition, parts)
	}
	nowPart := auditPartitionName(time.Now())
	if !contains(parts, nowPart) {
		t.Fatalf("current month partition %q missing: %v", nowPart, parts)
	}

	// 未来写入应落到对应月份分区，而不是兜底分区。
	future := time.Now().UTC().AddDate(0, 2, 0)
	futureRec, err := st.AppendAudit(ctx, domain.AuditEntry{
		OccurredAt: future, ActorSubject: "alice", Verb: "create", Resource: "configmaps",
	})
	if err != nil {
		t.Fatalf("AppendAudit future: %v", err)
	}
	if got := h.PartitionOf(t, st, futureRec.ID); got != auditPartitionName(future) {
		t.Fatalf("future row landed in %q, want %q", got, auditPartitionName(future))
	}

	// 保留策略：只删除整月早于截止时间的分区，保留当前月与兜底分区。
	dropped, err := st.DropExpiredAuditPartitions(ctx, 180)
	if err != nil {
		t.Fatalf("DropExpiredAuditPartitions: %v", err)
	}
	if !contains(dropped, oldPart) {
		t.Fatalf("dropped = %v, want %q", dropped, oldPart)
	}
	parts = mustPartitions(t, st)
	if contains(parts, oldPart) {
		t.Fatalf("partition %q still exists after retention", oldPart)
	}
	if !contains(parts, nowPart) {
		t.Fatal("retention must not drop the current month")
	}
	if !contains(parts, h.FallbackPartition) {
		t.Fatal("retention must not drop the fallback partition")
	}

	days, err := st.RetentionDays(ctx)
	if err != nil || days != 180 {
		t.Fatalf("RetentionDays = %d, %v; want 180", days, err)
	}
}

func testAuditStats(t *testing.T, h Harness) {
	st := h.NewStore(t)
	ctx := context.Background()

	actor := mustUser(t, st, "alice")
	cluster := mustCluster(t, st, "prod", &actor.ID)
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	fixtures := []domain.AuditEntry{
		{RequestID: "stats-1", ActorID: &actor.ID, ActorSubject: actor.Subject, ClusterID: &cluster.ID,
			ClusterName: cluster.Name, Verb: "create", Resource: "deployments", Result: "success"},
		{RequestID: "stats-2", ActorID: &actor.ID, ActorSubject: actor.Subject, ClusterID: &cluster.ID,
			ClusterName: cluster.Name, Verb: "patch", Resource: "deployments", Result: "failure"},
		{RequestID: "stats-3", ActorID: &actor.ID, ActorSubject: actor.Subject, ClusterID: &cluster.ID,
			ClusterName: cluster.Name, Verb: "create", Resource: "pods", Result: "success"},
	}
	offsets := []time.Duration{0, time.Hour, 26 * time.Hour}
	for i := range fixtures {
		fixtures[i].OccurredAt = base.Add(offsets[i])
		if _, err := st.AppendAudit(ctx, fixtures[i]); err != nil {
			t.Fatalf("AppendAudit %d: %v", i, err)
		}
	}

	stats, err := st.AuditStats(ctx, store.AuditStatsFilter{
		From: base.Add(-time.Minute), To: base.Add(72 * time.Hour),
	})
	if err != nil {
		t.Fatalf("AuditStats: %v", err)
	}
	if stats.Total != 3 || stats.Success != 2 || stats.Failed != 1 {
		t.Fatalf("totals = %+v, want 3/2/1", stats)
	}
	if len(stats.ByDay) != 2 {
		t.Fatalf("byDay = %+v, want 2 days", stats.ByDay)
	}
	if stats.ByVerb[0].Key != "create" || stats.ByVerb[0].Count != 2 {
		t.Fatalf("byVerb = %+v, want create=2 first", stats.ByVerb)
	}
	if len(stats.ByResource) != 2 || stats.ByResource[0].Key != "deployments" {
		t.Fatalf("byResource = %+v, want deployments first", stats.ByResource)
	}
	if len(stats.ByActor) != 1 || stats.ByActor[0].Count != 3 {
		t.Fatalf("byActor = %+v", stats.ByActor)
	}

	empty, err := st.AuditStats(ctx, store.AuditStatsFilter{
		From: base.Add(96 * time.Hour), To: base.Add(120 * time.Hour),
	})
	if err != nil {
		t.Fatalf("AuditStats empty: %v", err)
	}
	if empty.Total != 0 || len(empty.ByDay) != 0 || len(empty.ByVerb) != 0 {
		t.Fatalf("empty stats = %+v", empty)
	}
}

func bytes32(b byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return key
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func mustPartitions(t *testing.T, st store.Store) []string {
	t.Helper()
	parts, err := st.AuditPartitions(context.Background())
	if err != nil {
		t.Fatalf("AuditPartitions: %v", err)
	}
	return parts
}

// auditPartitionName 与两套实现的月分区命名保持一致（UTC 月份）。
func auditPartitionName(ts time.Time) string {
	return "audit_logs_" + ts.UTC().Format("200601")
}
