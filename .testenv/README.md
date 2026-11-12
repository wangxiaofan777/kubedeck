# .testenv

本地测试集群的凭据目录，**内容不入库**（见 `.gitignore`）。

`k3s.yaml` 由 k3s 生成，包含客户端证书与私钥，生成方式：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc \
  "mkdir -p /mnt/d/AI_Coding/kubedeck/.testenv && \
   install -m 0644 /etc/rancher/k3s/k3s.yaml /mnt/d/AI_Coding/kubedeck/.testenv/k3s.yaml"
```

> 该文件等价于集群管理员凭据，不要提交、不要分享、不要贴进工单或聊天记录。

完整环境搭建步骤见 [docs/local-environment.md](../docs/local-environment.md)。