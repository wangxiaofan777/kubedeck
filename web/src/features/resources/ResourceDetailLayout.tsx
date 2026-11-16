import { useState } from 'react'
import {
  Alert,
  App,
  Button,
  Drawer,
  Input,
  Result,
  Space,
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
import {
  NavLink,
  Outlet,
  useNavigate,
  useOutletContext,
  useParams,
} from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import PageHeader from '../../components/PageHeader'
import StatusPill from '../../components/StatusPill'
import { ErrorState, PageSkeleton } from '../../components/StateViews'
import { api, ApiError, type ResourceItem } from '../../lib/api'
import { formatDateTime } from '../../lib/format'
import { useCan } from '../auth/useCan'
import { statusOf, type ResourceType } from './resourceTypes'
import { useResourceType } from './useResourceType'
import {
  OWNER_TYPE,
  containersOf,
  detailBase,
  listPath,
  selectorOf,
} from './resourceDetailHelpers'

export interface ResourceDetailContextValue {
  cluster: string
  type: ResourceType
  namespace?: string
  name: string
  item: ResourceItem
  refetch: () => void
  updateCan: ReturnType<typeof useCan>
  deleteCan: ReturnType<typeof useCan>
  evictCan: ReturnType<typeof useCan>
  selector: string
  containers: string[]
}

export function useResourceDetail(): ResourceDetailContextValue {
  return useOutletContext<ResourceDetailContextValue>()
}

/** ResourceDetailLayout 负责详情页的公共数据、错误状态、页面头、子导航与右侧上下文栏。 */
export default function ResourceDetailLayout() {
  const { cluster, typeKey, namespace: nsParam, name } = useParams()
  const namespace = nsParam === '_' ? undefined : nsParam
  const type = useResourceType(cluster, typeKey)
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
    queryKey: ['detail', cluster, type?.key, type?.gvr.group, type?.gvr.version, namespace, name],
    queryFn: ({ signal }) =>
      api.detail({ cluster: cluster!, gvr: type!.gvr, namespace, name: name!, signal }),
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
    verb: 'create',
    subresource: 'eviction',
    enabled: type?.key === 'pods',
  })

  if (!type) {
    return <Result status="404" title="未知的资源类型" subTitle={`类型 "${typeKey}" 不在导航中。`} />
  }
  if (!cluster || !name) {
    return <Result status="404" title="资源地址不完整" subTitle="缺少集群或对象名称。" />
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
          <Button onClick={() => navigate(listPath(cluster, type, namespace))}>返回列表</Button>
        }
      />
    )
  }
  if (detailQuery.isLoading) return <PageSkeleton rows={10} />
  const item = detailQuery.data?.object
  if (!item) {
    return (
      <ErrorState
        error={detailQuery.error}
        title="无法加载资源"
        onRetry={() => void detailQuery.refetch()}
      />
    )
  }

  const status = statusOf(item, type)
  const ownerRefs = detailQuery.data?.ownerReferences ?? item.metadata.ownerReferences ?? []
  const requiresTypedConfirm = type.key === 'namespaces' || type.key === 'persistentvolumes'
  const base = detailBase(cluster, type, namespace, name)
  const list = listPath(cluster, type, namespace)
  const nodeName = typeof item.spec?.nodeName === 'string' ? item.spec.nodeName : undefined
  const selector = selectorOf(item)
  const containers = containersOf(item)

  const createURL = `/clusters/${encodeURIComponent(cluster)}/new/${type.key}?copy=${encodeURIComponent(
    `${item.metadata.namespace ?? '_'}/${item.metadata.name}`,
  )}${namespace ? `&namespace=${encodeURIComponent(namespace)}` : ''}`

  const openOwner = (kind: string, ownerName: string) => {
    const key = OWNER_TYPE[kind]
    if (!key) return
    const ns = item.metadata.namespace ?? '_'
    const suffix = item.metadata.namespace
      ? `?namespace=${encodeURIComponent(item.metadata.namespace)}`
      : ''
    navigate(`/clusters/${encodeURIComponent(cluster)}/${key}/${encodeURIComponent(ns)}/${encodeURIComponent(ownerName)}${suffix}`)
  }

  const remove = async () => {
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
      navigate(list)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '删除失败')
    } finally {
      setDeleting(false)
    }
  }

  const evict = async () => {
    if (!namespace) return
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

  const tabs = [
    { to: base, label: '概览', end: true },
    { to: `${base}/yaml`, label: 'YAML' },
    ...(updateCan.loading || updateCan.allowed
      ? [{ to: `${base}/diff`, label: '变更比较' }]
      : []),
    { to: `${base}/events`, label: '事件' },
    ...(type.key === 'pods' && namespace ? [{ to: `${base}/logs`, label: '日志' }] : []),
    ...(selector && type.key !== 'pods' && namespace
      ? [{ to: `${base}/pods`, label: '关联 Pod' }]
      : []),
  ]

  const context: ResourceDetailContextValue = {
    cluster,
    type,
    namespace,
    name,
    item,
    refetch: () => void detailQuery.refetch(),
    updateCan,
    deleteCan,
    evictCan,
    selector,
    containers,
  }

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `/clusters/${encodeURIComponent(cluster)}/overview` },
          ...(namespace ? [{ label: namespace }] : []),
          { label: type.label, to: list },
          { label: item.metadata.name },
        ]}
        title={
          <Space size={8}>
            <Button
              type="text"
              size="small"
              icon={<ArrowLeftOutlined />}
              onClick={() => navigate(list)}
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
              onClick={() => void detailQuery.refetch()}
            >
              刷新
            </Button>
            <Button
              icon={<CopyOutlined />}
              onClick={() => navigate(createURL)}
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
          <nav className="kd-subnav" aria-label="资源详情子视图">
            {tabs.map((tab) => (
              <NavLink
                key={tab.to}
                to={tab.to}
                end={tab.end}
                className={({ isActive }) => (isActive ? 'kd-subnav-item active' : 'kd-subnav-item')}
              >
                {tab.label}
              </NavLink>
            ))}
          </nav>
          <div style={{ marginTop: 14 }}>
            <Outlet context={context} />
          </div>
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
                    onClick={() =>
                    navigate(
                      `/clusters/${encodeURIComponent(cluster)}/nodes/${encodeURIComponent(nodeName)}`,
                    )
                  }
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
                    `${base}/../new/${type.key}?copy=${encodeURIComponent(
                      `${item.metadata.namespace ?? '_'}/${item.metadata.name}`,
                    )}${namespace ? `&namespace=${encodeURIComponent(namespace)}` : ''}`,
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
          message="将创建 Pod Eviction 请求"
          description="受 PodDisruptionBudget 约束；控制器管理的 Pod 会被自动重建。平台按集群版本自动选择 policy/v1 或 policy/v1beta1。"
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