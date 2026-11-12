// Package cluster 负责集群登记、凭据轮换与健康探测编排。
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kubedeck/kubedeck/internal/crypto"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
)

// 错误分类，供 API 层映射为稳定错误码。
var (
	ErrNoMasterKey  = errors.New("cluster: master key is not configured")
	ErrUnreachable  = errors.New("cluster: unreachable during probe")
	ErrNotSupported = errors.New("cluster: unsupported credential")
)

const (
	defaultMinVersion = "1.34"
	probeTimeout      = 6 * time.Second
)

// CreateRequest 是接入集群的输入。
type CreateRequest struct {
	Name        string
	DisplayName string
	Labels      map[string]string
	Credential  kube.IngestInput
	CreatedBy   *int64
}

// Store 是集群管理所需的最小仓储能力，便于在测试中替换。
type Store interface {
	Clusters(ctx context.Context) ([]domain.Cluster, error)
	ClusterByName(ctx context.Context, name string) (domain.Cluster, error)
	ClusterByID(ctx context.Context, id int64) (domain.Cluster, error)
	CredentialByClusterID(ctx context.Context, clusterID int64) (domain.Credential, error)
	CreateClusterWithCredential(ctx context.Context, c domain.Cluster, cred domain.Credential) (domain.Cluster, error)
	ReplaceCredential(ctx context.Context, c domain.Credential) error
	UpdateClusterConnection(ctx context.Context, id int64, server, version, status, message string) error
	UpdateClusterStatus(ctx context.Context, id int64, status domain.ClusterStatus, message, version string, seenAt time.Time) error
	RenameCluster(ctx context.Context, id int64, displayName string, labels map[string]string) error
	DeleteCluster(ctx context.Context, id int64) error
	GetSetting(ctx context.Context, key string) (json.RawMessage, error)
}

// Manager 维护数据库登记与内存注册表的一致性。
type Manager struct {
	registry *kube.Registry
	store    Store
	ring     *crypto.Ring
	logger   *slog.Logger

	mu  sync.Mutex
	ids map[string]int64
}

// NewManager 创建集群管理器。st 可以为 nil（仅开发模式）。
func NewManager(registry *kube.Registry, st Store, ring *crypto.Ring, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		registry: registry,
		store:    st,
		ring:     ring,
		logger:   logger,
		ids:      make(map[string]int64),
	}
}

// Registry 返回底层注册表。
func (m *Manager) Registry() *kube.Registry { return m.registry }

// Load 从数据库恢复已登记集群；单个集群损坏不影响其他集群。
func (m *Manager) Load(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	clusters, err := m.store.Clusters(ctx)
	if err != nil {
		return fmt.Errorf("load clusters: %w", err)
	}
	for _, c := range clusters {
		dc, err := m.buildCluster(ctx, c)
		if err != nil {
			m.logger.Error("skip cluster: cannot restore credential",
				"cluster", c.Name, "err", err)
			continue
		}
		dc.Seed(string(c.Status), c.ServerVersion, c.Message)
		m.registry.Add(dc)
		m.setID(c.Name, c.ID)
	}
	m.logger.Info("clusters loaded", "count", len(clusters))
	return nil
}

func (m *Manager) buildCluster(ctx context.Context, c domain.Cluster) (*kube.DirectCluster, error) {
	if m.ring == nil {
		return nil, ErrNoMasterKey
	}
	cred, err := m.store.CredentialByClusterID(ctx, c.ID)
	if err != nil {
		return nil, fmt.Errorf("read credential: %w", err)
	}
	plaintext, err := m.ring.Open(crypto.Sealed{
		Nonce: cred.Nonce, Ciphertext: cred.Ciphertext, KeyVersion: cred.KeyVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	parsed, err := kube.ParseStored(cred.AuthType, plaintext)
	if err != nil {
		return nil, err
	}
	return kube.NewDirectCluster(c.Name, parsed.Config)
}

// Create 接入集群：解析凭据 → 5 秒探测 → 版本校验 → 加密落库 → 注册。
func (m *Manager) Create(ctx context.Context, req CreateRequest) (domain.Cluster, error) {
	if m.store == nil {
		return domain.Cluster{}, errors.New("cluster: database is required")
	}
	if m.ring == nil {
		return domain.Cluster{}, ErrNoMasterKey
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return domain.Cluster{}, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}

	parsed, err := kube.ParseCredential(req.Credential)
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	probeCluster, err := kube.NewDirectCluster(req.Name, parsed.Config)
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	info, err := probe(ctx, probeCluster)
	if err != nil {
		return domain.Cluster{}, err
	}
	minVersion, err := m.minVersion(ctx)
	if err != nil {
		return domain.Cluster{}, err
	}
	if err := kube.CheckMinVersion(info.Version, minVersion); err != nil {
		return domain.Cluster{}, fmt.Errorf("%w: %v", ErrNotSupported, err)
	}

	sealed, err := m.ring.Seal(parsed.Plaintext)
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("encrypt credential: %w", err)
	}
	cluster, err := m.store.CreateClusterWithCredential(ctx, domain.Cluster{
		Name:          req.Name,
		DisplayName:   req.DisplayName,
		Server:        parsed.Server,
		Transport:     "direct",
		AuthMode:      "impersonation",
		ServerVersion: info.Version,
		Status:        domain.ClusterHealthy,
		Labels:        req.Labels,
		CreatedBy:     req.CreatedBy,
	}, domain.Credential{
		Ciphertext:  sealed.Ciphertext,
		Nonce:       sealed.Nonce,
		KeyVersion:  sealed.KeyVersion,
		AuthType:    parsed.AuthType,
		Fingerprint: crypto.Fingerprint(parsed.Plaintext),
	})
	if err != nil {
		return domain.Cluster{}, err
	}

	probeCluster.Seed(string(cluster.Status), cluster.ServerVersion, cluster.Message)
	m.registry.Add(probeCluster)
	m.setID(cluster.Name, cluster.ID)
	return cluster, nil
}

// Rotate 轮换凭据：新凭据探测成功后才覆盖旧凭据，旧凭据随即失效。
func (m *Manager) Rotate(ctx context.Context, name string, in kube.IngestInput) (domain.Cluster, error) {
	if m.store == nil || m.ring == nil {
		return domain.Cluster{}, ErrNoMasterKey
	}
	existing, err := m.store.ClusterByName(ctx, name)
	if err != nil {
		return domain.Cluster{}, err
	}
	parsed, err := kube.ParseCredential(in)
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	probeCluster, err := kube.NewDirectCluster(name, parsed.Config)
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	info, err := probe(ctx, probeCluster)
	if err != nil {
		return domain.Cluster{}, err
	}
	minVersion, err := m.minVersion(ctx)
	if err != nil {
		return domain.Cluster{}, err
	}
	if err := kube.CheckMinVersion(info.Version, minVersion); err != nil {
		return domain.Cluster{}, fmt.Errorf("%w: %v", ErrNotSupported, err)
	}

	sealed, err := m.ring.Seal(parsed.Plaintext)
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("encrypt credential: %w", err)
	}
	if err := m.store.ReplaceCredential(ctx, domain.Credential{
		ClusterID:   existing.ID,
		Ciphertext:  sealed.Ciphertext,
		Nonce:       sealed.Nonce,
		KeyVersion:  sealed.KeyVersion,
		AuthType:    parsed.AuthType,
		Fingerprint: crypto.Fingerprint(parsed.Plaintext),
	}); err != nil {
		return domain.Cluster{}, err
	}
	if err := m.store.UpdateClusterConnection(ctx, existing.ID, parsed.Server,
		info.Version, string(domain.ClusterHealthy), ""); err != nil {
		return domain.Cluster{}, err
	}

	probeCluster.Seed(string(domain.ClusterHealthy), info.Version, "")
	m.registry.Add(probeCluster)
	m.setID(name, existing.ID)
	return m.store.ClusterByID(ctx, existing.ID)
}

// UpdateMeta 修改展示名与标签。
func (m *Manager) UpdateMeta(ctx context.Context, name, displayName string, labels map[string]string) (domain.Cluster, error) {
	if m.store == nil {
		return domain.Cluster{}, errors.New("cluster: database is required")
	}
	existing, err := m.store.ClusterByName(ctx, name)
	if err != nil {
		return domain.Cluster{}, err
	}
	if err := m.store.RenameCluster(ctx, existing.ID, displayName, labels); err != nil {
		return domain.Cluster{}, err
	}
	return m.store.ClusterByID(ctx, existing.ID)
}

// Delete 移除集群登记并停止健康探测；不触碰目标集群资源。
func (m *Manager) Delete(ctx context.Context, name string) error {
	if m.store == nil {
		return errors.New("cluster: database is required")
	}
	existing, err := m.store.ClusterByName(ctx, name)
	if err != nil {
		return err
	}
	if err := m.store.DeleteCluster(ctx, existing.ID); err != nil {
		return err
	}
	m.registry.Remove(name)
	m.mu.Lock()
	delete(m.ids, name)
	m.mu.Unlock()
	return nil
}

// CredentialMeta 返回凭据指纹与轮换时间，不返回密文。
func (m *Manager) CredentialMeta(ctx context.Context, name string) (domain.Credential, error) {
	if m.store == nil {
		return domain.Credential{}, errors.New("cluster: database is required")
	}
	existing, err := m.store.ClusterByName(ctx, name)
	if err != nil {
		return domain.Credential{}, err
	}
	return m.store.CredentialByClusterID(ctx, existing.ID)
}

// ProbeAll 探测全部集群并持久化状态与版本。开发集群没有数据库 ID，只更新内存状态。
func (m *Manager) ProbeAll(ctx context.Context) {
	for _, access := range m.registry.All() {
		prober, ok := access.(interface {
			Probe(context.Context) kube.ClusterInfo
		})
		if !ok {
			continue
		}
		info := prober.Probe(ctx)
		m.mu.Lock()
		id, persisted := m.ids[access.Name()]
		m.mu.Unlock()
		if !persisted || m.store == nil {
			continue
		}
		if err := m.store.UpdateClusterStatus(ctx, id, domain.ClusterStatus(info.Status),
			info.Message, info.Version, time.Now().UTC()); err != nil {
			m.logger.Warn("persist cluster health", "cluster", access.Name(), "err", err)
		}
	}
}

// Info 返回单个集群的最近状态。
func (m *Manager) Info(ctx context.Context, name string) (kube.ClusterInfo, bool) {
	c, ok := m.registry.Get(name)
	if !ok {
		return kube.ClusterInfo{}, false
	}
	return c.Info(ctx), true
}

func (m *Manager) minVersion(ctx context.Context) (string, error) {
	if m.store == nil {
		return defaultMinVersion, nil
	}
	raw, err := m.store.GetSetting(ctx, domain.SettingMinK8sVersion)
	if errors.Is(err, domain.ErrNotFound) {
		return defaultMinVersion, nil
	}
	if err != nil {
		return "", fmt.Errorf("read minimum version: %w", err)
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil || v == "" {
		return defaultMinVersion, nil
	}
	return v, nil
}

func (m *Manager) setID(name string, id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ids[name] = id
}

func probe(ctx context.Context, c *kube.DirectCluster) (kube.ClusterInfo, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	info := c.Probe(probeCtx)
	if info.Status != kube.StatusHealthy {
		msg := info.Message
		if msg == "" {
			msg = "集群未通过就绪探测"
		}
		return info, fmt.Errorf("%w: %s", ErrUnreachable, msg)
	}
	return info, nil
}
