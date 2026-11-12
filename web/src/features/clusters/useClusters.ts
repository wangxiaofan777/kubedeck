import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'

/** 集群列表：30 秒刷新一次，保持顶栏状态与版本新鲜。 */
export function useClusters() {
  return useQuery({
    queryKey: ['clusters'],
    queryFn: ({ signal }) => api.clusters(signal),
    refetchInterval: 30_000,
    // 集群列表刷新失败时保留上一次结果，避免顶栏与导航整体消失。
    placeholderData: keepPreviousData,
  })
}
