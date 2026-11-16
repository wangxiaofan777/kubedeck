import type { ReactNode } from 'react'
import { Alert, Button, Empty, Skeleton } from 'antd'
import { ApiError } from '../lib/api'

/** ErrorState 统一错误展示：错误码、requestId 与重试。 */
export function ErrorState({
  error,
  title = '加载失败',
  onRetry,
  extra,
}: {
  error: unknown
  title?: string
  onRetry?: () => void
  extra?: ReactNode
}) {
  const apiError = error instanceof ApiError ? error : undefined
  const message = error instanceof Error ? error.message : '请稍后重试'
  return (
    <Alert
      type="error"
      showIcon
      message={
        <span>
          {title}
          {apiError?.code ? <span className="kd-muted"> · {apiError.code}</span> : null}
        </span>
      }
      description={
        <div>
          <div>{message}</div>
          {apiError?.requestId && (
            <div className="kd-mono kd-muted" style={{ marginTop: 4 }}>
              requestId: {apiError.requestId}
            </div>
          )}
          {extra}
        </div>
      }
      action={
        onRetry ? (
          <Button size="small" onClick={onRetry}>
            重试
          </Button>
        ) : undefined
      }
    />
  )
}

/** EmptyState 统一空态：说明 + 可执行动作。 */
export function EmptyState({
  description,
  action,
  simple = true,
}: {
  description: ReactNode
  action?: ReactNode
  simple?: boolean
}) {
  return (
    <div style={{ padding: '40px 16px', textAlign: 'center' }}>
      <Empty
        image={simple ? Empty.PRESENTED_IMAGE_SIMPLE : undefined}
        description={description}
      >
        {action}
      </Empty>
    </div>
  )
}

/** PageSkeleton 用于首屏，避免整页 Spin。 */
export function PageSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="kd-card kd-card-pad">
      <Skeleton active paragraph={{ rows }} />
    </div>
  )
}

export function InlineError({ error }: { error: unknown }) {
  if (!error) return null
  return <ErrorState error={error} title="请求失败" />
}