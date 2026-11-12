import type { GVR, ResourceItem } from '../../lib/api'

export interface ResourceType {
  key: string
  label: string
  group: '工作负载' | '网络' | '配置' | '集群'
  /** core 组用空串，API 层会转换为路径中的 "_"。 */
  gvr: GVR
  namespaced: boolean
}

export const RESOURCE_TYPES: ResourceType[] = [
  {
    key: 'pods',
    label: 'Pods',
    group: '工作负载',
    gvr: { group: '', version: 'v1', resource: 'pods' },
    namespaced: true,
  },
  {
    key: 'deployments',
    label: 'Deployments',
    group: '工作负载',
    gvr: { group: 'apps', version: 'v1', resource: 'deployments' },
    namespaced: true,
  },
  {
    key: 'statefulsets',
    label: 'StatefulSets',
    group: '工作负载',
    gvr: { group: 'apps', version: 'v1', resource: 'statefulsets' },
    namespaced: true,
  },
  {
    key: 'daemonsets',
    label: 'DaemonSets',
    group: '工作负载',
    gvr: { group: 'apps', version: 'v1', resource: 'daemonsets' },
    namespaced: true,
  },
  {
    key: 'jobs',
    label: 'Jobs',
    group: '工作负载',
    gvr: { group: 'batch', version: 'v1', resource: 'jobs' },
    namespaced: true,
  },
  {
    key: 'cronjobs',
    label: 'CronJobs',
    group: '工作负载',
    gvr: { group: 'batch', version: 'v1', resource: 'cronjobs' },
    namespaced: true,
  },
  {
    key: 'services',
    label: 'Services',
    group: '网络',
    gvr: { group: '', version: 'v1', resource: 'services' },
    namespaced: true,
  },
  {
    key: 'ingresses',
    label: 'Ingresses',
    group: '网络',
    gvr: { group: 'networking.k8s.io', version: 'v1', resource: 'ingresses' },
    namespaced: true,
  },
  {
    key: 'configmaps',
    label: 'ConfigMaps',
    group: '配置',
    gvr: { group: '', version: 'v1', resource: 'configmaps' },
    namespaced: true,
  },
  {
    key: 'secrets',
    label: 'Secrets',
    group: '配置',
    gvr: { group: '', version: 'v1', resource: 'secrets' },
    namespaced: true,
  },
  {
    key: 'nodes',
    label: 'Nodes',
    group: '集群',
    gvr: { group: '', version: 'v1', resource: 'nodes' },
    namespaced: false,
  },
  {
    key: 'namespaces',
    label: 'Namespaces',
    group: '集群',
    gvr: { group: '', version: 'v1', resource: 'namespaces' },
    namespaced: false,
  },
  {
    key: 'events',
    label: 'Events',
    group: '集群',
    gvr: { group: '', version: 'v1', resource: 'events' },
    namespaced: true,
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
