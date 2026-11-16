## ADDED Requirements

### Requirement: kubeconfig 文件引用拒绝

系统 MUST 拒绝 kubeconfig 中通过文件路径引用凭据或证书的字段（`tokenFile`、`certificate-authority`、`client-certificate`、`client-key`），只接受内联数据，避免服务端读取任意本地文件。

#### Scenario: 提交 tokenFile

- **WHEN** 管理员提交包含 `tokenFile: /var/run/secrets/...` 的 kubeconfig
- **THEN** 系统 SHALL 返回 400，说明不支持文件路径凭据，且不发起连接

#### Scenario: 提交内联 token

- **WHEN** 管理员提交包含内联 `token` 的 kubeconfig
- **THEN** 系统 SHALL 正常解析并执行接入探测

### Requirement: 集群状态消息脱敏

对外暴露的集群状态消息 SHALL 只描述错误分类（不可达、凭据无效、证书校验失败），MUST NOT 包含内部地址、DNS 名或原始网络错误。原始错误 SHALL 写入服务端日志。

#### Scenario: 探测失败

- **WHEN** 健康探测因 `dial tcp 10.0.0.5:6443` 失败
- **THEN** 集群详情 SHALL 显示「集群不可达」，不得显示目标 IP 与端口