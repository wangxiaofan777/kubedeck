package kube

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// IngestInput 是接入集群时客户端提交的凭据，二选一：
// kubeconfig 文本，或 server + token（可选 CA）。
type IngestInput struct {
	Kubeconfig            string `json:"kubeconfig,omitempty"`
	Server                string `json:"server,omitempty"`
	Token                 string `json:"token,omitempty"`
	CAData                string `json:"caData,omitempty"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify,omitempty"`
}

// ParsedCredential 是解析后的集群连接信息。Plaintext 是用于加密存储的原始凭据，
// 绝不能出现在日志或 API 响应中。
type ParsedCredential struct {
	Config    *rest.Config
	Server    string
	AuthType  string
	Plaintext []byte
}

// ParseCredential 解析并校验接入凭据。
// 安全约束：只接受 https；禁止链路本地地址；拒绝 kubeconfig 中的 exec/auth-provider，
// 避免用户提交的配置在服务端执行任意命令。
func ParseCredential(in IngestInput) (ParsedCredential, error) {
	if strings.TrimSpace(in.Kubeconfig) != "" {
		return parseKubeconfig(in.Kubeconfig)
	}
	if strings.TrimSpace(in.Server) == "" || strings.TrimSpace(in.Token) == "" {
		return ParsedCredential{}, errors.New("需要提供 kubeconfig，或 server + token")
	}
	if err := ValidateServer(in.Server); err != nil {
		return ParsedCredential{}, err
	}
	cfg := &rest.Config{
		Host:            strings.TrimSpace(in.Server),
		BearerToken:     strings.TrimSpace(in.Token),
		BearerTokenFile: "",
		TLSClientConfig: rest.TLSClientConfig{Insecure: in.InsecureSkipTLSVerify},
	}
	if in.CAData != "" {
		ca := []byte(strings.TrimSpace(in.CAData))
		if !x509.NewCertPool().AppendCertsFromPEM(ca) {
			return ParsedCredential{}, errors.New("caData 不是合法的 PEM 证书")
		}
		cfg.TLSClientConfig.CAData = ca
	}
	if cfg.TLSClientConfig.Insecure {
		cfg.TLSClientConfig.CAData = nil
	}
	stored, err := json.Marshal(IngestInput{
		Server:                cfg.Host,
		Token:                 in.Token,
		CAData:                in.CAData,
		InsecureSkipTLSVerify: in.InsecureSkipTLSVerify,
	})
	if err != nil {
		return ParsedCredential{}, fmt.Errorf("encode credential: %w", err)
	}
	return ParsedCredential{Config: cfg, Server: cfg.Host, AuthType: "token", Plaintext: stored}, nil
}

func parseKubeconfig(raw string) (ParsedCredential, error) {
	kc, err := clientcmd.Load([]byte(raw))
	if err != nil {
		return ParsedCredential{}, fmt.Errorf("kubeconfig 解析失败: %w", err)
	}
	contextName := kc.CurrentContext
	if contextName == "" {
		return ParsedCredential{}, errors.New("kubeconfig 缺少 current-context")
	}
	kctx, ok := kc.Contexts[contextName]
	if !ok {
		return ParsedCredential{}, fmt.Errorf("kubeconfig 中不存在 context %q", contextName)
	}
	cluster, ok := kc.Clusters[kctx.Cluster]
	if !ok {
		return ParsedCredential{}, fmt.Errorf("kubeconfig 中不存在 cluster %q", kctx.Cluster)
	}
	auth, ok := kc.AuthInfos[kctx.AuthInfo]
	if !ok {
		return ParsedCredential{}, fmt.Errorf("kubeconfig 中不存在 user %q", kctx.AuthInfo)
	}
	if auth.Exec != nil || auth.AuthProvider != nil {
		return ParsedCredential{}, errors.New("kubeconfig 不支持 exec 或 auth-provider 凭据")
	}
	if err := ValidateServer(cluster.Server); err != nil {
		return ParsedCredential{}, err
	}

	cfg := &rest.Config{Host: cluster.Server}
	cfg.TLSClientConfig = rest.TLSClientConfig{
		Insecure:   cluster.InsecureSkipTLSVerify,
		CAData:     cluster.CertificateAuthorityData,
		CAFile:     cluster.CertificateAuthority,
		ServerName: cluster.TLSServerName,
	}
	switch {
	case auth.Token != "":
		cfg.BearerToken = auth.Token
	case auth.TokenFile != "":
		cfg.BearerTokenFile = auth.TokenFile
	case len(auth.ClientCertificateData) > 0 && len(auth.ClientKeyData) > 0:
		if _, err := tls.X509KeyPair(auth.ClientCertificateData, auth.ClientKeyData); err != nil {
			return ParsedCredential{}, fmt.Errorf("客户端证书无效: %w", err)
		}
		cfg.TLSClientConfig.CertData = auth.ClientCertificateData
		cfg.TLSClientConfig.KeyData = auth.ClientKeyData
	case auth.Username != "":
		cfg.Username = auth.Username
		cfg.Password = auth.Password
	default:
		return ParsedCredential{}, errors.New("kubeconfig 中没有可用的凭据（token 或客户端证书）")
	}
	if cfg.TLSClientConfig.CAFile != "" {
		// 文件路径来自用户输入，禁止读取服务端本地文件。
		return ParsedCredential{}, errors.New("kubeconfig 不支持证书文件路径，请使用 certificate-authority-data")
	}
	return ParsedCredential{Config: cfg, Server: cluster.Server, AuthType: authType(auth), Plaintext: []byte(raw)}, nil
}

func authType(auth *clientcmdapi.AuthInfo) string {
	if len(auth.ClientCertificateData) > 0 {
		return "client-cert"
	}
	return "kubeconfig"
}

// ValidateServer 只允许 https，并拒绝链路本地地址段 169.254.0.0/16。
func ValidateServer(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("server 必须是合法的 URL")
	}
	if u.Scheme != "https" {
		return errors.New("server 必须使用 https://")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return errors.New("server 不能指向链路本地地址段 169.254.0.0/16")
		}
	} else if strings.HasPrefix(host, "169.254.") {
		return errors.New("server 不能指向链路本地地址段 169.254.0.0/16")
	}
	return nil
}

// CheckMinVersion 校验集群版本不低于平台声明的最低版本。
func CheckMinVersion(serverVersion, minVersion string) error {
	if serverVersion == "" {
		return errors.New("无法读取集群版本")
	}
	got, err := version.ParseGeneric(serverVersion)
	if err != nil {
		return fmt.Errorf("无法解析集群版本 %q: %w", serverVersion, err)
	}
	min, err := version.ParseGeneric(minVersion)
	if err != nil {
		return fmt.Errorf("最低支持版本配置无效 %q: %w", minVersion, err)
	}
	if got.LessThan(min) {
		return fmt.Errorf("集群版本 %s 低于最低支持版本 %s", serverVersion, minVersion)
	}
	return nil
}

// ParseStored 用数据库中的明文（解密后）重建连接配置。
// authType 为 token 时明文是 IngestInput 的 JSON；其余为 kubeconfig 文本。
func ParseStored(authType string, plaintext []byte) (ParsedCredential, error) {
	if authType == "token" {
		var in IngestInput
		if err := json.Unmarshal(plaintext, &in); err != nil {
			return ParsedCredential{}, fmt.Errorf("decode stored token credential: %w", err)
		}
		return ParseCredential(in)
	}
	return ParseCredential(IngestInput{Kubeconfig: string(plaintext)})
}
