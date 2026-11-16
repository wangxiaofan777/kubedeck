import { useMemo } from 'react'
import {
  preferredResourceType,
  resolveType,
  type ResourceType,
} from './resourceTypes'
import { useApiResources } from './useApiResources'

/**
 * useResourceType 返回当前集群真正可用的资源类型：静态表优先，旧集群自动回退到 beta API。
 * 资源发现失败时保持静态定义，由具体请求决定是否报错。
 */
export function useResourceType(
  cluster: string | undefined,
  typeKey: string | undefined,
): ResourceType | undefined {
  const base = resolveType(typeKey)
  const discovery = useApiResources(cluster)
  return useMemo(
    () => (base ? preferredResourceType(base, discovery.data?.resources) : undefined),
    [base, discovery.data],
  )
}