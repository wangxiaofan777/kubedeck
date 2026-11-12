import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'
import type { ResourceType } from './resourceTypes'

export interface ResourceListParams {
  namespace: string
  labelSelector?: string
  fieldSelector?: string
  continueToken?: string
  limit?: number
  sortBy?: string
  order?: string
}

/** 资源列表：15 秒兜底刷新；实时增量由 WebSocket 合并。 */
export function useResourceList(
  cluster: string | undefined,
  type: ResourceType,
  params: ResourceListParams,
) {
  return useQuery({
    queryKey: [
      'resources',
      cluster,
      type.key,
      params.namespace,
      params.labelSelector ?? '',
      params.fieldSelector ?? '',
      params.continueToken ?? '',
      params.limit ?? 200,
      params.sortBy ?? '',
      params.order ?? '',
    ],
    queryFn: ({ signal }) =>
      api.list({
        cluster: cluster!,
        gvr: type.gvr,
        namespace: params.namespace,
        labelSelector: params.labelSelector,
        fieldSelector: params.fieldSelector,
        continueToken: params.continueToken,
        limit: params.limit,
        sortBy: params.sortBy,
        order: params.order,
        signal,
      }),
    enabled: !!cluster,
    refetchInterval: 15_000,
    placeholderData: keepPreviousData,
  })
}
