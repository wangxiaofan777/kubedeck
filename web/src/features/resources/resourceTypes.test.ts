import { describe, expect, it } from 'vitest'
import type { APIResource } from '../../lib/api'
import { preferredResourceType, resolveType } from './resourceTypes'

function resource(
  group: string,
  version: string,
  resourceName: string,
  kind: string,
): APIResource {
  return {
    group,
    version,
    resource: resourceName,
    kind,
    namespaced: true,
    verbs: ['get', 'list', 'create'],
  }
}

describe('preferredResourceType', () => {
  const cronjobs = resolveType('cronjobs')!
  const ingresses = resolveType('ingresses')!

  it('keeps the static version when the cluster serves it', () => {
    const resources = [resource('batch', 'v1', 'cronjobs', 'CronJob')]
    expect(preferredResourceType(cronjobs, resources).gvr.version).toBe('v1')
  })

  it('falls back to batch/v1beta1 on 1.18 clusters', () => {
    const resources = [resource('batch', 'v1beta1', 'cronjobs', 'CronJob')]
    const effective = preferredResourceType(cronjobs, resources)
    expect(effective.gvr).toEqual({ group: 'batch', version: 'v1beta1', resource: 'cronjobs' })
    expect(effective.key).toBe('cronjobs')
    expect(effective.label).toBe(cronjobs.label)
  })

  it('falls back to networking.k8s.io/v1beta1 for Ingress', () => {
    const resources = [resource('networking.k8s.io', 'v1beta1', 'ingresses', 'Ingress')]
    expect(preferredResourceType(ingresses, resources).gvr.version).toBe('v1beta1')
  })

  it('prefers stable versions when several are served', () => {
    const resources = [
      resource('networking.k8s.io', 'v1beta1', 'ingresses', 'Ingress'),
      resource('networking.k8s.io', 'v1', 'ingresses', 'Ingress'),
      resource('extensions', 'v1beta1', 'ingresses', 'Ingress'),
    ]
    expect(preferredResourceType(ingresses, resources).gvr.version).toBe('v1')
  })

  it('returns the static type when discovery has no match or has failed', () => {
    expect(preferredResourceType(cronjobs, undefined)).toBe(cronjobs)
    expect(preferredResourceType(cronjobs, [])).toBe(cronjobs)
    expect(preferredResourceType(cronjobs, [resource('apps', 'v1', 'deployments', 'Deployment')])).toBe(
      cronjobs,
    )
  })
})