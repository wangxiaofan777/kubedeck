import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type ClusterUsage } from '../../lib/api'

/** 总览：30 秒自动刷新，页面不可见时暂停轮询，避免后台标签页持续打 apiserver。 */
export function useOverview(cluster: string | undefined) {
  const [visible, setVisible] = useState(() => document.visibilityState === 'visible')

  useEffect(() => {
    const onChange = () => setVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', onChange)
    return () => document.removeEventListener('visibilitychange', onChange)
  }, [])

  return useQuery({
    queryKey: ['overview', cluster],
    queryFn: ({ signal }) => api.overview(cluster as string, signal),
    enabled: Boolean(cluster),
    refetchInterval: visible ? 30_000 : false,
  })
}

export interface UsageSample {
  fetchedAt: string
  cpuMilli: number
  memoryBytes: number
}

/** 会话内最多保留 20 个采样点（30 秒一个，约 10 分钟）。 */
const MAX_USAGE_SAMPLES = 20

/**
 * 累积本次会话的用量趋势。服务端不保存历史，切换集群或刷新页面后重新开始；
 * 同一个 fetchedAt 只记录一次，避免手动刷新造成重复点。
 */
export function useUsageTrend(cluster: string | undefined, usage: ClusterUsage | null | undefined) {
  const [samples, setSamples] = useState<UsageSample[]>([])
  const lastSampleAt = useRef<string | null>(null)

  useEffect(() => {
    lastSampleAt.current = null
    setSamples([])
  }, [cluster])

  useEffect(() => {
    if (!usage?.available) {
      lastSampleAt.current = null
      setSamples([])
      return
    }
    if (lastSampleAt.current === usage.fetchedAt) return
    lastSampleAt.current = usage.fetchedAt
    setSamples((previous) =>
      [
        ...previous,
        {
          fetchedAt: usage.fetchedAt,
          cpuMilli: usage.cpuMilli,
          memoryBytes: usage.memoryBytes,
        },
      ].slice(-MAX_USAGE_SAMPLES),
    )
  }, [usage])

  return samples
}