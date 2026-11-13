import type { APIResource, GVR, ResourceItem } from '../../lib/api'

export interface ResourceType {
  key: string
  label: string
  kind: string
  group: '工作负载' | '网络' | '配置' | '集群'
  /** core 组用空串，API 层会转换为路径中的 "_"。 */
  gvr: GVR
  namespaced: boolean
  /** 是否允许在控制台新建；Events/Nodes 由控制器或 kubelet 创建，默认不提供入口。 */
  creatable?: boolean
}

export const RESOURCE_TYPES: ResourceType[] = [
  {
    key: 'pods',
    label: 'Pods',
    kind: 'Pod',
    group: '工作负载',
    gvr: { group: '', version: 'v1', resource: 'pods' },
    namespaced: true,
  },
  {
    key: 'deployments',
    label: 'Deployments',
    kind: 'Deployment',
    group: '工作负载',
    gvr: { group: 'apps', version: 'v1', resource: 'deployments' },
    namespaced: true,
  },
  {
    key: 'statefulsets',
    label: 'StatefulSets',
    kind: 'StatefulSet',
    group: '工作负载',
    gvr: { group: 'apps', version: 'v1', resource: 'statefulsets' },
    namespaced: true,
  },
  {
    key: 'daemonsets',
    label: 'DaemonSets',
    kind: 'DaemonSet',
    group: '工作负载',
    gvr: { group: 'apps', version: 'v1', resource: 'daemonsets' },
    namespaced: true,
  },
  {
    key: 'jobs',
    label: 'Jobs',
    kind: 'Job',
    group: '工作负载',
    gvr: { group: 'batch', version: 'v1', resource: 'jobs' },
    namespaced: true,
  },
  {
    key: 'cronjobs',
    label: 'CronJobs',
    kind: 'CronJob',
    group: '工作负载',
    gvr: { group: 'batch', version: 'v1', resource: 'cronjobs' },
    namespaced: true,
  },
  {
    key: 'services',
    label: 'Services',
    kind: 'Service',
    group: '网络',
    gvr: { group: '', version: 'v1', resource: 'services' },
    namespaced: true,
  },
  {
    key: 'ingresses',
    label: 'Ingresses',
    kind: 'Ingress',
    group: '网络',
    gvr: { group: 'networking.k8s.io', version: 'v1', resource: 'ingresses' },
    namespaced: true,
  },
  {
    key: 'configmaps',
    label: 'ConfigMaps',
    kind: 'ConfigMap',
    group: '配置',
    gvr: { group: '', version: 'v1', resource: 'configmaps' },
    namespaced: true,
  },
  {
    key: 'secrets',
    label: 'Secrets',
    kind: 'Secret',
    group: '配置',
    gvr: { group: '', version: 'v1', resource: 'secrets' },
    namespaced: true,
  },
  {
    key: 'nodes',
    label: 'Nodes',
    kind: 'Node',
    group: '集群',
    gvr: { group: '', version: 'v1', resource: 'nodes' },
    namespaced: false,
    creatable: false,
  },
  {
    key: 'namespaces',
    label: 'Namespaces',
    kind: 'Namespace',
    group: '集群',
    gvr: { group: '', version: 'v1', resource: 'namespaces' },
    namespaced: false,
  },
  {
    key: 'events',
    label: 'Events',
    kind: 'Event',
    group: '集群',
    gvr: { group: '', version: 'v1', resource: 'events' },
    namespaced: true,
    creatable: false,
  },
]

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

function asNumber(value: unknown, fallback = 0): number {
  return typeof value === 'number' ? value : fallback
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

export function statusOf(item: ResourceItem, type: ResourceType): string {
  const status = asRecord(item.status)
  const spec = asRecord(item.spec)

  switch (type.key) {
    case 'pods': {
      const phase = asString(status.phase) || 'Unknown'
      const containers = Array.isArray(status.containerStatuses)
        ? (status.containerStatuses as { ready?: boolean }[])
        : []
      const ready = containers.filter((c) => c.ready).length
      return containers.length > 0 ? `${phase} ${ready}/${containers.length}` : phase
    }
    case 'deployments':
    case 'statefulsets':
      return `${asNumber(status.readyReplicas)}/${asNumber(spec.replicas, 1)}`
    case 'daemonsets':
      return `${asNumber(status.numberReady)}/${asNumber(status.desiredNumberScheduled)}`
    case 'services':
      return asString(spec.type) || '-'
    case 'nodes': {
      const conditions = Array.isArray(status.conditions)
        ? (status.conditions as { type: string; status: string }[])
        : []
      return conditions.find((c) => c.type === 'Ready')?.status === 'True' ? 'Ready' : 'NotReady'
    }
    case 'jobs': {
      if (asNumber(status.succeeded) > 0) return `Succeeded ${asNumber(status.succeeded)}`
      if (asNumber(status.failed) > 0) return `Failed ${asNumber(status.failed)}`
      return asNumber(status.active) > 0 ? `Active ${asNumber(status.active)}` : 'Pending'
    }
    default:
      return '-'
  }
}

export function resolveType(key: string | undefined): ResourceType | undefined {
  return RESOURCE_TYPES.find((t) => t.key === key)
}

export function isCreatable(type: ResourceType): boolean {
  return type.creatable !== false
}

/** createTemplate 只预填身份字段，不生成看似可提交但实际无效的 spec。 */
export function createTemplate(type: ResourceType, namespace: string): string {
  const apiVersion = type.gvr.group
    ? `${type.gvr.group}/${type.gvr.version}`
    : type.gvr.version
  const lines = [`apiVersion: ${apiVersion}`, `kind: ${type.kind}`, 'metadata:', '  name: ""']
  if (type.namespaced) lines.push(`  namespace: ${namespace}`)
  return `${lines.join('\n')}\n`
}

export interface ParsedApiVersion {
  group: string
  version: string
}

/** 解析 apiVersion：`v1` 归入核心组，`apps/v1` 拆成 group+version。 */
export function parseApiVersion(apiVersion: string): ParsedApiVersion | undefined {
  if (!apiVersion) return undefined
  const parts = apiVersion.split('/')
  if (parts.length === 1) return parts[0] ? { group: '', version: parts[0] } : undefined
  if (parts.length === 2 && parts[0] && parts[1]) return { group: parts[0], version: parts[1] }
  return undefined
}

/** CreateTarget 是创建弹窗根据 YAML 解析出的实际目标。 */
export interface CreateTarget {
  gvr: GVR
  kind: string
  namespaced: boolean
  /** 控制台内置页面类型 key；未内置时为 undefined。 */
  pageKey?: string
}

export type CreateTargetResolution =
  | { ok: true; target: CreateTarget }
  | { ok: false; reason: string }

/** 资源发现中按 apiVersion + kind 找到可直接创建的目标。 */
export function resolveCreateTarget(
  resources: APIResource[],
  apiVersion: string,
  kind: string,
): CreateTargetResolution {
  const parsed = parseApiVersion(apiVersion)
  if (!parsed) return { ok: false, reason: `apiVersion 格式不合法：${apiVersion}` }

  const candidates = resources.filter((r) => r.kind === kind && r.group === parsed.group)
  const match =
    candidates.find((r) => r.version === parsed.version) ??
    candidates.find((r) => r.verbs.includes('create')) ??
    candidates[0]
  if (!match) return { ok: false, reason: `集群中未发现 ${apiVersion} 的 ${kind}` }
  if (!match.verbs.includes('create')) {
    return { ok: false, reason: `资源类型 ${kind} 不支持创建` }
  }

  const page = RESOURCE_TYPES.find(
    (t) =>
      t.gvr.group === match.group &&
      t.gvr.version === match.version &&
      t.gvr.resource === match.resource,
  )
  return {
    ok: true,
    target: {
      gvr: { group: match.group, version: match.version, resource: match.resource },
      kind: match.kind,
      namespaced: match.namespaced,
      pageKey: page?.key,
    },
  }
}

/** YAML 声明的类型是否就是当前页面类型。 */
export function matchesResourceType(apiVersion: string, kind: string, type: ResourceType): boolean {
  const parsed = parseApiVersion(apiVersion)
  if (!parsed) return false
  return (
    kind === type.kind &&
    parsed.group === type.gvr.group &&
    parsed.version === type.gvr.version
  )
}

export function isWorkload(type: ResourceType): boolean {
  return ['deployments', 'statefulsets', 'daemonsets', 'replicasets'].includes(type.key)
}

export function supportsScale(type: ResourceType): boolean {
  return ['deployments', 'statefulsets', 'replicasets'].includes(type.key)
}

export function supportsRestart(type: ResourceType): boolean {
  return ['deployments', 'statefulsets', 'daemonsets'].includes(type.key)
}

export function supportsRollback(type: ResourceType): boolean {
  return type.key === 'deployments'
}

export function statusColor(status: string): string {
  const lower = status.toLowerCase()
  if (/(running|ready|active|succeeded|healthy|available)/.test(lower)) return 'green'
  if (/(pending|progressing|containercreating|terminating|updating)/.test(lower)) return 'blue'
  if (/(failed|error|crashloopbackoff|notready|unreachable|degraded|imagepullbackoff)/.test(lower))
    return 'red'
  return 'default'
}

export function isCrashLoop(item: ResourceItem): boolean {
  const status = asRecord(item.status)
  const containers = Array.isArray(status.containerStatuses)
    ? (status.containerStatuses as { state?: { waiting?: { reason?: string } } }[])
    : []
  return containers.some((c) => c.state?.waiting?.reason === 'CrashLoopBackOff')
}
