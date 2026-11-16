import type { ResourceItem } from '../../lib/api'
import type { ResourceType } from './resourceTypes'

export const OWNER_TYPE: Record<string, string> = {
  Deployment: 'deployments',
  ReplicaSet: 'replicasets',
  StatefulSet: 'statefulsets',
  DaemonSet: 'daemonsets',
  Job: 'jobs',
  CronJob: 'cronjobs',
}

export function record(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

export function selectorOf(item: ResourceItem): string {
  const spec = record(item.spec)
  const selector = record(spec.selector)
  const matchLabels = record(selector.matchLabels)
  const labels = Object.keys(matchLabels).length ? matchLabels : selector
  return Object.entries(labels)
    .filter(([, value]) => typeof value === 'string')
    .map(([key, value]) => `${key}=${value}`)
    .join(',')
}

export function containersOf(item?: ResourceItem): string[] {
  const list = item?.spec?.containers
  return Array.isArray(list)
    ? (list as { name?: string }[]).map((container) => container.name ?? '').filter(Boolean)
    : []
}

export function detailBase(cluster: string, type: ResourceType, namespace: string | undefined, name: string) {
  const ns = namespace ?? '_'
  return `/clusters/${encodeURIComponent(cluster)}/${type.key}/${encodeURIComponent(ns)}/${encodeURIComponent(name)}`
}

export function listPath(cluster: string, type: ResourceType, namespace: string | undefined) {
  return `/clusters/${encodeURIComponent(cluster)}/${type.key}${
    namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
  }`
}