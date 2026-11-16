import { describe, expect, it } from 'vitest'
import type { ResourceItem } from '../../lib/api'
import {
  FORM_KINDS,
  buildManifest,
  emptyValues,
  templatesFor,
  valuesFromResource,
  type FormKind,
} from './createManifest'

const KIND_KIND: Record<FormKind, string> = {
  deployments: 'Deployment',
  statefulsets: 'StatefulSet',
  services: 'Service',
  configmaps: 'ConfigMap',
  secrets: 'Secret',
  cronjobs: 'CronJob',
  ingresses: 'Ingress',
  namespaces: 'Namespace',
}

function valuesFor(kind: FormKind) {
  const values = emptyValues('default')
  values.name = 'sample-app'
  values.image = 'nginx:1.27'
  if (kind === 'cronjobs') values.schedule = '*/5 * * * *'
  if (kind === 'ingresses') {
    values.host = 'demo.example.com'
    values.serviceName = 'sample-app'
  }
  return values
}

interface ManifestShape {
  apiVersion?: string
  kind?: string
  metadata: { name?: string; namespace?: string }
  spec: {
    replicas?: number
    selector?: { matchLabels?: Record<string, string> }
    template?: { metadata?: { labels?: Record<string, string> } }
  }
  stringData?: Record<string, string>
  data?: Record<string, string>
}

function manifestOf(kind: FormKind, values = valuesFor(kind)): ManifestShape {
  return buildManifest(kind, values) as unknown as ManifestShape
}

describe('buildManifest', () => {
  it.each(FORM_KINDS)('builds a %s manifest with the expected identity', (kind) => {
    const manifest = manifestOf(kind)
    expect(manifest.kind).toBe(KIND_KIND[kind])
    expect(manifest.apiVersion).toBeTruthy()
    expect(manifest.metadata.name).toBe('sample-app')
    if (kind !== 'namespaces') expect(manifest.metadata.namespace).toBe('default')
    else expect(manifest.metadata.namespace).toBeUndefined()
  })

  it('uses selector labels without YAML aliases', () => {
    const manifest = manifestOf('deployments')
    const selectorLabels = manifest.spec.selector?.matchLabels
    const templateLabels = manifest.spec.template?.metadata?.labels
    expect(selectorLabels).toEqual({ app: 'sample-app' })
    expect(templateLabels).toEqual({ app: 'sample-app' })
    expect(selectorLabels).not.toBe(templateLabels)
  })

  it('builds Secrets from stringData so the server owns base64', () => {
    const values = valuesFor('secrets')
    values.env = [{ key: 'username', value: 'demo' }]
    const manifest = manifestOf('secrets', values)
    expect(manifest.stringData).toEqual({ username: 'demo' })
    expect(manifest.data).toBeUndefined()
  })

  it('snapshots a representative Deployment', () => {
    expect(buildManifest('deployments', valuesFor('deployments'))).toMatchSnapshot()
  })
})

describe('templatesFor', () => {
  it('offers workload templates but not for key-value resources', () => {
    expect(templatesFor('deployments').length).toBeGreaterThan(0)
    expect(templatesFor('secrets')).toEqual([])
  })
})

describe('valuesFromResource', () => {
  it('maps an existing Deployment back into form values', () => {
    const manifest = manifestOf('deployments')
    const values = valuesFromResource(
      'deployments',
      manifest as unknown as ResourceItem,
      'default',
    )
    expect(values.name).toBe('sample-app')
    expect(values.image).toBe('nginx:1.27')
    expect(values.replicas).toBe(2)
  })
})