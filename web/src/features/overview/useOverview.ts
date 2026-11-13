import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'

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