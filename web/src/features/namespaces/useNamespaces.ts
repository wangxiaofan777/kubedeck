import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api'

/** 当前集群的命名空间列表。 */
export function useNamespaces(cluster?: string) {
  return useQuery({
    queryKey: ['namespaces', cluster],
    queryFn: ({ signal }) => api.namespaces(cluster!, signal),
    enabled: !!cluster,
  })
}
