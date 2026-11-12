import type { ResourceItem } from './api'

export interface ResourceType {
  key: string
  label: string
  group: '工作负载' | '网络' | '配置' | '集群'
  /** [group, version, resource]，core 组用 "_" 占位 */
  path: [string, string, string]
  namespaced: boolean
}

export const RESOURCE_TYPES: ResourceType[] = [
  { key: 'pods', label: 'Pods', group: '工作负载', path: ['_', 'v1', 'pods'], namespaced: true },
  { key: 'deployments', label: 'Deployments', group: '工作负载', path: ['apps', 'v1', 'deployments'], namespaced: true },
  { key: 'statefulsets', label: 'StatefulSets', group: '工作负载', path: ['apps', 'v1', 'statefulsets'], namespaced: true },
  { key: 'daemonsets', label: 'DaemonSets', group: '工作负载', path: ['apps', 'v1', 'daemonsets'], namespaced: true },
  { key: 'jobs', label: 'Jobs', group: '工作负载', path: ['batch', 'v1', 'jobs'], namespaced: true },
  { key: 'cronjobs', label: 'CronJobs', group: '工作负载', path: ['batch', 'v1', 'cronjobs'], namespaced: true },
  { key: 'services', label: 'Services', group: '网络', path: ['_', 'v1', 'services'], namespaced: true },
  { key: 'ingresses', label: 'Ingresses', group: '网络', path: ['networking.k8s.io', 'v1', 'ingresses'], namespaced: true },
  { key: 'configmaps', label: 'ConfigMaps', group: '配置', path: ['_', 'v1', 'configmaps'], namespaced: true },
  { key: 'secrets', label: 'Secrets', group: '配置', path: ['_', 'v1', 'secrets'], namespaced: true },
  { key: 'nodes', label: 'Nodes', group: '集群', path: ['_', 'v1', 'nodes'], namespaced: false },
  { key: 'namespaces', label: 'Namespaces', group: '集群', path: ['_', 'v1', 'namespaces'], namespaced: false },
  { key: 'events', label: 'Events', group: '集群', path: ['_', 'v1', 'events'], namespaced: true },
]

export function statusOf(item: ResourceItem, type: ResourceType): string {
  const status = (item.status ?? {}) as Record<string, any>
  const spec = (item.spec ?? {}) as Record<string, any>

  switch (type.key) {
    case 'pods': {
      const phase = String(status.phase ?? 'Unknown')
      const containers = (status.containerStatuses ?? []) as { ready?: boolean }[]
      const ready = containers.filter((c) => c.ready).length
      return containers.length > 0 ? `${phase} ${ready}/${containers.length}` : phase
    }
    case 'deployments':
    case 'statefulsets':
      return `${status.readyReplicas ?? 0}/${spec.replicas ?? 0}`
    case 'daemonsets':
      return `${status.numberReady ?? 0}/${status.desiredNumberScheduled ?? 0}`
    case 'services':
      return String(spec.type ?? '-')
    case 'nodes': {
      const conditions = (status.conditions ?? []) as { type: string; status: string }[]
      const ready = conditions.find((c) => c.type === 'Ready')
      return ready?.status === 'True' ? 'Ready' : 'NotReady'
    }
    case 'jobs': {
      if (status.succeeded) return `Succeeded ${status.succeeded}`
      if (status.failed) return `Failed ${status.failed}`
      return status.active ? `Active ${status.active}` : 'Pending'
    }
    default:
      return '-'
  }
}

export function ageOf(timestamp?: string): string {
  if (!timestamp) return '-'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(timestamp).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

export function statusColor(value: string): string {
  if (/^(Running|Ready|Succeeded|Active|Healthy)/.test(value)) return 'green'
  if (/^(Pending|ContainerCreating|Terminating)/.test(value)) return 'gold'
  if (/^(Failed|Error|CrashLoopBackOff|NotReady|Unknown)/.test(value)) return 'red'
  return 'default'
}