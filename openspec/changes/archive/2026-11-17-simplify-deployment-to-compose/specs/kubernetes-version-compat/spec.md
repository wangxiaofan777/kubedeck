## REMOVED Requirements

### Requirement: 兼容的部署清单

**Reason**: 部署方式收敛为 Docker Compose，仓库不再维护 Helm Chart，Chart 的版本条件渲染行为不再适用。

**Migration**: 已使用 Helm 的部署方改用 `docker compose` 部署，或自行维护清单；平台对目标集群 1.18–1.37 的兼容承诺不变，仍由“支持的 Kubernetes 版本范围”和“遗留 API 版本协商”保证。