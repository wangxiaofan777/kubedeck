import { Alert } from 'antd'
import { ApiError } from '../lib/api'

interface Props {
  error: unknown
}

/** 把任意查询/请求错误归一化为可读的告警。 */
export default function ErrorAlert({ error }: Props) {
  if (!error) return null

  if (error instanceof ApiError) {
    return (
      <Alert
        type="error"
        showIcon
        style={{ marginBottom: 16 }}
        message={`${error.code}: ${error.message}`}
        description={error.requestId ? `requestId: ${error.requestId}` : undefined}
      />
    )
  }

  const message = error instanceof Error ? error.message : '请求失败'
  return <Alert type="error" showIcon style={{ marginBottom: 16 }} message={message} />
}
