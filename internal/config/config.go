// Package config 负责进程配置的加载与校验（环境变量 + 配置文件）。
package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是进程级配置。优先级：环境变量 > YAML 文件 > 默认值。
type Config struct {
	// Addr 是 HTTP 监听地址，默认 :8080。
	Addr string `yaml:"addr"`
	// LogLevel 是日志级别（debug/info/warn/error），默认 info。
	LogLevel string `yaml:"log_level"`
	// AllowedOrigins 是 CORS 白名单；为空表示仅同源。
	AllowedOrigins []string `yaml:"allowed_origins"`
	// DevKubeconfig 非空时启用开发模式：直接以该 kubeconfig 接入一个本地集群，
	// 跳过 OIDC 与数据库。生产部署不设置该项。
	DevKubeconfig string `yaml:"dev_kubeconfig"`
	// DevClusterName 是开发模式下集群的展示名。
	DevClusterName string `yaml:"dev_cluster_name"`
	// DevUser 非空时以该用户身份发起 Impersonation 请求（用于本地验证 RBAC）。
	DevUser string `yaml:"dev_user"`
	// DatabaseURL 是平台元数据库连接串；为空时不连接数据库（仅 dev 模式可用）。
	DatabaseURL string `yaml:"database_url"`
	// MasterKey / MasterKeyVersion 是凭据加密的当前主密钥（base64 编码的 32 字节）。
	// 主密钥来自环境变量 / KMS / Vault，绝不入库（design D7）。
	MasterKey        string `yaml:"master_key"`
	MasterKeyVersion int    `yaml:"master_key_version"`
	// PreviousMasterKey / PreviousMasterKeyVersion 用于密钥轮换期间解密旧密文。
	PreviousMasterKey        string `yaml:"previous_master_key"`
	PreviousMasterKeyVersion int    `yaml:"previous_master_key_version"`
	// OIDC 登录配置。未配置 dev_kubeconfig 时至少需要 issuer/client_id/redirect_url。
	OIDCIssuer       string   `yaml:"oidc_issuer"`
	OIDCClientID     string   `yaml:"oidc_client_id"`
	OIDCClientSecret string   `yaml:"oidc_client_secret"`
	OIDCRedirectURL  string   `yaml:"oidc_redirect_url"`
	OIDCScopes       []string `yaml:"oidc_scopes"`
	// BootstrapToken 用于创建首个平台管理员；创建成功后接口自动失效。
	BootstrapToken string `yaml:"bootstrap_token"`
	// CookieInsecure 仅供本地 HTTP 调试；生产必须为 false（Secure Cookie）。
	CookieInsecure bool `yaml:"cookie_insecure"`
	// SessionTTLHours 是平台会话有效期，默认 8 小时。
	SessionTTLHours int `yaml:"session_ttl_hours"`
	// MaxSessionsPerUser 是同一用户并发会话上限，超出时最早会话失效。
	MaxSessionsPerUser int `yaml:"max_sessions_per_user"`
}

// Default 返回内置默认配置。
func Default() Config {
	return Config{
		Addr:               ":8080",
		LogLevel:           "info",
		DevClusterName:     "local",
		MasterKeyVersion:   1,
		SessionTTLHours:    8,
		MaxSessionsPerUser: 5,
	}
}

// Load 按优先级合并配置：先读 KUBEDECK_CONFIG 指向的 YAML（可选），再用环境变量覆盖。
// 加载完成后执行 Validate；非法配置直接返回错误，由调用方阻止启动。
func Load() (Config, error) {
	cfg := Default()

	if path := os.Getenv("KUBEDECK_CONFIG"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %q: %w", path, err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true) // 未知键视为错误，避免拼写错误被静默忽略
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("parse config %q: %w", path, err)
		}
	}

	if v := os.Getenv("KUBEDECK_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("KUBEDECK_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("KUBEDECK_ALLOWED_ORIGINS"); v != "" {
		cfg.AllowedOrigins = nil
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
	}
	if v := os.Getenv("KUBEDECK_DEV_KUBECONFIG"); v != "" {
		cfg.DevKubeconfig = v
	}
	if v := os.Getenv("KUBEDECK_DEV_CLUSTER_NAME"); v != "" {
		cfg.DevClusterName = v
	}
	if v := os.Getenv("KUBEDECK_DEV_USER"); v != "" {
		cfg.DevUser = v
	}
	if v := os.Getenv("KUBEDECK_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("KUBEDECK_MASTER_KEY"); v != "" {
		cfg.MasterKey = v
	}
	if v := os.Getenv("KUBEDECK_MASTER_KEY_VERSION"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return cfg, fmt.Errorf("KUBEDECK_MASTER_KEY_VERSION must be a positive integer, got %q", v)
		}
		cfg.MasterKeyVersion = n
	}
	if v := os.Getenv("KUBEDECK_PREVIOUS_MASTER_KEY"); v != "" {
		cfg.PreviousMasterKey = v
	}
	if v := os.Getenv("KUBEDECK_PREVIOUS_MASTER_KEY_VERSION"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return cfg, fmt.Errorf("KUBEDECK_PREVIOUS_MASTER_KEY_VERSION must be a positive integer, got %q", v)
		}
		cfg.PreviousMasterKeyVersion = n
	}
	if v := os.Getenv("KUBEDECK_OIDC_ISSUER"); v != "" {
		cfg.OIDCIssuer = v
	}
	if v := os.Getenv("KUBEDECK_OIDC_CLIENT_ID"); v != "" {
		cfg.OIDCClientID = v
	}
	if v := os.Getenv("KUBEDECK_OIDC_CLIENT_SECRET"); v != "" {
		cfg.OIDCClientSecret = v
	}
	if v := os.Getenv("KUBEDECK_OIDC_REDIRECT_URL"); v != "" {
		cfg.OIDCRedirectURL = v
	}
	if v := os.Getenv("KUBEDECK_OIDC_SCOPES"); v != "" {
		cfg.OIDCScopes = nil
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				cfg.OIDCScopes = append(cfg.OIDCScopes, s)
			}
		}
	}
	if v := os.Getenv("KUBEDECK_BOOTSTRAP_TOKEN"); v != "" {
		cfg.BootstrapToken = v
	}
	if v := os.Getenv("KUBEDECK_COOKIE_INSECURE"); v != "" {
		cfg.CookieInsecure, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("KUBEDECK_SESSION_TTL_HOURS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return cfg, fmt.Errorf("KUBEDECK_SESSION_TTL_HOURS must be a positive integer, got %q", v)
		}
		cfg.SessionTTLHours = n
	}
	if v := os.Getenv("KUBEDECK_MAX_SESSIONS_PER_USER"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return cfg, fmt.Errorf("KUBEDECK_MAX_SESSIONS_PER_USER must be a positive integer, got %q", v)
		}
		cfg.MaxSessionsPerUser = n
	}

	cfg.Addr = strings.TrimSpace(cfg.Addr)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	cfg.DevClusterName = strings.TrimSpace(cfg.DevClusterName)
	cfg.OIDCIssuer = strings.TrimSpace(cfg.OIDCIssuer)
	cfg.OIDCClientID = strings.TrimSpace(cfg.OIDCClientID)
	cfg.OIDCRedirectURL = strings.TrimSpace(cfg.OIDCRedirectURL)

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate 在监听前校验配置，错误信息必须能定位到具体字段。
func (c Config) Validate() error {
	if c.Addr == "" {
		return errors.New("addr: must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level: unsupported %q (want debug|info|warn|error)", c.LogLevel)
	}
	for _, origin := range c.AllowedOrigins {
		if strings.TrimSpace(origin) == "*" {
			return errors.New("allowed_origins: wildcard \"*\" is not allowed because credentials are enabled")
		}
	}
	if c.DevKubeconfig != "" && c.DevClusterName == "" {
		return errors.New("dev_cluster_name: required when dev_kubeconfig is set")
	}
	// 安全底线：没有显式开发模式时，必须配置 OIDC，不能以无认证状态启动。
	if c.DevKubeconfig == "" {
		if c.OIDCIssuer == "" {
			return errors.New("oidc_issuer: required unless dev_kubeconfig is set")
		}
		if c.OIDCClientID == "" {
			return errors.New("oidc_client_id: required when oidc_issuer is set")
		}
		if c.OIDCRedirectURL == "" {
			return errors.New("oidc_redirect_url: required when oidc_issuer is set")
		}
	}
	if c.BootstrapToken != "" && len(c.BootstrapToken) < 16 {
		return errors.New("bootstrap_token: must be at least 16 characters")
	}
	if c.SessionTTLHours < 1 {
		return errors.New("session_ttl_hours: must be >= 1")
	}
	if c.MaxSessionsPerUser < 1 {
		return errors.New("max_sessions_per_user: must be >= 1")
	}

	if c.MasterKeyVersion < 1 {
		return errors.New("master_key_version: must be >= 1")
	}
	if c.MasterKey == "" && c.MasterKeyVersion != 1 {
		return errors.New("master_key_version: set without master_key")
	}
	if c.MasterKey != "" {
		if err := validateMasterKey(c.MasterKey); err != nil {
			return fmt.Errorf("master_key: %w", err)
		}
	}
	if c.PreviousMasterKey != "" {
		if err := validateMasterKey(c.PreviousMasterKey); err != nil {
			return fmt.Errorf("previous_master_key: %w", err)
		}
		if c.PreviousMasterKeyVersion < 1 {
			return errors.New("previous_master_key_version: required when previous_master_key is set")
		}
		if c.PreviousMasterKeyVersion == c.MasterKeyVersion {
			return errors.New("previous_master_key_version: must differ from master_key_version")
		}
	} else if c.PreviousMasterKeyVersion != 0 {
		return errors.New("previous_master_key: required when previous_master_key_version is set")
	}
	return nil
}

func validateMasterKey(value string) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return errors.New("must be base64 encoded")
	}
	if len(key) != 32 {
		return fmt.Errorf("must decode to 32 bytes, got %d", len(key))
	}
	return nil
}
