package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/crypto"
	"github.com/kubedeck/kubedeck/internal/domain"
)

// testStore 连接真实 PostgreSQL。未设置 KUBEDECK_TEST_DATABASE_URL 时跳过。
// 每个测试重置 public schema，保证互不影响。
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("KUBEDECK_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KUBEDECK_TEST_DATABASE_URL not set")
	}
	if !strings.Contains(url, "kubedeck_test") {
		t.Fatalf("refusing to run destructive tests against %q: database name must contain kubedeck_test", url)
	}
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
	if err := Migrate(ctx, st.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func mustUser(t *testing.T, st *Store, subject string) domain.User {
	t.Helper()
	u, err := st.UpsertUser(context.Background(), domain.User{
		Subject: subject, Email: subject + "@example.com", DisplayName: subject,
	})
	if err != nil {
		t.Fatalf("UpsertUser(%s): %v", subject, err)
	}
	return u
}

func mustCluster(t *testing.T, st *Store, name string, createdBy *int64) domain.Cluster {
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

func TestMigrateIsIdempotent(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	versions, err := AppliedMigrations(ctx, st.Pool())
	if err != nil {
		t.Fatalf("AppliedMigrations: %v", err)
	}
	want := []string{"0001_init", "0002_audit_partitions", "0003_audit_hash"}
	if strings.Join(versions, ",") != strings.Join(want, ",") {
		t.Fatalf("applied migrations = %v, want %v", versions, want)
	}

	// 再次执行应当全部跳过且不报错。
	if err := Migrate(ctx, st.Pool()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	versions, _ = AppliedMigrations(ctx, st.Pool())
	if len(versions) != len(want) {
		t.Fatalf("migrations duplicated: %v", versions)
	}

	roles, err := st.Roles(ctx)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	if len(roles) != 4 {
		t.Fatalf("seeded roles = %d, want 4", len(roles))
	}

	var withChecksum int
	if err := st.Pool().QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations WHERE checksum <> ''`).Scan(&withChecksum); err != nil {
		t.Fatalf("count checksums: %v", err)
	}
	if withChecksum != len(want) {
		t.Fatalf("migrations with checksum = %d, want %d", withChecksum, len(want))
	}
}

func TestUserUpsertAndListing(t *testing.T) {
	st := testStore(t)
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

func TestClusterCRUDAndStatusRefresh(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "admin")

	c := mustCluster(t, st, "prod", &owner.ID)
	if c.Status != domain.ClusterHealthy || c.ServerVersion != "v1.37.2" {
		t.Fatalf("unexpected cluster: %+v", c)
	}

	// 同名集群必须冲突（接入边界与冲突处理）。
	if _, err := st.CreateCluster(ctx, domain.Cluster{Name: "prod", Server: "https://other:6443"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate cluster error = %v, want ErrConflict", err)
	}

	// 健康检查刷新版本，空版本保留旧值。
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

func TestClusterVisibilityFollowsBindings(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	admin := mustUser(t, st, "admin")
	alice := mustUser(t, st, "alice")

	prod := mustCluster(t, st, "prod", &admin.ID)
	staging := mustCluster(t, st, "staging", &admin.ID)

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

	// 无绑定用户看到空数组而不是全部集群。
	bob := mustUser(t, st, "bob")
	visible, err = st.ClustersForUser(ctx, bob.ID)
	if err != nil || len(visible) != 0 {
		t.Fatalf("bob visible = %+v, %v; want empty", visible, err)
	}

	// 非法的 namespace 不应泄漏到其他集群。
	bindings, err := st.BindingsForUser(ctx, alice.ID)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("BindingsForUser = %+v, %v", bindings, err)
	}
	if bindings[0].ClusterName != "prod" || bindings[0].Namespace != "default" || bindings[0].RoleCode != "developer" {
		t.Fatalf("unexpected binding: %+v", bindings[0])
	}

	// 回收绑定后可见范围立即收缩。
	if err := st.DeleteRoleBinding(ctx, bindings[0].ID); err != nil {
		t.Fatalf("DeleteRoleBinding: %v", err)
	}
	if visible, _ := st.ClustersForUser(ctx, alice.ID); len(visible) != 0 {
		t.Fatalf("alice should see nothing after revoke, got %+v", visible)
	}

	// 四元组唯一：重复绑定不产生第二行。
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

	_ = staging
}

func TestCredentialEncryptionAndRotation(t *testing.T) {
	st := testStore(t)
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

	// 轮换：用 v2 覆盖，旧密文立即不可用，凭据仍可解密。
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

func TestSetClusterAccessReplacesBindings(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	admin := mustUser(t, st, "admin")
	alice := mustUser(t, st, "alice")
	prod := mustCluster(t, st, "prod", &admin.ID)

	if _, err := st.CreateRoleBinding(ctx, alice.ID, prod.ID, "default", "viewer", &admin.ID); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	err := st.SetClusterAccess(ctx, alice.ID, prod.ID, []RoleBindingInput{
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

	// 未知角色必须整体回滚，不留下半套绑定。
	err = st.SetClusterAccess(ctx, alice.ID, prod.ID, []RoleBindingInput{
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

	// 重复输入必须去重，而不是触发唯一约束回滚。
	err = st.SetClusterAccess(ctx, alice.ID, prod.ID, []RoleBindingInput{
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

func TestAuditChain(t *testing.T) {
	st := testStore(t)
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

	entries, err := st.QueryAudit(ctx, AuditFilter{ActorSubject: "alice", Resource: "deployments"})
	if err != nil || len(entries) != 3 {
		t.Fatalf("QueryAudit = %d, %v; want 3", len(entries), err)
	}
	if entries[0].Detail["reason"] != "scale" {
		t.Fatalf("detail round-trip failed: %v", entries[0].Detail)
	}
	if entries[0].ClientIP != "10.0.0.8" {
		t.Fatalf("client ip = %q", entries[0].ClientIP)
	}

	// 篡改第二条记录（测试中临时关闭只追加触发器），校验应定位到它。
	if _, err := st.Pool().Exec(ctx, `ALTER TABLE audit_logs DISABLE TRIGGER audit_logs_append_only`); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}
	if _, err := st.Pool().Exec(ctx,
		`UPDATE audit_logs SET body_hash = 'sha256:tampered' WHERE id = $1`, entries[1].ID); err != nil {
		t.Fatalf("tamper row: %v", err)
	}
	status, err = st.VerifyAuditChain(ctx)
	if err != nil {
		t.Fatalf("VerifyAuditChain after tamper: %v", err)
	}
	if status.BrokenID != entries[1].ID || status.BrokenKind != "hash" {
		t.Fatalf("tamper not detected: %+v (want id %d)", status, entries[1].ID)
	}
}

func TestAuditAppendOnly(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	rec, err := st.AppendAudit(ctx, domain.AuditEntry{ActorSubject: "alice", Verb: "delete", Resource: "pods"})
	if err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}

	if _, err := st.Pool().Exec(ctx,
		`UPDATE audit_logs SET result = 'failure' WHERE id = $1`, rec.ID); err == nil {
		t.Fatal("UPDATE on audit_logs must be rejected")
	}
	if _, err := st.Pool().Exec(ctx,
		`DELETE FROM audit_logs WHERE id = $1`, rec.ID); err == nil {
		t.Fatal("DELETE on audit_logs must be rejected")
	}
}

func TestAuditPartitionsAndRetention(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	parts, err := st.AuditPartitions(ctx)
	if err != nil {
		t.Fatalf("AuditPartitions: %v", err)
	}
	if !contains(parts, "audit_logs_default") {
		t.Fatalf("default partition missing: %v", parts)
	}
	nowPart := auditPartitionName(time.Now())
	if !contains(parts, nowPart) {
		t.Fatalf("current month partition %q missing: %v", nowPart, parts)
	}

	// 跨月写入：+2 个月的数据应落到该月分区，而不是 default。
	future := time.Now().UTC().AddDate(0, 2, 0)
	rec, err := st.AppendAudit(ctx, domain.AuditEntry{
		OccurredAt: future, ActorSubject: "alice", Verb: "create", Resource: "configmaps",
	})
	if err != nil {
		t.Fatalf("AppendAudit future: %v", err)
	}
	var partName string
	if err := st.Pool().QueryRow(ctx,
		`SELECT tableoid::regclass::text FROM audit_logs WHERE id = $1`, rec.ID).Scan(&partName); err != nil {
		t.Fatalf("read partition: %v", err)
	}
	if partName != auditPartitionName(future) {
		t.Fatalf("future row landed in %q, want %q", partName, auditPartitionName(future))
	}

	// 保留策略：创建 200 天前的分区，按 180 天保留期应被删除。
	old := time.Now().UTC().AddDate(0, 0, -200)
	if _, err := st.EnsureAuditPartitions(ctx, 0); err != nil {
		t.Fatalf("EnsureAuditPartitions: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `SELECT ensure_audit_partition($1)`, old); err != nil {
		t.Fatalf("create old partition: %v", err)
	}
	oldPart := auditPartitionName(old)
	if !contains(mustPartitions(t, st), oldPart) {
		t.Fatalf("old partition %q not created", oldPart)
	}

	dropped, err := st.DropExpiredAuditPartitions(ctx, 180)
	if err != nil {
		t.Fatalf("DropExpiredAuditPartitions: %v", err)
	}
	if !contains(dropped, oldPart) {
		t.Fatalf("dropped = %v, want %q", dropped, oldPart)
	}
	if contains(mustPartitions(t, st), oldPart) {
		t.Fatalf("partition %q still exists after retention", oldPart)
	}
	if !contains(mustPartitions(t, st), nowPart) {
		t.Fatal("retention must not drop the current month")
	}

	days, err := st.RetentionDays(ctx)
	if err != nil || days != 180 {
		t.Fatalf("RetentionDays = %d, %v; want 180", days, err)
	}
}

func TestSettings(t *testing.T) {
	st := testStore(t)
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
}

func TestRetentionDaysFailsClosed(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 未配置时回退默认值。
	if days, err := st.RetentionDays(ctx); err != nil || days != 180 {
		t.Fatalf("default retention = %d, %v; want 180", days, err)
	}

	// 已配置但值非法时必须报错，调用方据此跳过清理，而不是回退 180。
	if err := st.SetSetting(ctx, domain.SettingAuditRetentionDays, "not-a-number"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if _, err := st.RetentionDays(ctx); err == nil {
		t.Fatal("invalid retention value must return an error")
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

func mustPartitions(t *testing.T, st *Store) []string {
	t.Helper()
	parts, err := st.AuditPartitions(context.Background())
	if err != nil {
		t.Fatalf("AuditPartitions: %v", err)
	}
	return parts
}

// auditPartitionName 与 SQL 函数 audit_partition_name 保持一致（UTC 月份）。
func auditPartitionName(ts time.Time) string {
	return "audit_logs_" + ts.UTC().Format("200601")
}

func TestCreateClusterWithCredentialAtomic(t *testing.T) {
	st := testStore(t)
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
