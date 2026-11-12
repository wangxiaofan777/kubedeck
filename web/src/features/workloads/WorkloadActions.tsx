import { useEffect, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  InputNumber,
  List,
  Modal,
  Popconfirm,
  Progress,
  Space,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  PauseCircleOutlined,
  PlayCircleOutlined,
  RedoOutlined,
  SyncOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type GVR, type ResourceItem, type Revision } from '../../lib/api'
import { useCan } from '../auth/useCan'

interface Props {
  cluster: string
  gvr: GVR
  namespace?: string
  item: ResourceItem
  onChanged: () => void
}

export default function WorkloadActions({ cluster, gvr, namespace, item, onChanged }: Props) {
  const name = item.metadata.name
  const queryClient = useQueryClient()
  const [operationId, setOperationId] = useState<string>()
  const [scaleOpen, setScaleOpen] = useState(false)
  const [replicas, setReplicas] = useState(1)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [selectedRevision, setSelectedRevision] = useState<Revision>()
  const [diff, setDiff] = useState('')
  const [busy, setBusy] = useState(false)

  const scaleCan = useCan({
    cluster,
    gvr,
    namespace,
    verb: 'scale',
    enabled: supportsScaleResource(gvr),
  })
  const restartCan = useCan({
    cluster,
    gvr,
    namespace,
    verb: 'restart',
    enabled: supportsRestartResource(gvr),
  })
  const updateCan = useCan({
    cluster,
    gvr,
    namespace,
    verb: 'update',
    enabled: supportsRollbackResource(gvr),
  })

  const paused = item.spec?.paused === true
  const currentReplicas =
    typeof item.spec?.replicas === 'number' ? (item.spec.replicas as number) : 1

  const historyQuery = useQuery({
    queryKey: ['history', cluster, gvr.resource, namespace, name],
    queryFn: () => api.history({ cluster, gvr, namespace, name }),
    enabled: historyOpen && supportsRollbackResource(gvr),
  })

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['detail'] })
    onChanged()
  }

  const doScale = async () => {
    setBusy(true)
    try {
      const result = await api.scale({ cluster, gvr, namespace, name, replicas })
      setOperationId(result.operationId)
      setScaleOpen(false)
      message.success('伸缩请求已受理')
      await refresh()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '伸缩失败')
    } finally {
      setBusy(false)
    }
  }

  const doRestart = async () => {
    setBusy(true)
    try {
      const result = await api.restart({ cluster, gvr, namespace, name })
      setOperationId(result.operationId)
      message.success('重启请求已受理')
      await refresh()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '重启失败')
    } finally {
      setBusy(false)
    }
  }

  const doPause = async (next: boolean) => {
    setBusy(true)
    try {
      if (next) await api.pause({ cluster, gvr, namespace, name })
      else await api.resume({ cluster, gvr, namespace, name })
      message.success(next ? '已暂停滚动更新' : '已恢复滚动更新')
      await refresh()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  const previewRevision = async (revision: Revision) => {
    setSelectedRevision(revision)
    setBusy(true)
    try {
      const result = await api.rollbackDiff({
        cluster,
        gvr,
        namespace,
        name,
        revision: revision.revision,
      })
      setDiff(result.diff)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '获取差异失败')
      setDiff('')
    } finally {
      setBusy(false)
    }
  }

  const doRollback = async () => {
    if (!selectedRevision) return
    setBusy(true)
    try {
      await api.rollback({
        cluster,
        gvr,
        namespace,
        name,
        revision: selectedRevision.revision,
      })
      message.success(`已回滚到 revision ${selectedRevision.revision}`)
      setHistoryOpen(false)
      setSelectedRevision(undefined)
      setDiff('')
      await refresh()
    } catch (error) {
      if (error instanceof ApiError && error.code === 'UNPROCESSABLE') {
        message.error('目标 revision 已被清理，无法回滚')
      } else {
        message.error(error instanceof Error ? error.message : '回滚失败')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <Space wrap>
        {supportsScaleResource(gvr) && (scaleCan.loading || scaleCan.allowed) && (
          <Button
            icon={<ThunderboltOutlined />}
            disabled={scaleCan.loading}
            onClick={() => {
              setReplicas(currentReplicas)
              setScaleOpen(true)
            }}
          >
            伸缩
          </Button>
        )}
        {supportsRestartResource(gvr) && (restartCan.loading || restartCan.allowed) && (
          <Popconfirm
            title="触发滚动重启"
            description="将更新 Pod 模板注解，由控制器滚动替换 Pod。"
            onConfirm={() => void doRestart()}
            disabled={restartCan.loading}
          >
            <Button icon={<RedoOutlined />} loading={busy} disabled={restartCan.loading}>
              重启
            </Button>
          </Popconfirm>
        )}
        {supportsRollbackResource(gvr) && (updateCan.loading || updateCan.allowed) && (
          <Button
            icon={<SyncOutlined />}
            disabled={updateCan.loading}
            onClick={() => setHistoryOpen(true)}
          >
            历史与回滚
          </Button>
        )}
        {gvr.resource === 'deployments' &&
          (updateCan.loading || updateCan.allowed) &&
          (paused ? (
            <Button
              icon={<PlayCircleOutlined />}
              loading={busy}
              disabled={updateCan.loading}
              onClick={() => void doPause(false)}
            >
              恢复滚动更新
            </Button>
          ) : (
            <Popconfirm
              title="暂停滚动更新"
              description="暂停期间变更不会自动推进。"
              onConfirm={() => void doPause(true)}
              disabled={updateCan.loading}
            >
              <Button icon={<PauseCircleOutlined />} loading={busy} disabled={updateCan.loading}>
                暂停滚动更新
              </Button>
            </Popconfirm>
          ))}
      </Space>

      {paused && (
        <Alert
          type="warning"
          showIcon
          style={{ marginTop: 12 }}
          message="当前 Deployment 处于暂停状态，变更不会自动推进。"
        />
      )}

      {operationId && (
        <OperationProgress cluster={cluster} operationId={operationId} onDone={onChanged} />
      )}

      <Modal
        open={scaleOpen}
        title="调整副本数"
        okText="提交"
        confirmLoading={busy}
        onOk={() => void doScale()}
        onCancel={() => setScaleOpen(false)}
      >
        <Space direction="vertical" style={{ width: '100%' }}>
          <InputNumber
            min={0}
            max={100000}
            value={replicas}
            onChange={(value) => setReplicas(value ?? 0)}
            style={{ width: '100%' }}
          />
          {replicas === 0 && (
            <Alert
              type="warning"
              showIcon
              message="缩容到 0 将停止所有副本，请确认这是预期操作。"
            />
          )}
        </Space>
      </Modal>

      <Modal
        open={historyOpen}
        title="历史版本与回滚"
        width={900}
        okText="回滚到所选版本"
        okButtonProps={{ disabled: !selectedRevision, loading: busy }}
        onOk={() => void doRollback()}
        onCancel={() => {
          setHistoryOpen(false)
          setSelectedRevision(undefined)
          setDiff('')
        }}
      >
        <Typography.Paragraph type="secondary">
          当前 revision：{historyQuery.data?.currentRevision ?? '-'}
        </Typography.Paragraph>
        <List
          loading={historyQuery.isFetching}
          dataSource={historyQuery.data?.revisions ?? []}
          locale={{ emptyText: '没有可用的历史版本' }}
          renderItem={(revision) => (
            <List.Item
              style={{
                cursor: 'pointer',
                background:
                  selectedRevision?.revision === revision.revision ? '#f0f7ff' : undefined,
              }}
              onClick={() => void previewRevision(revision)}
            >
              <List.Item.Meta
                title={
                  <Space>
                    <Tag>revision {revision.revision}</Tag>
                    {revision.revision === historyQuery.data?.currentRevision && (
                      <Tag color="green">当前</Tag>
                    )}
                  </Space>
                }
                description={
                  <span>
                    {revision.images.join(', ')} · {revision.readyReplicas}/{revision.replicas}{' '}
                    ready
                  </span>
                }
              />
            </List.Item>
          )}
        />
        {diff && (
          <pre
            style={{
              maxHeight: 260,
              overflow: 'auto',
              background: '#fafafa',
              border: '1px solid #f0f0f0',
              borderRadius: 6,
              padding: 12,
              fontSize: 12,
            }}
          >
            {diff}
          </pre>
        )}
      </Modal>
    </div>
  )
}

export function OperationProgress({
  cluster,
  operationId,
  onDone,
}: {
  cluster: string
  operationId: string
  onDone?: () => void
}) {
  const query = useQuery({
    queryKey: ['operation', cluster, operationId],
    queryFn: ({ signal }) => api.operation(cluster, operationId, signal),
    refetchInterval: (query) => (query.state.data?.concluded ? false : 2000),
  })
  const operation = query.data
  const concluded = operation?.concluded === true
  const onDoneRef = useRef(onDone)
  onDoneRef.current = onDone
  useEffect(() => {
    if (concluded) onDoneRef.current?.()
  }, [concluded])
  if (!operation) return null
  const percent =
    operation.targetReplicas === 0
      ? operation.concluded
        ? 100
        : 50
      : Math.min(100, Math.round((operation.readyReplicas / operation.targetReplicas) * 100))
  return (
    <Card size="small" style={{ marginTop: 12 }} title="操作进度">
      <Space direction="vertical" style={{ width: '100%' }}>
        <Progress
          percent={percent}
          status={operation.concluded && !operation.success ? 'exception' : undefined}
        />
        <Descriptions size="small" column={4}>
          <Descriptions.Item label="目标副本">{operation.targetReplicas}</Descriptions.Item>
          <Descriptions.Item label="当前副本">{operation.currentReplicas}</Descriptions.Item>
          <Descriptions.Item label="就绪副本">{operation.readyReplicas}</Descriptions.Item>
          <Descriptions.Item label="耗时">
            {(operation.elapsedMs / 1000).toFixed(1)}s
          </Descriptions.Item>
        </Descriptions>
        {operation.message && <Alert type="warning" showIcon message={operation.message} />}
        {operation.concluded && operation.success && (
          <Alert type="success" showIcon message="操作已收敛" />
        )}
      </Space>
    </Card>
  )
}

function supportsScaleResource(gvr: GVR): boolean {
  return (
    gvr.group === 'apps' && ['deployments', 'statefulsets', 'replicasets'].includes(gvr.resource)
  )
}

function supportsRestartResource(gvr: GVR): boolean {
  return (
    gvr.group === 'apps' && ['deployments', 'statefulsets', 'daemonsets'].includes(gvr.resource)
  )
}

function supportsRollbackResource(gvr: GVR): boolean {
  return gvr.group === 'apps' && gvr.resource === 'deployments'
}
