// Package store 定义平台元数据的仓储契约，并提供与具体数据库引擎无关的共享类型与工具。
//
// 只存平台元数据（用户、集群、凭据密文、角色绑定、审计）；集群资源状态一律不入库。
// 具体实现位于 postgres 与 mysql 子包。
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// 受支持的存储引擎标识，对应 KUBEDECK_DB_DRIVER 的取值。
const (
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"
)

// DefaultRetentionDays 是未配置审计保留期时使用的默认天数。
const DefaultRetentionDays = 180

// Store 是平台元数据仓储的完整契约。业务层可以继续依赖更窄的接口，
// 但每个引擎实现都必须满足本接口，以保证行为可替换。
type Store interface {
	// Driver 返回引擎标识（postgres/mysql），用于日志与诊断。
	Driver() string
	// Migrate 应用该引擎尚未执行的迁移。
	Migrate(ctx context.Context) error
	// AppliedMigrations 返回已应用的迁移版本，供启动日志与测试断言使用。
	AppliedMigrations(ctx context.Context) ([]string, error)
	// Ping 检查数据库可用性。
	Ping(ctx context.Context) error
	// Close 释放连接池。
	Close()

	// 用户
	UpsertUser(ctx context.Context, u domain.User) (domain.User, error)
	UserBySubject(ctx context.Context, subject string) (domain.User, error)
	UserByID(ctx context.Context, id int64) (domain.User, error)
	ListUsers(ctx context.Context, limit, offset int) ([]domain.User, error)
	CountPlatformAdmins(ctx context.Context) (int, error)
	SetUserStatus(ctx context.Context, id int64, status string) error
	SetPlatformAdmin(ctx context.Context, id int64, admin bool) error

	// 集群与凭据
	CreateCluster(ctx context.Context, c domain.Cluster) (domain.Cluster, error)
	ClusterByName(ctx context.Context, name string) (domain.Cluster, error)
	ClusterByID(ctx context.Context, id int64) (domain.Cluster, error)
	Clusters(ctx context.Context) ([]domain.Cluster, error)
	ClustersForUser(ctx context.Context, userID int64) ([]domain.Cluster, error)
	UpdateClusterStatus(ctx context.Context, id int64, status domain.ClusterStatus, message, version string, seenAt time.Time) error
	RenameCluster(ctx context.Context, id int64, displayName string, labels map[string]string) error
	DeleteCluster(ctx context.Context, id int64) error
	CreateClusterWithCredential(ctx context.Context, c domain.Cluster, cred domain.Credential) (domain.Cluster, error)
	ClusterCredentialMeta(ctx context.Context, clusterID int64) (domain.Credential, error)
	UpdateClusterConnection(ctx context.Context, id int64, server, version, status, message string) error

	ReplaceCredential(ctx context.Context, c domain.Credential) error
	CredentialByClusterID(ctx context.Context, clusterID int64) (domain.Credential, error)
	CredentialsByKeyVersion(ctx context.Context, keyVersion int) ([]domain.Credential, error)
	CountCredentials(ctx context.Context) (int, error)

	// 角色与绑定
	Roles(ctx context.Context) ([]domain.Role, error)
	RoleByCode(ctx context.Context, code string) (domain.Role, error)
	CreateRoleBinding(ctx context.Context, userID, clusterID int64, namespace, roleCode string, grantedBy *int64) (domain.RoleBinding, error)
	RoleBindingByID(ctx context.Context, id int64) (domain.RoleBinding, error)
	BindingsForUser(ctx context.Context, userID int64) ([]domain.RoleBinding, error)
	BindingsForCluster(ctx context.Context, clusterID int64) ([]domain.RoleBinding, error)
	DeleteRoleBinding(ctx context.Context, id int64) error
	SetClusterAccess(ctx context.Context, userID, clusterID int64, bindings []domain.RoleBindingInput, grantedBy *int64) error
	AllRoleBindings(ctx context.Context) ([]domain.RoleBinding, error)

	// 平台管理员与设置
	BootstrapAdmin(ctx context.Context, subject, email, displayName string) (domain.User, error)
	SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error)
	GetSetting(ctx context.Context, key string) (json.RawMessage, error)
	SetSetting(ctx context.Context, key string, value any) error

	// 审计
	AppendAudit(ctx context.Context, e domain.AuditEntry) (domain.AuditRecord, error)
	AppendAuditBatch(ctx context.Context, entries []domain.AuditEntry) ([]domain.AuditRecord, error)
	QueryAudit(ctx context.Context, f AuditFilter) ([]domain.AuditEntry, error)
	CountAudit(ctx context.Context, f AuditFilter) (int, error)
	VerifyAuditChain(ctx context.Context) (ChainStatus, error)
	AuditStats(ctx context.Context, f AuditStatsFilter) (AuditStats, error)
	EnsureAuditPartitions(ctx context.Context, aheadMonths int) (int, error)
	DropExpiredAuditPartitions(ctx context.Context, retainDays int) ([]string, error)
	AuditPartitions(ctx context.Context) ([]string, error)
	RetentionDays(ctx context.Context) (int, error)
}

// ResolveDriver 根据连接串与显式驱动确定引擎。
// 显式驱动为空时按 URL scheme 识别；无 scheme 的连接串保持向后兼容，默认 PostgreSQL。
// 显式驱动与可识别 scheme 冲突、或驱动取值不受支持时返回错误。
func ResolveDriver(databaseURL, explicit string) (string, error) {
	explicit = strings.ToLower(strings.TrimSpace(explicit))
	switch explicit {
	case "", DriverPostgres, DriverMySQL:
	default:
		return "", fmt.Errorf("database_driver: unsupported %q (want %s|%s)", explicit, DriverPostgres, DriverMySQL)
	}

	detected := ""
	lowered := strings.ToLower(strings.TrimSpace(databaseURL))
	switch {
	case strings.HasPrefix(lowered, "postgres://"), strings.HasPrefix(lowered, "postgresql://"):
		detected = DriverPostgres
	case strings.HasPrefix(lowered, "mysql://"):
		detected = DriverMySQL
	}

	if explicit != "" && detected != "" && explicit != detected {
		return "", fmt.Errorf("database_driver %q conflicts with database_url scheme %q", explicit, detected)
	}
	if explicit != "" {
		return explicit, nil
	}
	if detected != "" {
		return detected, nil
	}
	return DriverPostgres, nil
}

// AuditFilter 是审计查询条件；零值表示不限。
type AuditFilter struct {
	From         *time.Time
	To           *time.Time
	ActorSubject string
	ClusterName  string
	Resource     string
	Result       string
	Limit        int
	Offset       int
}

// AuditStatsFilter 是审计统计范围；From/To 必填，ClusterName 为空表示全部集群。
type AuditStatsFilter struct {
	From        time.Time
	To          time.Time
	ClusterName string
}

// AuditCount 是单个分组键的计数。
type AuditCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// AuditDayCount 是按天汇总的成功/失败计数。
type AuditDayCount struct {
	Day    string `json:"day"`
	Total  int    `json:"total"`
	Failed int    `json:"failed"`
}

// AuditStats 是审计聚合结果，只包含计数与分组键。
type AuditStats struct {
	From       time.Time       `json:"from"`
	To         time.Time       `json:"to"`
	Total      int             `json:"total"`
	Success    int             `json:"success"`
	Failed     int             `json:"failed"`
	ByDay      []AuditDayCount `json:"byDay"`
	ByVerb     []AuditCount    `json:"byVerb"`
	ByResource []AuditCount    `json:"byResource"`
	ByActor    []AuditCount    `json:"byActor"`
}

// ChainStatus 是哈希链校验结果。Truncated 表示最早的分区已被保留策略删除，
// 校验从第一条存活记录开始，不能据此判断更早的历史是否被篡改。
type ChainStatus struct {
	Checked    int    `json:"checked"`
	BrokenID   int64  `json:"brokenId,omitempty"`
	BrokenKind string `json:"brokenKind,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// PageArgs 归一化用户列表分页参数。
func PageArgs(limit, offset int) (int, int) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// AuditPageArgs 是审计专用分页：导出需要一次取回较大结果，不能套用用户列表的 500 上限。
func AuditPageArgs(limit, offset int) (int, int) {
	const maxAuditPage = 5000
	if limit <= 0 || limit > maxAuditPage {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// PrepareAuditEntry 归一化审计条目并返回可写入的 detail JSON。
// 时间统一为 UTC；结果为空时视为 success；detail 为空时写空对象。
func PrepareAuditEntry(e domain.AuditEntry) (domain.AuditEntry, []byte, error) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	} else {
		e.OccurredAt = e.OccurredAt.UTC()
	}
	// 数据库时间精度为微秒；先截断，保证哈希输入与回读值一致。
	e.OccurredAt = e.OccurredAt.Truncate(time.Microsecond)
	if e.Result == "" {
		e.Result = "success"
	}
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return domain.AuditEntry{}, nil, fmt.Errorf("store: encode audit detail: %w", err)
	}
	return e, detail, nil
}

// CanonicalAuditDetail 把 JSON 值规范化为键排序、无多余空白的文本，
// 供不依赖数据库 JSON 规范化行为的引擎计算稳定哈希。
func CanonicalAuditDetail(raw []byte) (string, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return "{}", nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("store: decode audit detail: %w", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("store: canonicalize audit detail: %w", err)
	}
	return string(out), nil
}

// ComputeAuditHash 按固定字段顺序计算审计记录哈希。
// 布局与 PostgreSQL 的 audit_log_hash 函数一致（UTC 微秒时间戳，\x1f 分隔），
// 但 detail 使用 Go 侧规范化文本。哈希链只在单一引擎内校验。
func ComputeAuditHash(e domain.AuditEntry, detail string) string {
	fields := []string{
		e.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000"),
		e.RequestID,
		nullableID(e.ActorID),
		e.ActorSubject,
		e.ActorEmail,
		nullableID(e.ClusterID),
		e.ClusterName,
		e.Verb,
		e.APIGroup,
		e.Resource,
		e.Namespace,
		e.ObjectName,
		e.Result,
		fmt.Sprintf("%d", e.ResultCode),
		fmt.Sprintf("%d", e.LatencyMS),
		e.ClientIP,
		e.UserAgent,
		e.BodyHash,
		detail,
		e.PrevHash,
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func nullableID(v *int64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}
