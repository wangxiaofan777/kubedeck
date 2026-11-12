import { useEffect, useRef } from 'react'
import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import type { GVR, ListResponse, ResourceItem } from '../lib/api'

interface RealtimeOptions {
  cluster?: string
  gvr: GVR
  namespace?: string
  queryKey: QueryKey
  resourceVersion?: string
  enabled?: boolean
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
  queryKey,
  resourceVersion,
  enabled = true,
}: RealtimeOptions) {
  const queryClient = useQueryClient()
  const rvRef = useRef(resourceVersion)

  useEffect(() => {
    rvRef.current = resourceVersion
  }, [resourceVersion])

  useEffect(() => {
    if (!enabled || !cluster) return
    const subscriptionID = `${gvr.group}/${gvr.version}/${gvr.resource}/${namespace ?? ''}`
    let socket: WebSocket | undefined
    let closed = false
    let timer: number | undefined
    let attempts = 0

    const connect = () => {
      if (closed) return
      const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
      socket = new WebSocket(
        `${scheme}://${window.location.host}/api/v1/clusters/${encodeURIComponent(cluster)}/stream`,
      )

      socket.onopen = () => {
        attempts = 0
        socket?.send(
          JSON.stringify({
            type: 'subscribe',
            id: subscriptionID,
            group: gvr.group,
            version: gvr.version,
            resource: gvr.resource,
            namespace,
            resourceVersion: rvRef.current ?? '',
          }),
        )
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
          void queryClient.invalidateQueries({ queryKey })
          return
        }
        if (frame.type === 'error' && frame.code === 'FORBIDDEN') {
          closed = true
          socket?.close()
        }
      }

      socket.onclose = () => {
        if (closed) return
        attempts += 1
        const delay = Math.min(30_000, 500 * 2 ** Math.min(attempts, 6))
        timer = window.setTimeout(connect, delay)
      }
    }

    connect()
    return () => {
      closed = true
      if (timer) window.clearTimeout(timer)
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'unsubscribe', id: subscriptionID }))
      }
      socket?.close()
    }
  }, [cluster, gvr.group, gvr.version, gvr.resource, namespace, enabled, queryClient, queryKey])
}
