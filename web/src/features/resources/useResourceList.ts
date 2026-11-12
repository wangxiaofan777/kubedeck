import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'
import type { ResourceType } from './resourceTypes'

/** 资源列表：15 秒刷新；切换类型/命名空间时保留上一页数据避免闪烁。 */
export function useResourceList(
  cluster: string | undefined,
  type: ResourceType,
  namespace: string,
) {
  return useQuery({
    queryKey: ['resources', cluster, type.key, namespace],
    queryFn: ({ signal }) => api.list({ cluster: cluster!, gvr: type.gvr, namespace, signal }),
    enabled: !!cluster,
    refetchInterval: 15_000,
    placeholderData: keepPreviousData,
  })
}
