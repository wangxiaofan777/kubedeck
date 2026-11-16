import { lazy, Suspense, useMemo, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Descriptions,
  Drawer,
  Input,
  Result,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import {
  ArrowLeftOutlined,
  CopyOutlined,
  DeleteOutlined,
  LogoutOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type EventItem, type ResourceItem } from '../../lib/api'
import PageHeader from '../../components/PageHeader'
import StatusPill from '../../components/StatusPill'
import { ErrorState, PageSkeleton } from '../../components/StateViews'
import { ageOf, formatDateTime } from '../../lib/format'
import { useCan } from '../auth/useCan'
import { resolveType, statusOf } from './resourceTypes'
import LogViewer from '../logs/LogViewer'
import WorkloadActions from '../workloads/WorkloadActions'

const YamlEditor = lazy(() => import('./YamlEditor'))

const OWNER_TYPE: Record<string, string> = {
  Deployment: 'deployments',
  ReplicaSet: 'replicasets',
  StatefulSet: 'statefulsets',
  DaemonSet: 'daemonsets',
  Job: 'jobs',
  CronJob: 'cronjobs',
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

function selectorOf(item: ResourceItem): string {
  const spec = record(item.spec)
  const selector = record(spec.selector)
  const matchLabels = record(selector.matchLabels)
  const labels = Object.keys(matchLabels).length ? matchLabels : selector
  return Object.entries(labels)
    .filter(([, value]) => typeof value === 'string')
    .map(([key, value]) => `${key}=${value}`)
    .join(',')
}

export default function ResourceDetailPage() {
  const { cluster, typeKey, namespace: nsParam, name } = useParams()
  const namespace = nsParam === '_' ? undefined : nsParam
  const type = resolveType(typeKey)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [evictOpen, setEvictOpen] = useState(false)
  const [evicting, setEvicting] = useState(false)
  const [evictError, setEvictError] = useState<ApiError>()

  const detailQuery = useQuery({
    queryKey: ['detail', cluster, type?.key, namespace, name],
    queryFn: ({ signal }) =>
      api.detail({ cluster: cluster!, gvr: type!.gvr, namespace, name: name!, signal }),
    enabled: !!cluster && !!type && !!name,
  })

  const eventsQuery = useQuery({
    queryKey: ['events', cluster, namespace, type?.kind, name],
    queryFn: ({ signal }) =>
      api.events(cluster!, { namespace, kind: type!.kind, name, signal }),
    enabled: !!cluster && !!type && !!name,
  })

  const updateCan = useCan({
    cluster,
    gvr: type?.gvr ?? { group: '', version: 'v1', resource: 'pods' },
    namespace,
    verb: 'update',
    enabled: !!type,
  })
  const deleteCan = useCan({
    cluster,
    gvr: type?.gvr ?? { group: '', version: 'v1', resource: 'pods' },
    namespace,
    verb: 'delete',
    enabled: !!type,
  })
  const evictCan = useCan({
    cluster,
    gvr: { group: '', version: 'v1', resource: 'pods' },
    namespace,
    name,
    verb: 'evict',
    enabled: type?.key === 'pods',
  })

  const item = detailQuery.data?.object
  const containers = useMemo(() => {
    const list = item?.spec?.containers
    return Array.isArray(list)
      ? (list as { name?: string }[]).map((container) => container.name ?? '').filter(Boolean)
      : []
  }, [item])

  const selector = useMemo(() => (item ? selectorOf(item) : ''), [item])
  const relatedQuery = useQuery({
    queryKey: ['related-pods', cluster, namespace, selector],
    queryFn: ({ signal }) =>
      api.list({
        cluster: cluster!,
        gvr: { group: '', version: 'v1', resource: 'pods' },
        namespace,
        labelSelector: selector,
        limit: 200,
        signal,
      }),
    enabled: !!cluster && !!selector && !!namespace && type?.key !== 'pods',
  })

  if (!type) {
    return <Result status="404" title="未知的资源类型" subTitle={`类型 "${typeKey}" 不在导航中。`} />
  }
  if (detailQuery.error instanceof ApiError && detailQuery.error.code === 'FORBIDDEN') {
    return (
      <Result
        status="403"
        title="无访问权限"
        subTitle="当前账号无权查看该资源。"
        extra={<Button onClick={() => navigate(-1)}>返回</Button>}
      />
    )
  }
  if (detailQuery.error instanceof ApiError && detailQuery.error.code === 'NOT_FOUND') {
    return (
      <Result
        status="404"
        title="资源不存在"
        subTitle="对象可能已被删除。"
        extra={
          <Button
            onClick={() =>
              navigate(
                `/clusters/${encodeURIComponent(cluster ?? '')}/${type.key}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''}`,
              )
            }
          >
            返回列表
          </Button>
        }
      />
    )
  }
  if (detailQuery.isLoading) return <PageSkeleton rows={10} />
  if (!item) {
    return <ErrorState error={detailQuery.error} title="无法加载资源" onRetry={() => void detailQuery.refetch()} />
  }

  const status = statusOf(item, type)
  const ownerRefs = detailQuery.data?.ownerReferences ?? item.metadata.ownerReferences ?? []
  const requiresTypedConfirm = type.key === 'namespaces' || type.key === 'persistentvolumes'
  const base = `/clusters/${encodeURIComponent(cluster ?? '')}`
  const listPath = `${base}/${type.key}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''}`
  const nodeName = typeof item.spec?.nodeName === 'string' ? item.spec.nodeName : undefined
  const isWorkload = ['deployments', 'statefulsets', 'daemonsets', 'replicasets'].includes(type.key)

  const openOwner = (kind: string, ownerName: string) => {
    const key = OWNER_TYPE[kind]
    if (!key) return
    const ns = item.metadata.namespace ?? '_'
    const suffix = item.metadata.namespace
      ? `?namespace=${encodeURIComponent(item.metadata.namespace)}`
      : ''
    navigate(`${base}/${key}/${encodeURIComponent(ns)}/${encodeURIComponent(ownerName)}${suffix}`)
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
      await queryClient.invalidateQueries({ queryKey: ['resources'] })
      navigate(listPath)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '删除失败')
    } finally {
      setDeleting(false)
    }
  }

  const evict = async () => {
    if (!cluster || !namespace || !name) return
    setEvicting(true)
    setEvictError(undefined)
    try {
      await api.evict({ cluster, namespace, name })
      message.success('驱逐请求已受理')
      setEvictOpen(false)
      await detailQuery.refetch()
    } catch (error) {
      if (error instanceof ApiError) {
        setEvictError(error)
      } else {
        message.error(error instanceof Error ? error.message : '驱逐失败')
      }
    } finally {
      setEvicting(false)
    }
  }

  const overview = (
    <Space direction="vertical" size={14} style={{ width: '100%' }}>
      <Card size="small" title="元数据">
        <Descriptions column={2} size="small">
          <Descriptions.Item label="名称">{item.metadata.name}</Descriptions.Item>
          <Descriptions.Item label="命名空间">
            {item.metadata.namespace ?? <span className="kd-muted">集群级</span>}
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            <StatusPill value={status} />
          </Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {item.metadata.creationTimestamp
              ? `${formatDateTime(item.metadata.creationTimestamp)}（${ageOf(item.metadata.creationTimestamp)} 前）`
              : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="资源版本" span={2}>
            <span className="kd-mono">{item.metadata.resourceVersion ?? '-'}</span>
          </Descriptions.Item>
          <Descriptions.Item label="UID" span={2}>
            <span className="kd-mono">{item.metadata.uid ?? '-'}</span>
          </Descriptions.Item>
          <Descriptions.Item label="标签" span={2}>
            {Object.entries(item.metadata.labels ?? {}).length === 0 ? (
              <span className="kd-muted">-</span>
            ) : (
              <Space size={4} wrap>
                {Object.entries(item.metadata.labels ?? {}).map(([key, value]) => (
                  <Tag key={key} style={{ marginInlineEnd: 0 }}>
                    {key}={value}
                  </Tag>
                ))}
              </Space>
            )}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {type.key === 'pods' && Array.isArray(item.spec?.containers) && (
        <Card size="small" title="容器">
          <Table
            rowKey={(row) => (row as { name?: string }).name ?? ''}
            size="small"
            pagination={false}
            dataSource={item.spec.containers as Record<string, unknown>[]}
            columns={[
              { title: '名称', dataIndex: 'name' },
              {
                title: '镜像',
                dataIndex: 'image',
                ellipsis: true,
                render: (value: string) => <span className="kd-mono">{value}</span>,
              },
              {
                title: '就绪',
                key: 'ready',
                width: 90,
                render: (_, row) => {
                  const statuses = Array.isArray(item.status?.containerStatuses)
                    ? (item.status.containerStatuses as {
                        name?: string
                        ready?: boolean
                        restartCount?: number
                      }[])
                    : []
                  const found = statuses.find(
                    (entry) => entry.name === (row as { name?: string }).name,
                  )
                  return found?.ready ? (
                    <StatusPill value="Ready" />
                  ) : (
                    <StatusPill value="NotReady" />
                  )
                },
              },
              {
                title: '重启',
                key: 'restarts',
                width: 80,
                render: (_, row) => {
                  const statuses = Array.isArray(item.status?.containerStatuses)
                    ? (item.status.containerStatuses as {
                        name?: string
                        restartCount?: number
                      }[])
                    : []
                  return (
                    statuses.find(
                      (entry) => entry.name === (row as { name?: string }).name,
                    )?.restartCount ?? 0
                  )
                },
              },
            ]}
          />
        </Card>
      )}

      {isWorkload && (
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
    {
      title: '类型',
      dataIndex: 'type',
      width: 90,
      render: (value: string) => <StatusPill value={value} />,
    },
    { title: '原因', dataIndex: 'reason', width: 180 },
    { title: '消息', dataIndex: 'message', ellipsis: true },
    { title: '次数', dataIndex: 'count', width: 70 },
    {
      title: '最后发生',
      dataIndex: 'lastTimestamp',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
  ]

  const tabs = [
    { key: 'overview', label: '概览', children: overview },
    {
      key: 'yaml',
      label: 'YAML',
      children: (
        <Suspense fallback={<PageSkeleton rows={8} />}>
          <YamlEditor
            cluster={cluster!}
            gvr={type.gvr}
            namespace={namespace}
            name={item.metadata.name}
            canUpdate={updateCan.allowed && !updateCan.loading}
            onSaved={() => {
              void detailQuery.refetch()
              void queryClient.invalidateQueries({ queryKey: ['resources'] })
            }}
          />
        </Suspense>
      ),
    },
    {
      key: 'events',
      label: `事件${eventsQuery.data?.items?.length ? `（${eventsQuery.data.items.length}）` : ''}`,
      children: eventsQuery.isError ? (
        <ErrorState error={eventsQuery.error} onRetry={() => void eventsQuery.refetch()} />
      ) : (
        <Table<EventItem>
          size="small"
          rowKey={(event) =>
            `${event.reason}-${event.lastTimestamp}-${event.involvedObject?.name ?? ''}`
          }
          loading={eventsQuery.isFetching && !eventsQuery.isLoading}
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
    ...(selector && type.key !== 'pods' && namespace
      ? [
          {
            key: 'related',
            label: '关联 Pod',
            children: relatedQuery.isError ? (
              <ErrorState error={relatedQuery.error} onRetry={() => void relatedQuery.refetch()} />
            ) : (
              <Table<ResourceItem>
                size="small"
                rowKey={(pod) =>
                  pod.metadata.uid ?? `${pod.metadata.namespace}/${pod.metadata.name}`
                }
                loading={relatedQuery.isFetching}
                dataSource={relatedQuery.data?.items ?? []}
                pagination={false}
                columns={[
                  { title: '名称', dataIndex: ['metadata', 'name'], ellipsis: true },
                  {
                    title: '状态',
                    key: 'status',
                    width: 170,
                    render: (_, pod) => (
                      <StatusPill
                        value={
                          typeof pod.status?.phase === 'string' ? pod.status.phase : 'Unknown'
                        }
                      />
                    ),
                  },
                  {
                    title: '节点',
                    key: 'node',
                    width: 160,
                    render: (_, pod) =>
                      typeof pod.spec?.nodeName === 'string' ? (
                        <span
                          className="kd-link"
                          onClick={() =>
                            navigate(
                              `${base}/nodes/${encodeURIComponent(String(pod.spec?.nodeName))}`,
                            )
                          }
                        >
                          {pod.spec.nodeName}
                        </span>
                      ) : (
                        '-'
                      ),
                  },
                  {
                    title: 'Age',
                    key: 'age',
                    width: 80,
                    render: (_, pod) => ageOf(pod.metadata.creationTimestamp),
                  },
                ]}
              />
            ),
          },
        ]
      : []),
  ]

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `${base}/overview` },
          ...(namespace ? [{ label: namespace }] : []),
          { label: type.label, to: listPath },
          { label: item.metadata.name },
        ]}
        title={
          <Space size={8}>
            <Button
              type="text"
              size="small"
              icon={<ArrowLeftOutlined />}
              onClick={() => navigate(listPath)}
              aria-label="返回列表"
            />
            {item.metadata.name}
          </Space>
        }
        tags={
          <Space size={6} wrap>
            <Tag color="blue">{type.label}</Tag>
            <StatusPill value={status} />
            {!updateCan.loading && !updateCan.allowed && <Tag>只读</Tag>}
          </Space>
        }
        actions={
          <Space>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => {
                void detailQuery.refetch()
                void eventsQuery.refetch()
              }}
            >
              刷新
            </Button>
            <Button
              icon={<CopyOutlined />}
              onClick={() =>
                navigate(
                  `${base}/new/${type.key}?copy=${encodeURIComponent(`${item.metadata.namespace ?? '_'}/${item.metadata.name}`)}${namespace ? `&namespace=${encodeURIComponent(namespace)}` : ''}`,
                )
              }
            >
              复制为新
            </Button>
            {type.key === 'pods' && (evictCan.loading || evictCan.allowed) && (
              <Button
                icon={<LogoutOutlined />}
                disabled={evictCan.loading}
                onClick={() => setEvictOpen(true)}
              >
                驱逐
              </Button>
            )}
            {(deleteCan.loading || deleteCan.allowed) && (
              <Button danger icon={<DeleteOutlined />} onClick={() => setDeleteOpen(true)}>
                删除
              </Button>
            )}
          </Space>
        }
      />

      <div className="kd-workbench">
        <div>
          <Tabs items={tabs} />
        </div>

        <aside className="kd-rail">
          <div className="kd-rail-block">
            <div className="kd-rail-label">状态</div>
            <Space direction="vertical" size={6} style={{ width: '100%' }}>
              <StatusPill value={status} />
              <span className="kd-muted" style={{ fontSize: 12 }}>
                创建于 {formatDateTime(item.metadata.creationTimestamp)}
              </span>
            </Space>
          </div>

          <div className="kd-rail-block">
            <div className="kd-rail-label">归属</div>
            <div className="kd-kv">
              <div>
                <span>命名空间</span>
                <span>{item.metadata.namespace ?? '集群级'}</span>
              </div>
              {nodeName && (
                <div>
                  <span>节点</span>
                  <span
                    className="kd-link kd-mono"
                    onClick={() => navigate(`${base}/nodes/${encodeURIComponent(nodeName)}`)}
                  >
                    {nodeName}
                  </span>
                </div>
              )}
              {ownerRefs.length > 0 &&
                ownerRefs.map((reference) => (
                  <div key={`${reference.kind}/${reference.name}`}>
                    <span>{reference.kind}</span>
                    {OWNER_TYPE[reference.kind] ? (
                      <span
                        className="kd-link kd-mono"
                        onClick={() => openOwner(reference.kind, reference.name)}
                      >
                        {reference.name}
                      </span>
                    ) : (
                      <span className="kd-mono">{reference.name}</span>
                    )}
                  </div>
                ))}
            </div>
          </div>

          <div className="kd-rail-block">
            <div className="kd-rail-label">快捷操作</div>
            <Space direction="vertical" size={8} style={{ width: '100%' }}>
              {type.key === 'pods' && (evictCan.loading || evictCan.allowed) && (
                <Button block icon={<LogoutOutlined />} onClick={() => setEvictOpen(true)}>
                  驱逐 Pod
                </Button>
              )}
              {(deleteCan.loading || deleteCan.allowed) && (
                <Button block danger icon={<DeleteOutlined />} onClick={() => setDeleteOpen(true)}>
                  删除对象
                </Button>
              )}
              <Button
                block
                icon={<CopyOutlined />}
                onClick={() =>
                  navigate(
                    `${base}/new/${type.key}?copy=${encodeURIComponent(`${item.metadata.namespace ?? '_'}/${item.metadata.name}`)}${namespace ? `&namespace=${encodeURIComponent(namespace)}` : ''}`,
                  )
                }
              >
                复制为新
              </Button>
            </Space>
          </div>
        </aside>
      </div>

      <Drawer
        title={`删除 ${type.label} / ${item.metadata.name}`}
        width={520}
        open={deleteOpen}
        onClose={() => setDeleteOpen(false)}
        extra={
          <Button
            danger
            type="primary"
            loading={deleting}
            disabled={requiresTypedConfirm && deleteConfirm !== item.metadata.name}
            onClick={() => void remove()}
          >
            确认删除
          </Button>
        }
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
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
      </Drawer>

      <Drawer
        title={`驱逐 Pod / ${item.metadata.name}`}
        width={520}
        open={evictOpen}
        onClose={() => {
          setEvictOpen(false)
          setEvictError(undefined)
        }}
        extra={
          <Button danger type="primary" loading={evicting} onClick={() => void evict()}>
            确认驱逐
          </Button>
        }
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 14 }}
          message="将创建 policy/v1 Eviction 请求"
          description="受 PodDisruptionBudget 约束；控制器管理的 Pod 会被自动重建。"
        />
        {evictError && (
          <Alert
            type="error"
            showIcon
            style={{ marginBottom: 14 }}
            message="驱逐被阻止"
            description={
              <div>
                <div>{evictError.message}</div>
                {typeof evictError.details?.pdbName === 'string' && (
                  <div style={{ marginTop: 6 }}>
                    PodDisruptionBudget：
                    <b>{String(evictError.details.pdbName)}</b>，当前允许中断数：
                    <b>{String(evictError.details.allowedDisruptions ?? 0)}</b>
                  </div>
                )}
              </div>
            }
          />
        )}
      </Drawer>
    </div>
  )
}