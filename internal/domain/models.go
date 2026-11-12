package domain

import "time"

// User 是平台用户（OIDC 首次登录时 upsert）。
type User struct {
	ID              int64      `json:"id"`
	Subject         string     `json:"subject"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"displayName"`
	Source          string     `json:"source"`
	IsPlatformAdmin bool       `json:"isPlatformAdmin"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	LastLoginAt     *time.Time `json:"lastLoginAt,omitempty"`
}

// ClusterStatus 是集群健康三态（另有 unknown 表示尚未探测）。
type ClusterStatus string

const (
	ClusterHealthy     ClusterStatus = "healthy"
	ClusterDegraded    ClusterStatus = "degraded"
	ClusterUnreachable ClusterStatus = "unreachable"
	ClusterUnknown     ClusterStatus = "unknown"
)

// Cluster 是已接入集群的登记信息。凭据单独存放且默认不加载。
type Cluster struct {
	ID            int64             `json:"id"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"displayName"`
	Server        string            `json:"server"`
	Transport     string            `json:"transport"`
	AuthMode      string            `json:"authMode"`
	ServerVersion string            `json:"serverVersion"`
	Status        ClusterStatus     `json:"status"`
	Message       string            `json:"message,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	LastSeenAt    *time.Time        `json:"lastSeenAt,omitempty"`
	CreatedBy     *int64            `json:"createdBy,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// Credential 是加密后的集群凭据；字段只允许在服务端内部流转。
type Credential struct {
	ClusterID   int64
	Ciphertext  []byte
	Nonce       []byte
	KeyVersion  int
	AuthType    string
	Fingerprint string
	RotatedAt   time.Time
	ExpiresAt   *time.Time
}

// Role 是平台预置角色。Permissions 只描述平台级能力，不复制 K8S 规则。
type Role struct {
	ID          int64             `json:"id"`
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Permissions map[string]string `json:"permissions"`
	IsSystem    bool              `json:"isSystem"`
	CreatedAt   time.Time         `json:"createdAt"`
}

// RoleBinding 是用户 × 集群 × 命名空间 × 角色。Namespace 为 "*" 表示全部命名空间。
type RoleBinding struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"userId"`
	ClusterID   int64     `json:"clusterId"`
	ClusterName string    `json:"clusterName"`
	Namespace   string    `json:"namespace"`
	RoleCode    string    `json:"roleCode"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RoleBindingInput 描述一条待写入的绑定；Namespace 为空表示全部命名空间。
type RoleBindingInput struct {
	Namespace string `json:"namespace"`
	RoleCode  string `json:"roleCode"`
}

// AuditEntry 是一条审计记录。写入时 ID 为空，查询时由仓储填充。
type AuditEntry struct {
	ID           int64          `json:"id"`
	OccurredAt   time.Time      `json:"occurredAt"`
	RequestID    string         `json:"requestId"`
	ActorID      *int64         `json:"actorId,omitempty"`
	ActorSubject string         `json:"actorSubject"`
	ActorEmail   string         `json:"actorEmail,omitempty"`
	ClusterID    *int64         `json:"clusterId,omitempty"`
	ClusterName  string         `json:"clusterName,omitempty"`
	Verb         string         `json:"verb"`
	APIGroup     string         `json:"apiGroup,omitempty"`
	Resource     string         `json:"resource,omitempty"`
	Namespace    string         `json:"namespace,omitempty"`
	ObjectName   string         `json:"objectName,omitempty"`
	Result       string         `json:"result"`
	ResultCode   int            `json:"resultCode"`
	LatencyMS    int            `json:"latencyMs"`
	ClientIP     string         `json:"clientIp,omitempty"`
	UserAgent    string         `json:"userAgent,omitempty"`
	BodyHash     string         `json:"bodyHash,omitempty"`
	Detail       map[string]any `json:"detail,omitempty"`
	PrevHash     string         `json:"prevHash,omitempty"`
	Hash         string         `json:"hash"`
}

// AuditRecord 是写入后的审计记录标识。
type AuditRecord struct {
	ID         int64
	OccurredAt time.Time
	PrevHash   string
	Hash       string
}

// Settings 是平台配置项。
const (
	SettingAuditRetentionDays = "audit.retention_days"
	SettingMinK8sVersion      = "platform.min_k8s_version"
)
