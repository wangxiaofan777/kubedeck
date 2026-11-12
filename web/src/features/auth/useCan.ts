import { useQuery } from '@tanstack/react-query'
import { api, type GVR } from '../../lib/api'

export type Verb =
  | 'get'
  | 'list'
  | 'create'
  | 'update'
  | 'patch'
  | 'delete'
  | 'deletecollection'
  | 'scale'
  | 'restart'
  | 'rollback'
  | 'evict'

/** useCan 复用服务端 SSAR 预检；缓存 25 秒，满足 60 秒内生效的约束。 */
export function useCan(params: {
  cluster?: string
  gvr: GVR
  namespace?: string
  verb: Verb
  name?: string
  enabled?: boolean
}) {
  const { cluster, gvr, namespace, verb, name, enabled = true } = params
  const query = useQuery({
    queryKey: ['can', cluster, gvr.group, gvr.version, gvr.resource, namespace, verb, name],
    queryFn: ({ signal }) =>
      api.can(
        [
          {
            cluster: cluster!,
            namespace: namespace || undefined,
            verb,
            group: gvr.group,
            version: gvr.version,
            resource: gvr.resource,
            name,
          },
        ],
        signal,
      ),
    enabled: enabled && !!cluster,
    staleTime: 25_000,
  })
  const result = query.data?.results?.[0]
  return {
    allowed: result?.allowed ?? false,
    reason: result?.reason,
    loading: query.isLoading,
    refetch: query.refetch,
  }
}
