import { lazy, Suspense, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Input,
  Modal,
  Popconfirm,
  Result,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd'
import { ArrowLeftOutlined, DeleteOutlined, LogoutOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type EventItem } from '../../lib/api'
import { ageOf, statusColor } from '../../lib/format'
import ErrorAlert from '../../components/ErrorAlert'
import Can from '../auth/Can'
import { useCan } from '../auth/useCan'
import { resolveType, statusOf, RESOURCE_TYPES } from '../resources/resourceTypes'
const YamlEditor = lazy(() => import('../resources/YamlEditor'))
import LogViewer from '../logs/LogViewer'
import WorkloadActions from '../workloads/WorkloadActions'

export default function ResourceDetailPage() {
  const { cluster, typeKey, namespace: nsParam, name } = useParams()
  const namespace = nsParam === '_' ? undefined : nsParam
  const type = resolveType(typeKey) ?? RESOURCE_TYPES[0]
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [evicting, setEvicting] = useState(false)

  const detailQuery = useQuery({
    queryKey: ['detail', cluster, type.key, namespace, name],
    queryFn: ({ signal }) =>
      api.detail({ cluster: cluster!, gvr: type.gvr, namespace, name: name!, signal }),
    enabled: !!cluster && !!name,
  })

  const eventsQuery = useQuery({
    queryKey: ['events', cluster, namespace, type.kind, name],
    queryFn: ({ signal }) =>
      api.events(cluster!, {
        namespace,
        kind: type.kind,
        name,
        signal,
      }),
    enabled: !!cluster && !!name,
  })

  const updateCan = useCan({ cluster, gvr: type.gvr, namespace, verb: 'update' })

  const item = detailQuery.data?.object
  const containers = useMemo(() => {
    const list = item?.spec?.containers
    return Array.isArray(list)
      ? (list as { name?: string }[]).map((c) => c.name ?? '').filter(Boolean)
      : []
  }, [item])

  const backToList = () => {
    const suffix = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
    navigate(`/clusters/${encodeURIComponent(cluster!)}/${type.key}${suffix}`)
  }

  const remove = async () => {
    if (!cluster || !name) return
    setDeleting(true)
    try {
      await api.remove({
        cluster,
        gvr: type.gvr,
        namespace,
        name,
        propagationPolicy: 'Background',
      })
      message.success('已删除')
      setDeleteOpen(false)
      await queryClient.invalidateQueries({ queryKey: ['resources'] })
      backToList()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '删除失败')
    } finally {
      setDeleting(false)
    }
  }

  const evict = async () => {
    if (!cluster || !namespace || !name) return
    setEvicting(true)
    try {
      await api.evict({ cluster, namespace, name })
      message.success('驱逐请求已受理')
      await detailQuery.refetch()
    } catch (error) {
      if (error instanceof ApiError) {
        const details = (error.details ?? {}) as { pdbName?: string; allowedDisruptions?: number }
        Modal.error({
          title: '驱逐被阻止',
          content: (
            <div>
              <p>{error.message}</p>
              {details.pdbName && (
                <p>
                  PodDisruptionBudget：<b>{details.pdbName}</b>，当前允许中断数：
                  <b>{details.allowedDisruptions ?? 0}</b>
                </p>
              )}
            </div>
          ),
        })
      } else {
        message.error(error instanceof Error ? error.message : '驱逐失败')
      }
    } finally {
      setEvicting(false)
    }
  }

  if (detailQuery.error instanceof ApiError && detailQuery.error.code === 'FORBIDDEN') {
    return <Result status="403" title="无访问权限" subTitle="当前账号无权查看该资源。" />
  }
  if (detailQuery.error instanceof ApiError && detailQuery.error.code === 'NOT_FOUND') {
    return <Result status="404" title="资源不存在" subTitle="对象可能已被删除。" />
  }

  if (detailQuery.isLoading) {
    return (
      <div style={{ padding: 64, textAlign: 'center' }}>
        <Spin size="large" />
      </div>
    )
  }
  if (!item) {
    return (
      <>
        <ErrorAlert error={detailQuery.error} />
        <Empty description="无法加载资源" />
      </>
    )
  }

  const status = statusOf(item, type)
  const ownerRefs = detailQuery.data?.ownerReferences ?? item.metadata.ownerReferences ?? []
  const requiresTypedConfirm = type.key === 'namespaces' || type.key === 'persistentvolumes'

  const overview = (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Descriptions column={2} size="small" bordered>
        <Descriptions.Item label="名称">{item.metadata.name}</Descriptions.Item>
        <Descriptions.Item label="命名空间">
          {item.metadata.namespace ?? '集群级'}
        </Descriptions.Item>
        <Descriptions.Item label="状态">
          <Tag color={statusColor(status)}>{status}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="创建时间">
          {item.metadata.creationTimestamp
            ? `${new Date(item.metadata.creationTimestamp).toLocaleString()} (${ageOf(item.metadata.creationTimestamp)})`
            : '-'}
        </Descriptions.Item>
        <Descriptions.Item label="资源版本" span={2}>
          {item.metadata.resourceVersion ?? '-'}
        </Descriptions.Item>
        <Descriptions.Item label="标签" span={2}>
          {Object.entries(item.metadata.labels ?? {}).length === 0
            ? '-'
            : Object.entries(item.metadata.labels ?? {}).map(([key, value]) => (
                <Tag key={key} style={{ marginBottom: 4 }}>
                  {key}={value}
                </Tag>
              ))}
        </Descriptions.Item>
        <Descriptions.Item label="Owner 引用" span={2}>
          {ownerRefs.length === 0
            ? '-'
            : ownerRefs.map((ref) => `${ref.kind}/${ref.name}`).join(', ')}
        </Descriptions.Item>
      </Descriptions>

      {type.key === 'pods' && Array.isArray(item.spec?.containers) && (
        <Card title="容器">
          <Table
            rowKey={(row) => (row as { name?: string }).name ?? ''}
            size="small"
            pagination={false}
            dataSource={item.spec.containers as Record<string, unknown>[]}
            columns={[
              { title: '名称', dataIndex: 'name' },
              { title: '镜像', dataIndex: 'image', ellipsis: true },
              {
                title: '就绪',
                key: 'ready',
                render: (_, row) => {
                  const statuses = Array.isArray(item.status?.containerStatuses)
                    ? (item.status.containerStatuses as {
                        name?: string
                        ready?: boolean
                        restartCount?: number
                      }[])
                    : []
                  const found = statuses.find((s) => s.name === (row as { name?: string }).name)
                  return found?.ready ? <Tag color="green">Ready</Tag> : <Tag>NotReady</Tag>
                },
              },
              {
                title: '重启次数',
                key: 'restarts',
                render: (_, row) => {
                  const statuses = Array.isArray(item.status?.containerStatuses)
                    ? (item.status.containerStatuses as { name?: string; restartCount?: number }[])
                    : []
                  return (
                    statuses.find((s) => s.name === (row as { name?: string }).name)
                      ?.restartCount ?? 0
                  )
                },
              },
            ]}
          />
        </Card>
      )}

      {['deployments', 'statefulsets', 'daemonsets', 'replicasets'].includes(type.key) && (
        <WorkloadActions
          cluster={cluster!}
          gvr={type.gvr}
          namespace={namespace}
          item={item}
          onChanged={() => {
            void detailQuery.refetch()
            void eventsQuery.refetch()
          }}
        />
      )}
    </Space>
  )

  const eventColumns = [
    { title: '类型', dataIndex: 'type', width: 90 },
    { title: '原因', dataIndex: 'reason', width: 160 },
    { title: '消息', dataIndex: 'message', ellipsis: true },
    { title: '次数', dataIndex: 'count', width: 70 },
    {
      title: '最后发生',
      dataIndex: 'lastTimestamp',
      width: 190,
      render: (value: string) => (value ? new Date(value).toLocaleString() : '-'),
    },
  ]

  return (
    <div>
      <Space style={{ marginBottom: 12 }} align="center">
        <Button icon={<ArrowLeftOutlined />} onClick={backToList}>
          返回列表
        </Button>
        <Typography.Title level={4} style={{ margin: 0 }}>
          {item.metadata.name}
        </Typography.Title>
        <Tag color="blue">{type.label}</Tag>
        <Tag color={statusColor(status)}>{status}</Tag>
        {!updateCan.loading && !updateCan.allowed && <Tag>只读</Tag>}
      </Space>

      <Space wrap style={{ marginBottom: 12 }}>
        {type.key === 'pods' && (
          <Can
            cluster={cluster}
            gvr={{ group: '', version: 'v1', resource: 'pods' }}
            namespace={namespace}
            name={name}
            verb="evict"
            pending={
              <Button icon={<LogoutOutlined />} disabled>
                驱逐
              </Button>
            }
          >
            {() => (
              <Popconfirm
                title="驱逐该 Pod"
                description="将创建 Eviction 请求，受 PodDisruptionBudget 约束。"
                onConfirm={() => void evict()}
              >
                <Button icon={<LogoutOutlined />} loading={evicting}>
                  驱逐
                </Button>
              </Popconfirm>
            )}
          </Can>
        )}
        <Can
          cluster={cluster}
          gvr={type.gvr}
          namespace={namespace}
          verb="delete"
          pending={
            <Button danger icon={<DeleteOutlined />} disabled>
              删除
            </Button>
          }
        >
          {() => (
            <Button danger icon={<DeleteOutlined />} onClick={() => setDeleteOpen(true)}>
              删除
            </Button>
          )}
        </Can>
      </Space>

      <ErrorAlert error={detailQuery.error ?? eventsQuery.error} />

      {detailQuery.data?.eventsError && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="事件加载失败"
          description={detailQuery.data.eventsError}
        />
      )}

      <Tabs
        items={[
          { key: 'overview', label: '概览', children: overview },
          {
            key: 'yaml',
            label: 'YAML',
            children: (
              <Suspense fallback={<Spin />}>
                <YamlEditor
                  cluster={cluster!}
                  gvr={type.gvr}
                  namespace={namespace}
                  name={item.metadata.name}
                  canUpdate={updateCan.allowed && !updateCan.loading}
                  onSaved={() => void detailQuery.refetch()}
                />
              </Suspense>
            ),
          },
          {
            key: 'events',
            label: '事件',
            children: (
              <Table<EventItem>
                size="small"
                rowKey={(event) =>
                  `${event.reason}-${event.lastTimestamp}-${event.involvedObject?.name ?? ''}`
                }
                loading={eventsQuery.isFetching}
                dataSource={eventsQuery.data?.items ?? []}
                columns={eventColumns}
                pagination={false}
              />
            ),
          },
          ...(type.key === 'pods' && namespace
            ? [
                {
                  key: 'logs',
                  label: '日志',
                  children: (
                    <LogViewer
                      cluster={cluster!}
                      namespace={namespace}
                      pod={item.metadata.name}
                      containers={containers}
                    />
                  ),
                },
              ]
            : []),
        ]}
      />

      <Modal
        open={deleteOpen}
        title={`删除 ${type.label} / ${item.metadata.name}`}
        okText="确认删除"
        okButtonProps={{
          danger: true,
          loading: deleting,
          disabled: requiresTypedConfirm && deleteConfirm !== item.metadata.name,
        }}
        onOk={() => void remove()}
        onCancel={() => setDeleteOpen(false)}
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="删除后无法撤销"
          description={
            type.key === 'namespaces'
              ? '命名空间内的全部资源都会被删除。'
              : type.key === 'pods' && ownerRefs.length > 0
                ? '该 Pod 由控制器管理，删除后会被自动重建。'
                : undefined
          }
        />
        {requiresTypedConfirm && (
          <>
            <Typography.Paragraph>
              请输入名称 <Typography.Text code>{item.metadata.name}</Typography.Text> 确认：
            </Typography.Paragraph>
            <Input
              value={deleteConfirm}
              onChange={(event) => setDeleteConfirm(event.target.value)}
              aria-label="删除确认名称"
            />
          </>
        )}
      </Modal>
    </div>
  )
}
