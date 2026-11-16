import { useEffect, useRef } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import type { GVR, ListResponse, ResourceItem } from '../lib/api'

interface RealtimeOptions {
  cluster?: string
  gvr: GVR
  namespace?: string
  labelSelector?: string
  queryKey: QueryKey
  resourceVersion?: string
  enabled?: boolean
}

/** buildSubscribeFrame 把订阅参数收敛到一个可测试的纯函数。 */
export function buildSubscribeFrame(
  params: {
    id: string
    gvr: GVR
    namespace?: string
    labelSelector?: string
  },
  resourceVersion: string,
) {
  return {
    type: 'subscribe' as const,
    id: params.id,
    group: params.gvr.group,
    version: params.gvr.version,
    resource: params.gvr.resource,
    namespace: params.namespace,
    labelSelector: params.labelSelector,
    resourceVersion,
  }
}

// resyncWindowMS 内连续两次 resync 视为异常，关闭连接交给退避重连，避免忙循环。
export const resyncWindowMS = 5_000

/** resyncAction 判断收到 resync 后应重订阅还是重建连接。 */
export function resyncAction(lastResyncAt: number, now: number): 'resubscribe' | 'reconnect' {
  if (lastResyncAt === 0) return 'resubscribe'
  return now - lastResyncAt < resyncWindowMS ? 'reconnect' : 'resubscribe'
}

interface StreamFrame {
  type: string
  id?: string
  eventType?: 'ADDED' | 'MODIFIED' | 'DELETED'
  object?: ResourceItem
  reason?: string
  code?: string
  message?: string
}

function mergeItems(
  old: ResourceItem[],
  incoming: ResourceItem,
  eventType: string,
): ResourceItem[] {
  const key = (item: ResourceItem) =>
    item.metadata.uid ?? `${item.metadata.namespace ?? ''}/${item.metadata.name}`
  const incomingKey = key(incoming)
  if (eventType === 'DELETED') return old.filter((item) => key(item) !== incomingKey)
  const index = old.findIndex((item) => key(item) === incomingKey)
  if (index === -1) return [...old, incoming]
  const next = old.slice()
  next[index] = {
    ...old[index],
    ...incoming,
    metadata: { ...old[index].metadata, ...incoming.metadata },
  }
  return next
}

/** useResourceRealtime 维护单条 WS，并按订阅 id 将增量合并进列表缓存。 */
export function useResourceRealtime({
  cluster,
  gvr,
  namespace,
  labelSelector,
  queryKey,
  resourceVersion,
  enabled = true,
}: RealtimeOptions) {
  const queryClient = useQueryClient()
  const rvRef = useRef(resourceVersion)

  useEffect(() => {
    rvRef.current = resourceVersion
  }, [resourceVersion])

  const group = gvr.group
  const version = gvr.version
  const resource = gvr.resource

  useEffect(() => {
    if (!enabled || !cluster) return
    const subscriptionID = `${group}/${version}/${resource}/${namespace ?? ''}`
    let socket: WebSocket | undefined
    let closed = false
    let timer: number | undefined
    let stableTimer: number | undefined
    let attempts = 0
    let lastResyncAt = 0

    const sendSubscribe = (rv: string) => {
      if (socket?.readyState !== WebSocket.OPEN) return
      socket.send(
        JSON.stringify(
          buildSubscribeFrame(
            { id: subscriptionID, gvr: { group, version, resource }, namespace, labelSelector },
            rv,
          ),
        ),
      )
    }

    const connect = () => {
      if (closed) return
      const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
      socket = new WebSocket(
        `${scheme}://${window.location.host}/api/v1/clusters/${encodeURIComponent(cluster)}/stream`,
      )

      socket.onopen = () => {
        // 只有连接稳定一段时间才重置退避；服务端接受后立即断开时仍会继续退避。
        stableTimer = window.setTimeout(() => {
          attempts = 0
        }, 5_000)
        sendSubscribe(rvRef.current ?? '')
      }

      socket.onmessage = (event) => {
        let frame: StreamFrame
        try {
          frame = JSON.parse(event.data as string) as StreamFrame
        } catch {
          return
        }
        if (frame.type === 'event' && frame.object) {
          if (frame.object.metadata.resourceVersion) {
            rvRef.current = frame.object.metadata.resourceVersion
          }
          queryClient.setQueryData<ListResponse>(queryKey, (old) => {
            if (!old) return old
            return {
              ...old,
              items: mergeItems(old.items, frame.object!, frame.eventType ?? 'MODIFIED'),
            }
          })
          return
        }
        if (frame.type === 'resync') {
          const now = Date.now()
          if (resyncAction(lastResyncAt, now) === 'reconnect') {
            // 服务端连续要求重新同步，说明当前连接已无法保证顺序；交给重连逻辑重建。
            closed = true
            socket?.close()
            return
          }
          lastResyncAt = now
          rvRef.current = undefined
          void queryClient.invalidateQueries({ queryKey })
          // 服务端在发送 resync 后会移除旧订阅，客户端必须用空版本重新订阅。
          sendSubscribe('')
          return
        }
        if (frame.type === 'error' && frame.code === 'FORBIDDEN') {
          closed = true
          socket?.close()
        }
      }

      socket.onclose = () => {
        if (stableTimer) window.clearTimeout(stableTimer)
        if (closed) return
        attempts += 1
        // 指数退避 + 抖动，避免服务端抖动时所有页面同时重连。
        const base = Math.min(30_000, 500 * 2 ** Math.min(attempts, 6))
        const delay = Math.round(base * (0.75 + Math.random() * 0.5))
        timer = window.setTimeout(connect, delay)
      }
    }

    connect()
    return () => {
      closed = true
      if (timer) window.clearTimeout(timer)
      if (stableTimer) window.clearTimeout(stableTimer)
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'unsubscribe', id: subscriptionID }))
      }
      socket?.close()
    }
  }, [
    cluster,
    group,
    version,
    resource,
    namespace,
    labelSelector,
    enabled,
    queryClient,
    queryKey,
  ])
}
