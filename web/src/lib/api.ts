export interface ClusterInfo {
  name: string
  server: string
  version: string
  status: string
  message?: string
}

/** 总览统计范围：allNamespaces=false 表示只覆盖逐个读取成功的命名空间。 */
export interface OverviewScope {
  allNamespaces: boolean
  namespaces: number
  truncated: boolean
}

export interface OverviewAllocatable {
  cpuMilli: number
  memoryBytes: number
}

export interface OverviewNodes {
  total: number
  ready: number
  allocatable: OverviewAllocatable
}

export interface OverviewPods {
  total: number
  running: number
  pending: number
  succeeded: number
  failed: number
  unknown: number
  crashLoop: number
}

export interface WorkloadCount {
  total: number
  unavailable: number
}

export interface OverviewWorkloads {
  deployments: WorkloadCount
  statefulSets: WorkloadCount
  daemonSets: WorkloadCount
}

export interface OverviewEvent {
  type: string
  reason: string
  namespace?: string
  object: string
  message: string
  lastTimestamp: string
}

export interface ClusterOverview {
  cluster: string
  fetchedAt: string
  scope: OverviewScope
  nodes: OverviewNodes
  namespaces: { total: number }
  pods: OverviewPods
  workloads: OverviewWorkloads
  events: OverviewEvent[]
  unavailable: string[]
}

export interface ObjectMeta {
  name: string
  namespace?: string
  creationTimestamp?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  uid?: string
  resourceVersion?: string
  ownerReferences?: { kind: string; name: string; uid?: string }[]
}

export interface ResourceItem {
  apiVersion: string
  kind: string
  metadata: ObjectMeta
  status?: Record<string, unknown>
  spec?: Record<string, unknown>
  [key: string]: unknown
}

export interface ListResponse {
  items: ResourceItem[]
  continue?: string
  remainingItemCount?: number
  resourceVersion?: string
}

/** GVR 是资源定位三元组；core 组用空串表示。 */
export interface GVR {
  group: string
  version: string
  resource: string
}

export interface RoleBinding {
  id: number
  userId: number
  clusterId: number
  clusterName: string
  namespace: string
  roleCode: string
}

export interface MeResponse {
  user: {
    userId: number
    subject: string
    email: string
    displayName: string
    isPlatformAdmin: boolean
    dev?: boolean
  }
  mode: 'oidc' | 'dev'
  clusters: string[]
  bindings: RoleBinding[]
}

export interface APIResource {
  group: string
  version: string
  resource: string
  kind: string
  namespaced: boolean
  verbs: string[]
}

export interface DiscoveryResponse {
  resources: APIResource[]
  failedGroups?: string[]
  fetchedAt: string
}

export interface AccessCheck {
  cluster: string
  namespace?: string
  verb: string
  group?: string
  version?: string
  resource: string
  subresource?: string
  name?: string
}

export interface AccessResult {
  allowed: boolean
  reason?: string
}

export interface ResourceDiff {
  diff: string
  immutableFields?: string[]
  currentResourceVersion?: string
  submittedResourceVersion?: string
}

export interface WorkloadOperation {
  id: string
  type: 'scale' | 'restart'
  cluster: string
  namespace: string
  name: string
  resource: string
  targetReplicas: number
  currentReplicas: number
  readyReplicas: number
  updatedReplicas: number
  availableReplicas: number
  startedAt: string
  elapsedMs: number
  concluded: boolean
  success: boolean
  message?: string
}

export interface Revision {
  revision: number
  name: string
  images: string[]
  replicas: number
  readyReplicas: number
  createdAt: string
}

export interface EventItem {
  type: string
  reason: string
  message: string
  count: number
  lastTimestamp: string
  firstTimestamp?: string
  involvedObject?: { kind?: string; name?: string; namespace?: string }
  source?: { component?: string }
}

export interface AuditEntry {
  id: number
  occurredAt: string
  requestId: string
  actorSubject: string
  actorEmail?: string
  clusterName?: string
  verb: string
  apiGroup?: string
  resource?: string
  namespace?: string
  objectName?: string
  result: string
  resultCode: number
  latencyMs: number
  clientIp?: string
  bodyHash?: string
  detail?: Record<string, unknown>
}

export interface AuditListResponse {
  items: AuditEntry[]
  total: number
  limit: number
  offset: number
  retentionWarning?: string
}

export interface AuditCount {
  key: string
  count: number
}

export interface AuditDayCount {
  day: string
  total: number
  failed: number
}

export interface AuditStats {
  from: string
  to: string
  total: number
  success: number
  failed: number
  byDay: AuditDayCount[]
  byVerb: AuditCount[]
  byResource: AuditCount[]
  byActor: AuditCount[]
}

export class ApiError extends Error {
  readonly code: string
  readonly requestId?: string
  readonly status?: number
  readonly details?: Record<string, unknown>
  /** 5xx 与 429 可重试；4xx 与网络错误不自动重试（网络错误由用户主动刷新）。 */
  readonly retryable: boolean

  constructor(
    code: string,
    message: string,
    options: {
      requestId?: string
      status?: number
      details?: Record<string, unknown>
    } = {},
  ) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.requestId = options.requestId
    this.status = options.status
    this.details = options.details
    this.retryable =
      options.status !== undefined && (options.status >= 500 || options.status === 429)
  }
}

async function toError(res: Response): Promise<ApiError> {
  let code = `HTTP_${res.status}`
  let message = res.statusText || '请求失败'
  let requestId: string | undefined
  let details: Record<string, unknown> | undefined
  try {
    const body = (await res.json()) as {
      code?: string
      message?: string
      requestId?: string
      details?: Record<string, unknown>
    }
    code = body.code ?? code
    message = body.message ?? message
    requestId = body.requestId
    details = body.details
  } catch {
    // 非 JSON 响应：保留 HTTP 状态与状态文本。
  }
  return new ApiError(code, message, { requestId, status: res.status, details })
}

async function request<T>(url: string, init: RequestInit = {}): Promise<T> {
  let res: Response
  try {
    res = await fetch(url, {
      ...init,
      credentials: 'same-origin',
      headers: { Accept: 'application/json', ...init.headers },
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError('NETWORK_ERROR', '无法连接平台服务，请检查网络或后端是否启动')
  }

  if (!res.ok) throw await toError(res)
  if (res.status === 204) return undefined as T
  try {
    return (await res.json()) as T
  } catch {
    throw new ApiError('INVALID_RESPONSE', '服务端返回了无法解析的响应', {
      status: res.status,
    })
  }
}

async function requestText(
  url: string,
  init: RequestInit = {},
  accept = 'text/yaml',
): Promise<string> {
  let res: Response
  try {
    res = await fetch(url, {
      ...init,
      credentials: 'same-origin',
      headers: { Accept: accept, ...init.headers },
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError('NETWORK_ERROR', '无法连接平台服务，请检查网络或后端是否启动')
  }
  if (!res.ok) throw await toError(res)
  return res.text()
}

function clusterPath(cluster: string): string {
  return `/api/v1/clusters/${encodeURIComponent(cluster)}`
}

function resourcePath(cluster: string, gvr: GVR, name?: string): string {
  const base = `${clusterPath(cluster)}/resources/${gvr.group || '_'}/${gvr.version}/${gvr.resource}`
  return name ? `${base}/${encodeURIComponent(name)}` : base
}

function query(params: Record<string, unknown>): string {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue
    q.set(key, String(value))
  }
  const text = q.toString()
  return text ? `?${text}` : ''
}

export const api = {
  me(signal?: AbortSignal): Promise<MeResponse> {
    return request<MeResponse>('/api/v1/auth/me', { signal })
  },

  loginURL(returnTo: string): string {
    return `/auth/login?returnTo=${encodeURIComponent(returnTo)}`
  },

  logout(): Promise<void> {
    return request<void>('/api/v1/auth/logout', { method: 'POST' })
  },

  can(checks: AccessCheck[], signal?: AbortSignal): Promise<{ results: AccessResult[] }> {
    return request<{ results: AccessResult[] }>('/api/v1/auth/can', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ checks }),
      signal,
    })
  },

  clusters(signal?: AbortSignal): Promise<ClusterInfo[]> {
    return request<ClusterInfo[]>('/api/v1/clusters', { signal })
  },

  overview(cluster: string, signal?: AbortSignal): Promise<ClusterOverview> {
    return request<ClusterOverview>(`${clusterPath(cluster)}/overview`, { signal })
  },

  clusterDetail(cluster: string, signal?: AbortSignal): Promise<Record<string, unknown>> {
    return request<Record<string, unknown>>(`${clusterPath(cluster)}`, { signal })
  },

  namespaces(cluster: string, signal?: AbortSignal): Promise<{ namespaces: string[] }> {
    return request<{ namespaces: string[] }>(`${clusterPath(cluster)}/namespaces`, { signal })
  },

  apiResources(cluster: string, signal?: AbortSignal): Promise<DiscoveryResponse> {
    return request<DiscoveryResponse>(`${clusterPath(cluster)}/api-resources`, { signal })
  },

  list(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    limit?: number
    continueToken?: string
    labelSelector?: string
    fieldSelector?: string
    sortBy?: string
    order?: string
    signal?: AbortSignal
  }): Promise<ListResponse> {
    const qs = query({
      limit: params.limit ?? 200,
      continue: params.continueToken,
      namespace: params.namespace,
      labelSelector: params.labelSelector,
      fieldSelector: params.fieldSelector,
      sortBy: params.sortBy,
      order: params.order,
    })
    return request<ListResponse>(`${resourcePath(params.cluster, params.gvr)}${qs}`, {
      signal: params.signal,
    })
  },

  detail(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    signal?: AbortSignal
  }): Promise<{
    object: ResourceItem
    ownerReferences?: ObjectMeta['ownerReferences']
    events: EventItem[]
    eventsError?: string
  }> {
    const qs = query({ namespace: params.namespace })
    return request(`${resourcePath(params.cluster, params.gvr, params.name)}/detail${qs}`, {
      signal: params.signal,
    })
  },

  yaml(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    signal?: AbortSignal
  }): Promise<string> {
    return requestText(
      `${resourcePath(params.cluster, params.gvr, params.name)}${query({
        namespace: params.namespace,
        format: 'yaml',
      })}`,
      { signal: params.signal },
    )
  },

  diff(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    yaml: string
  }): Promise<ResourceDiff> {
    return request<ResourceDiff>(
      `${resourcePath(params.cluster, params.gvr, params.name)}/diff${query({
        namespace: params.namespace,
      })}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/yaml' },
        body: params.yaml,
      },
    )
  },

  updateYAML(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    yaml: string
    dryRun?: boolean
  }): Promise<ResourceItem> {
    return request<ResourceItem>(
      `${resourcePath(params.cluster, params.gvr, params.name)}${query({
        namespace: params.namespace,
        dryRun: params.dryRun ? 'All' : undefined,
      })}`,
      {
        method: 'PUT',
        headers: { 'Content-Type': 'application/yaml' },
        body: params.yaml,
      },
    )
  },

  create(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    yaml: string
    dryRun?: boolean
  }): Promise<ResourceItem> {
    return request<ResourceItem>(
      `${resourcePath(params.cluster, params.gvr)}${query({
        namespace: params.namespace,
        dryRun: params.dryRun ? 'All' : undefined,
      })}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/yaml' },
        body: params.yaml,
      },
    )
  },

  remove(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    propagationPolicy?: string
    dryRun?: boolean
  }): Promise<void> {
    return request<void>(
      `${resourcePath(params.cluster, params.gvr, params.name)}${query({
        namespace: params.namespace,
        propagationPolicy: params.propagationPolicy,
        dryRun: params.dryRun ? 'All' : undefined,
      })}`,
      { method: 'DELETE' },
    )
  },

  batchRemove(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    names: string[]
    propagationPolicy?: string
    dryRun?: boolean
  }): Promise<{
    results: { name: string; success: boolean; code?: string; message?: string }[]
    succeeded: number
    failed: number
  }> {
    return request(
      `${resourcePath(params.cluster, params.gvr)}${query({ namespace: params.namespace })}`,
      {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          names: params.names,
          propagationPolicy: params.propagationPolicy,
          dryRun: params.dryRun,
        }),
      },
    )
  },

  scale(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    replicas: number
  }): Promise<{ operationId: string; targetReplicas: number }> {
    return request(
      `${clusterPath(params.cluster)}/workloads/${params.gvr.group || '_'}/${params.gvr.version}/${params.gvr.resource}/${encodeURIComponent(params.name)}/scale${query({ namespace: params.namespace })}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ replicas: params.replicas }),
      },
    )
  },

  restart(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
  }): Promise<{ operationId: string; restartedAt: string; resourceVersion: string }> {
    return request(
      `${clusterPath(params.cluster)}/workloads/${params.gvr.group || '_'}/${params.gvr.version}/${params.gvr.resource}/${encodeURIComponent(params.name)}/restart${query({ namespace: params.namespace })}`,
      { method: 'POST', headers: { 'Idempotency-Key': crypto.randomUUID() } },
    )
  },

  history(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
  }): Promise<{ currentRevision: number; revisions: Revision[] }> {
    return request(
      `${clusterPath(params.cluster)}/workloads/${params.gvr.group || '_'}/${params.gvr.version}/${params.gvr.resource}/${encodeURIComponent(params.name)}/history${query({ namespace: params.namespace })}`,
    )
  },

  rollbackDiff(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    revision: number
  }): Promise<{ revision: number; diff: string; images: string[] }> {
    return request(
      `${clusterPath(params.cluster)}/workloads/${params.gvr.group || '_'}/${params.gvr.version}/${params.gvr.resource}/${encodeURIComponent(params.name)}/rollback/diff${query({ namespace: params.namespace, revision: params.revision })}`,
    )
  },

  rollback(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    revision: number
  }): Promise<{ rolledBackTo: number; currentRevision: number }> {
    return request(
      `${clusterPath(params.cluster)}/workloads/${params.gvr.group || '_'}/${params.gvr.version}/${params.gvr.resource}/${encodeURIComponent(params.name)}/rollback${query({ namespace: params.namespace })}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ revision: params.revision }),
      },
    )
  },

  pause(params: { cluster: string; gvr: GVR; namespace?: string; name: string }): Promise<void> {
    return request(
      `${clusterPath(params.cluster)}/workloads/${params.gvr.group || '_'}/${params.gvr.version}/${params.gvr.resource}/${encodeURIComponent(params.name)}/pause${query({ namespace: params.namespace })}`,
      { method: 'POST' },
    )
  },

  resume(params: { cluster: string; gvr: GVR; namespace?: string; name: string }): Promise<void> {
    return request(
      `${clusterPath(params.cluster)}/workloads/${params.gvr.group || '_'}/${params.gvr.version}/${params.gvr.resource}/${encodeURIComponent(params.name)}/resume${query({ namespace: params.namespace })}`,
      { method: 'POST' },
    )
  },

  evict(params: { cluster: string; namespace: string; name: string }): Promise<void> {
    return request(
      `${clusterPath(params.cluster)}/pods/${encodeURIComponent(params.namespace)}/${encodeURIComponent(params.name)}/evict`,
      { method: 'POST' },
    )
  },

  operation(cluster: string, id: string, signal?: AbortSignal): Promise<WorkloadOperation> {
    return request<WorkloadOperation>(
      `${clusterPath(cluster)}/operations/${encodeURIComponent(id)}`,
      { signal },
    )
  },

  events(
    cluster: string,
    params: {
      namespace?: string
      kind?: string
      name?: string
      type?: string
      sinceSeconds?: number
      signal?: AbortSignal
    } = {},
  ): Promise<{ items: EventItem[]; staleWarning?: string }> {
    return request(
      `${clusterPath(cluster)}/events${query({
        namespace: params.namespace,
        kind: params.kind,
        name: params.name,
        type: params.type,
        sinceSeconds: params.sinceSeconds,
      })}`,
      { signal: params.signal },
    )
  },

  logURL(params: {
    cluster: string
    namespace: string
    name: string
    container?: string
    tailLines?: number
    previous?: boolean
    follow?: boolean
    sinceTime?: string
    timestamps?: boolean
  }): string {
    return `${clusterPath(params.cluster)}/pods/${encodeURIComponent(params.namespace)}/${encodeURIComponent(params.name)}/log${query(
      {
        container: params.container,
        tailLines: params.tailLines,
        previous: params.previous ? 'true' : undefined,
        follow: params.follow ? 'true' : undefined,
        sinceTime: params.sinceTime,
        timestamps: params.timestamps === false ? 'false' : undefined,
      },
    )}`
  },

  async logStream(url: string, signal?: AbortSignal): Promise<Response> {
    const res = await fetch(url, {
      credentials: 'same-origin',
      signal,
      headers: { Accept: 'text/plain' },
    })
    if (!res.ok) throw await toError(res)
    return res
  },

  auditLogs(
    params: {
      from?: string
      to?: string
      actor?: string
      cluster?: string
      resource?: string
      result?: string
      limit?: number
      offset?: number
      signal?: AbortSignal
    } = {},
  ): Promise<AuditListResponse> {
    const { signal, ...filters } = params
    return request<AuditListResponse>(`/api/v1/admin/audit-logs${query(filters)}`, {
      signal,
    })
  },

  verifyAudit(): Promise<{
    checked: number
    intact: boolean
    brokenId?: number
    brokenKind?: string
  }> {
    return request('/api/v1/admin/audit-logs/verify')
  },

  auditStats(params: {
    cluster?: string
    from?: string
    to?: string
    signal?: AbortSignal
  } = {}): Promise<AuditStats> {
    const { signal, ...filters } = params
    return request<AuditStats>(`/api/v1/admin/audit-logs/stats${query(filters)}`, { signal })
  },

  auditExportURL(
    format: 'csv' | 'json',
    filters: {
      from?: string
      to?: string
      actor?: string
      cluster?: string
      resource?: string
      result?: string
    } = {},
  ): string {
    return `/api/v1/admin/audit-logs/export${query({ ...filters, format })}`
  },
}

export type ApiErrorLike = Pick<ApiError, 'code' | 'message' | 'requestId'>
