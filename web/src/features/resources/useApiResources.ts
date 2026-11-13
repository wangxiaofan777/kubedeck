import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'

/** 资源发现按集群缓存 5 分钟，供创建弹窗解析 YAML 目标类型。 */
export function useApiResources(cluster: string | undefined) {
  return useQuery({
    queryKey: ['api-resources', cluster],
    queryFn: ({ signal }) => api.apiResources(cluster as string, signal),
    enabled: Boolean(cluster),
    staleTime: 5 * 60_000,
  })
}