import type { ReactNode } from 'react'
import type { GVR } from '../../lib/api'
import { useCan, type Verb } from './useCan'

interface Props {
  cluster?: string
  gvr: GVR
  namespace?: string
  name?: string
  verb: Verb
  enabled?: boolean
  /** 预检未返回时渲染的内容；默认不渲染，调用方通常传禁用态按钮。 */
  pending?: ReactNode
  /** 明确无权限时渲染的内容，默认不渲染。 */
  denied?: ReactNode
  children: (state: { allowed: boolean; loading: boolean; reason?: string }) => ReactNode
}

/**
 * Can 将 SSAR 预检结果映射为渲染分支：加载中展示 pending，明确无权限展示 denied，
 * 只有 allowed 时才渲染 children。
 */
export default function Can({
  cluster,
  gvr,
  namespace,
  name,
  verb,
  enabled = true,
  pending = null,
  denied = null,
  children,
}: Props) {
  const access = useCan({ cluster, gvr, namespace, name, verb, enabled })
  if (access.loading) return <>{pending}</>
  if (!access.allowed) return <>{denied}</>
  return <>{children(access)}</>
}
