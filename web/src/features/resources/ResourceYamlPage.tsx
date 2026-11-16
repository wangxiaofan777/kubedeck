import { lazy, Suspense, useEffect, useState } from 'react'
import { Alert, App, Button, Card, Space, Spin, Typography } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { parse } from 'yaml'
import { api } from '../../lib/api'
import { PageSkeleton } from '../../components/StateViews'
import { useResourceDetail } from './ResourceDetailLayout'
import { clearYamlDraft, loadYamlDraft, saveYamlDraft, yamlDraftKey } from './yamlDraft'

const YamlCodeEditor = lazy(() => import('./YamlCodeEditor'))

export default function ResourceYamlPage() {
  const { cluster, type, namespace, name, updateCan } = useResourceDetail()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [code, setCode] = useState('')
  const [parseError, setParseError] = useState<string>()
  const [initialized, setInitialized] = useState(false)

  const yamlQuery = useQuery({
    queryKey: ['yaml', cluster, type.gvr.group, type.gvr.version, type.gvr.resource, namespace, name],
    queryFn: ({ signal }) => api.yaml({ cluster, gvr: type.gvr, namespace, name, signal }),
  })

  const draftKey = yamlDraftKey(cluster, type.gvr, namespace, name)

  useEffect(() => {
    // 同一路由切换到另一个对象时，重置本地编辑状态。
    setInitialized(false)
    setCode('')
    setParseError(undefined)
  }, [draftKey])

  useEffect(() => {
    if (initialized) return
    const draft = loadYamlDraft(draftKey)
    if (draft) {
      setCode(draft.code)
      setInitialized(true)
      return
    }
    if (yamlQuery.data !== undefined) {
      setCode(yamlQuery.data)
      setInitialized(true)
    }
  }, [yamlQuery.data, draftKey, initialized])

  const compare = () => {
    try {
      parse(code)
      setParseError(undefined)
    } catch (error) {
      setParseError(error instanceof Error ? error.message : 'YAML 解析失败')
      return
    }
    if (yamlQuery.data !== undefined && code === yamlQuery.data) {
      message.info('当前内容没有修改，无需比较')
      return
    }
    saveYamlDraft(draftKey, code)
    navigate('../diff')
  }

  const reset = () => {
    if (yamlQuery.data === undefined) return
    setCode(yamlQuery.data)
    setParseError(undefined)
    clearYamlDraft(draftKey)
    message.success('已恢复为服务端最新内容')
  }

  if (yamlQuery.isLoading) return <PageSkeleton rows={8} />
  if (yamlQuery.error) {
    return (
      <Alert
        type="error"
        showIcon
        message="YAML 加载失败"
        description={yamlQuery.error instanceof Error ? yamlQuery.error.message : '请稍后重试'}
        action={
          <Button size="small" onClick={() => void yamlQuery.refetch()}>
            重试
          </Button>
        }
      />
    )
  }

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {!updateCan.loading && !updateCan.allowed && (
        <Alert
          type="info"
          showIcon
          message="当前账号对资源只有只读权限"
          description="可以查看 YAML，但不能修改或提交。"
        />
      )}
      {parseError && <Alert type="error" showIcon message={`YAML 语法错误：${parseError}`} />}

      <Card
        size="small"
        title="YAML 编辑"
        extra={<span className="kd-muted" style={{ fontSize: 11 }}>提交前需进入变更比较页</span>}
      >
        <Suspense
          fallback={
            <div style={{ display: 'grid', placeItems: 'center', minHeight: 320 }}>
              <Spin />
            </div>
          }
        >
          <YamlCodeEditor
            value={code}
            readOnly={!updateCan.allowed}
            height={520}
            onChange={(value) => {
              setCode(value)
              setParseError(undefined)
            }}
          />
        </Suspense>
      </Card>

      <Space wrap>
        <Button
          type="primary"
          onClick={compare}
          disabled={!updateCan.allowed || !!parseError || yamlQuery.isFetching}
        >
          比较变更
        </Button>
        <Button onClick={reset} disabled={!updateCan.allowed || yamlQuery.isFetching}>
          放弃修改
        </Button>
        <Typography.Text type="secondary">
          比较页会执行 dryRun，并展示不可变字段提示与冲突处理。
        </Typography.Text>
      </Space>
    </Space>
  )
}