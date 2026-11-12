export interface ClusterInfo {
  name: string
  server: string
  version: string
  status: string
  message?: string
}

export interface ObjectMeta {
  name: string
  namespace?: string
  creationTimestamp?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  uid?: string
  resourceVersion?: string
  ownerReferences?: { kind: string; name: string }[]
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
}

export class ApiError extends Error {
  code: string
  requestId?: string

  constructor(code: string, message: string, requestId?: string) {
    super(message)
    this.code = code
    this.requestId = requestId
  }
}

async function toError(res: Response): Promise<ApiError> {
  let code = `HTTP_${res.status}`
  let message = res.statusText
  let requestId: string | undefined
  try {
    const body = (await res.json()) as { code?: string; message?: string; requestId?: string }
    code = body.code ?? code
    message = body.message ?? message
    requestId = body.requestId
  } catch {
    // 非 JSON 响应：保留 HTTP 状态
  }
  return new ApiError(code, message, requestId)
}

async function request<T>(url: string): Promise<T> {
  const res = await fetch(url, { headers: { Accept: 'application/json' } })
  if (!res.ok) throw await toError(res)
  return (await res.json()) as T
}

function clusterPath(cluster: string): string {
  return `/api/v1/clusters/${encodeURIComponent(cluster)}`
}

export const api = {
  clusters: () => request<ClusterInfo[]>('/api/v1/clusters'),

  namespaces: (cluster: string) =>
    request<{ namespaces: string[] }>(`${clusterPath(cluster)}/namespaces`),

  list(cluster: string, group: string, version: string, resource: string, namespace: string, limit = 200) {
    const q = new URLSearchParams({ limit: String(limit) })
    if (namespace) q.set('namespace', namespace)
    return request<ListResponse>(
      `${clusterPath(cluster)}/resources/${group}/${version}/${resource}?${q.toString()}`,
    )
  },

  async yaml(
    cluster: string,
    group: string,
    version: string,
    resource: string,
    namespace: string,
    name: string,
  ): Promise<string> {
    const q = new URLSearchParams({ format: 'yaml' })
    if (namespace) q.set('namespace', namespace)
    const res = await fetch(
      `${clusterPath(cluster)}/resources/${group}/${version}/${resource}/${encodeURIComponent(name)}?${q.toString()}`,
      { headers: { Accept: 'text/yaml' } },
    )
    if (!res.ok) throw await toError(res)
    return res.text()
  },
}