// Package config 负责进程配置的加载与校验（环境变量 + 配置文件）。
package config

import (
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
}

// Default 返回内置默认配置。
func Default() Config {
	return Config{
		Addr:           ":8080",
		LogLevel:       "info",
		DevClusterName: "local",

		MasterKeyVersion: 1,
	}
}

// Load 按优先级合并配置：先读 KUBEDECK_CONFIG 指向的 YAML（可选），再用环境变量覆盖。
func Load() (Config, error) {
	cfg := Default()

	if path := os.Getenv("KUBEDECK_CONFIG"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %q: %w", path, err)
		}
	}

	if v := os.Getenv("KUBEDECK_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("KUBEDECK_LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToLower(v)
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

	return cfg, nil
}
