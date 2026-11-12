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

/** GVR 是资源定位三元组；core 组用空串表示。 */
export interface GVR {
  group: string
  version: string
  resource: string
}

export class ApiError extends Error {
  readonly code: string
  readonly requestId?: string
  readonly status?: number
  /** 5xx 与 429 可重试；4xx 与网络错误不自动重试（网络错误由用户主动刷新）。 */
  readonly retryable: boolean

  constructor(
    code: string,
    message: string,
    options: { requestId?: string; status?: number } = {},
  ) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.requestId = options.requestId
    this.status = options.status
    this.retryable =
      options.status !== undefined && (options.status >= 500 || options.status === 429)
  }
}

async function toError(res: Response): Promise<ApiError> {
  let code = `HTTP_${res.status}`
  let message = res.statusText || '请求失败'
  let requestId: string | undefined
  try {
    const body = (await res.json()) as { code?: string; message?: string; requestId?: string }
    code = body.code ?? code
    message = body.message ?? message
    requestId = body.requestId
  } catch {
    // 非 JSON 响应：保留 HTTP 状态与状态文本。
  }
  return new ApiError(code, message, { requestId, status: res.status })
}

async function request<T>(url: string, init: RequestInit = {}): Promise<T> {
  let res: Response
  try {
    res = await fetch(url, {
      ...init,
      headers: { Accept: 'application/json', ...init.headers },
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError('NETWORK_ERROR', '无法连接平台服务，请检查网络或后端是否启动')
  }

  if (!res.ok) throw await toError(res)
  try {
    return (await res.json()) as T
  } catch {
    throw new ApiError('INVALID_RESPONSE', '服务端返回了无法解析的响应', { status: res.status })
  }
}

function clusterPath(cluster: string): string {
  return `/api/v1/clusters/${encodeURIComponent(cluster)}`
}

function resourcePath(cluster: string, gvr: GVR, name?: string): string {
  const base = `${clusterPath(cluster)}/resources/${gvr.group || '_'}/${gvr.version}/${gvr.resource}`
  return name ? `${base}/${encodeURIComponent(name)}` : base
}

export const api = {
  clusters(signal?: AbortSignal): Promise<ClusterInfo[]> {
    return request<ClusterInfo[]>('/api/v1/clusters', { signal })
  },

  namespaces(cluster: string, signal?: AbortSignal): Promise<{ namespaces: string[] }> {
    return request<{ namespaces: string[] }>(`${clusterPath(cluster)}/namespaces`, { signal })
  },

  list(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    limit?: number
    signal?: AbortSignal
  }): Promise<ListResponse> {
    const q = new URLSearchParams({ limit: String(params.limit ?? 200) })
    if (params.namespace) q.set('namespace', params.namespace)
    return request<ListResponse>(`${resourcePath(params.cluster, params.gvr)}?${q.toString()}`, {
      signal: params.signal,
    })
  },

  async yaml(params: {
    cluster: string
    gvr: GVR
    namespace?: string
    name: string
    signal?: AbortSignal
  }): Promise<string> {
    const q = new URLSearchParams({ format: 'yaml' })
    if (params.namespace) q.set('namespace', params.namespace)

    let res: Response
    try {
      res = await fetch(
        `${resourcePath(params.cluster, params.gvr, params.name)}?${q.toString()}`,
        {
          headers: { Accept: 'text/yaml' },
          signal: params.signal,
        },
      )
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') throw err
      throw new ApiError('NETWORK_ERROR', '无法连接平台服务，请检查网络或后端是否启动')
    }
    if (!res.ok) throw await toError(res)
    return res.text()
  },
}

export type ApiErrorLike = Pick<ApiError, 'code' | 'message' | 'requestId'>
