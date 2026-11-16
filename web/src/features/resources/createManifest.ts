import type { ResourceItem } from '../../lib/api'

export interface KeyValue {
  key: string
  value: string
}

export interface FormValues {
  name: string
  namespace: string
  image: string
  replicas: number
  containerPort?: number
  servicePort?: number
  serviceType: 'ClusterIP' | 'NodePort' | 'LoadBalancer'
  command: string
  env: KeyValue[]
  labels: KeyValue[]
  cpuRequest: string
  memoryRequest: string
  cpuLimit: string
  memoryLimit: string
  schedule: string
  host: string
  path: string
  pathType: 'Prefix' | 'Exact'
  tls: boolean
  serviceName: string
}

export const FORM_KINDS = [
  'deployments',
  'statefulsets',
  'services',
  'configmaps',
  'secrets',
  'cronjobs',
  'ingresses',
  'namespaces',
] as const

export type FormKind = (typeof FORM_KINDS)[number]

export function emptyValues(namespace = 'default'): FormValues {
  return {
    name: '',
    namespace,
    image: 'nginx:1.27',
    replicas: 2,
    containerPort: 80,
    servicePort: 80,
    serviceType: 'ClusterIP',
    command: '',
    env: [],
    labels: [],
    cpuRequest: '100m',
    memoryRequest: '128Mi',
    cpuLimit: '500m',
    memoryLimit: '512Mi',
    schedule: '0 * * * *',
    host: '',
    path: '/',
    pathType: 'Prefix',
    tls: false,
    serviceName: '',
  }
}

export interface Template {
  id: string
  label: string
  description: string
  patch: Partial<FormValues>
}

export function templatesFor(kind: FormKind): Template[] {
  if (kind === 'services') {
    return [
      {
        id: 'clusterip',
        label: '内部服务',
        description: 'ClusterIP，80 → 8080',
        patch: { serviceType: 'ClusterIP', servicePort: 80, containerPort: 8080 },
      },
      {
        id: 'nodeport',
        label: 'NodePort',
        description: '通过节点端口暴露',
        patch: { serviceType: 'NodePort', servicePort: 80, containerPort: 8080 },
      },
    ]
  }
  if (kind === 'cronjobs') {
    return [
      {
        id: 'hourly',
        label: '每小时任务',
        description: 'busybox 输出当前时间',
        patch: { image: 'busybox:1.36', schedule: '0 * * * *', command: 'date' },
      },
    ]
  }
  if (kind === 'configmaps' || kind === 'secrets' || kind === 'namespaces') {
    return []
  }
  return [
    {
      id: 'web',
      label: 'Web 服务',
      description: 'nginx，2 副本，80 端口',
      patch: { image: 'nginx:1.27', replicas: 2, containerPort: 80 },
    },
    {
      id: 'redis',
      label: 'Redis',
      description: '单副本缓存，6379',
      patch: { image: 'redis:7-alpine', replicas: 1, containerPort: 6379 },
    },
  ]
}

function labelsObject(labels: KeyValue[]): Record<string, string> {
  return Object.fromEntries(labels.filter((item) => item.key.trim()).map((item) => [item.key.trim(), item.value]))
}

function envArray(env: KeyValue[]) {
  return env
    .filter((item) => item.key.trim())
    .map((item) => ({ name: item.key.trim(), value: item.value }))
}

function resources(v: FormValues) {
  const limits: Record<string, string> = {}
  const requests: Record<string, string> = {}
  if (v.cpuRequest) requests.cpu = v.cpuRequest
  if (v.memoryRequest) requests.memory = v.memoryRequest
  if (v.cpuLimit) limits.cpu = v.cpuLimit
  if (v.memoryLimit) limits.memory = v.memoryLimit
  const out: Record<string, unknown> = {}
  if (Object.keys(requests).length) out.requests = requests
  if (Object.keys(limits).length) out.limits = limits
  return Object.keys(out).length ? out : undefined
}

function container(v: FormValues) {
  return {
    name: v.name || 'app',
    image: v.image,
    ...(v.containerPort ? { ports: [{ containerPort: v.containerPort, protocol: 'TCP' }] } : {}),
    ...(v.command.trim() ? { command: v.command.trim().split(/\s+/) } : {}),
    ...(envArray(v.env).length ? { env: envArray(v.env) } : {}),
    ...(resources(v) ? { resources: resources(v) } : {}),
  }
}

/** buildManifest 由表单值生成对象；纯函数，便于单测与 YAML 预览。 */
export function buildManifest(kind: FormKind, v: FormValues): Record<string, unknown> | undefined {
  const labels = labelsObject(v.labels)
  const base = {
    metadata: {
      name: v.name,
      ...(kind === 'namespaces' ? {} : { namespace: v.namespace }),
      ...(Object.keys(labels).length ? { labels } : {}),
    },
  }
  const appLabels = { app: v.name }

  switch (kind) {
    case 'deployments':
      return {
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        ...base,
        spec: {
          replicas: v.replicas,
          selector: { matchLabels: { ...appLabels } },
          template: {
            metadata: { labels: { ...appLabels } },
            spec: { containers: [container(v)] },
          },
        },
      }
    case 'statefulsets':
      return {
        apiVersion: 'apps/v1',
        kind: 'StatefulSet',
        ...base,
        spec: {
          replicas: v.replicas,
          serviceName: v.serviceName || v.name,
          selector: { matchLabels: { ...appLabels } },
          template: {
            metadata: { labels: { ...appLabels } },
            spec: { containers: [container(v)] },
          },
        },
      }
    case 'services':
      return {
        apiVersion: 'v1',
        kind: 'Service',
        ...base,
        spec: {
          type: v.serviceType,
          selector: { ...appLabels },
          ports: [
            {
              name: 'http',
              port: v.servicePort ?? 80,
              targetPort: v.containerPort ?? 8080,
              protocol: 'TCP',
            },
          ],
        },
      }
    case 'configmaps':
      return {
        apiVersion: 'v1',
        kind: 'ConfigMap',
        ...base,
        data: Object.fromEntries(
          v.env.filter((item) => item.key.trim()).map((item) => [item.key.trim(), item.value]),
        ),
      }
    case 'secrets':
      return {
        apiVersion: 'v1',
        kind: 'Secret',
        ...base,
        type: 'Opaque',
        stringData: Object.fromEntries(
          v.env.filter((item) => item.key.trim()).map((item) => [item.key.trim(), item.value]),
        ),
      }
    case 'cronjobs':
      return {
        apiVersion: 'batch/v1',
        kind: 'CronJob',
        ...base,
        spec: {
          schedule: v.schedule,
          jobTemplate: {
            spec: {
              template: {
                spec: {
                  restartPolicy: 'OnFailure',
                  containers: [container(v)],
                },
              },
            },
          },
        },
      }
    case 'ingresses':
      return {
        apiVersion: 'networking.k8s.io/v1',
        kind: 'Ingress',
        ...base,
        spec: {
          ...(v.tls ? { tls: [{ hosts: [v.host], secretName: `${v.name}-tls` }] } : {}),
          rules: [
            {
              ...(v.host ? { host: v.host } : {}),
              http: {
                paths: [
                  {
                    path: v.path || '/',
                    pathType: v.pathType,
                    backend: {
                      service: {
                        name: v.serviceName || v.name,
                        port: { number: v.servicePort ?? 80 },
                      },
                    },
                  },
                ],
              },
            },
          ],
        },
      }
    case 'namespaces':
      return { apiVersion: 'v1', kind: 'Namespace', ...base }
    default:
      return undefined
  }
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

/** valuesFromResource 从现有对象提取表单值（复制为新）。 */
export function valuesFromResource(
  kind: FormKind,
  item: ResourceItem,
  namespace: string,
): Partial<FormValues> {
  const spec = asRecord(item.spec)
  const jobSpec = asRecord(asRecord(asRecord(spec.jobTemplate).spec))
  const jobPod = asRecord(asRecord(jobSpec.template).spec)
  const templatePod = asRecord(asRecord(spec.template).spec)
  // Deployment/StatefulSet/Job 的容器在 spec.template.spec 下，CronJob 在 jobTemplate 下。
  const podSpec =
    kind === 'cronjobs'
      ? jobPod
      : Object.keys(templatePod).length > 0
        ? templatePod
        : spec
  const containers = Array.isArray(podSpec.containers) ? podSpec.containers : []
  const first = asRecord(containers[0])
  const port = Array.isArray(first.ports) ? asRecord(first.ports[0]) : {}
  const res = asRecord(first.resources)
  const requests = asRecord(res.requests)
  const limits = asRecord(res.limits)
  const env = Array.isArray(first.env)
    ? (first.env as { name?: string; value?: string }[]).map((entry) => ({
        key: entry.name ?? '',
        value: entry.value ?? '',
      }))
    : []
  const servicePorts = Array.isArray(spec.ports) ? asRecord(spec.ports[0]) : {}
  const rules = Array.isArray(spec.rules) ? asRecord(spec.rules[0]) : {}
  const http = asRecord(rules.http)
  const paths = Array.isArray(http.paths) ? asRecord(http.paths[0]) : {}
  const backend = asRecord(asRecord(paths.backend).service)
  const backendPort = asRecord(backend.port)

  return {
    name: item.metadata.name,
    namespace: item.metadata.namespace ?? namespace,
    image: (first.image as string) ?? 'nginx:1.27',
    replicas: typeof spec.replicas === 'number' ? spec.replicas : 1,
    containerPort:
      typeof port.containerPort === 'number'
        ? port.containerPort
        : typeof backendPort.number === 'number'
          ? backendPort.number
          : undefined,
    servicePort: typeof servicePorts.port === 'number' ? servicePorts.port : undefined,
    serviceType: (spec.type as FormValues['serviceType']) ?? 'ClusterIP',
    command: Array.isArray(first.command) ? (first.command as string[]).join(' ') : '',
    env,
    labels: Object.entries(item.metadata.labels ?? {}).map(([key, value]) => ({ key, value })),
    cpuRequest: (requests.cpu as string) ?? '',
    memoryRequest: (requests.memory as string) ?? '',
    cpuLimit: (limits.cpu as string) ?? '',
    memoryLimit: (limits.memory as string) ?? '',
    schedule: (spec.schedule as string) ?? '0 * * * *',
    host: (rules.host as string) ?? '',
    path: (paths.path as string) ?? '/',
    pathType: (paths.pathType as FormValues['pathType']) ?? 'Prefix',
    tls: Array.isArray(spec.tls) && spec.tls.length > 0,
    serviceName: (spec.serviceName as string) ?? (backend.name as string) ?? '',
  }
}