import { useMemo, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Drawer,
  InputNumber,
  Space,
  Table,
  Tag,
} from 'antd'
import {
  DisconnectOutlined,
  NodeIndexOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  api,
  ApiError,
  type DrainReport,
  type ResourceItem,
} from '../../lib/api'
import PageHeader from '../../components/PageHeader'
import StatusPill from '../../components/StatusPill'
import { ErrorState, PageSkeleton } from '../../components/StateViews'
import { ageOf, formatCPU, formatMemory, parseCPU, parseMemory } from '../../lib/format'
import { nodeRoles, nodeStatus } from './NodesPage'

const NODE_GVR = { group: '', version: 'v1', resource: 'nodes' }

function record(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

function podPhase(pod: ResourceItem): string {
  const phase = pod.status?.phase
  const statuses = Array.isArray(pod.status?.containerStatuses)
    ? (pod.status.containerStatuses as { ready?: boolean }[])
    : []
  const ready = statuses.filter((status) => status.ready).length
  return statuses.length && typeof phase === 'string' ? `${phase} ${ready}/${statuses.length}` : String(phase ?? 'Unknown')
}

function restarts(pod: ResourceItem): number {
  const statuses = Array.isArray(pod.status?.containerStatuses)
    ? (pod.status.containerStatuses as { restartCount?: number }[])
    : []
  return statuses.reduce((sum, status) => sum + (status.restartCount ?? 0), 0)
}

function ownerKind(pod: ResourceItem): string {
  const owner = pod.metadata.ownerReferences?.find((reference) => reference.kind)
  return owner?.kind ?? '无控制器'
}

function previewClass(pod: ResourceItem, force: boolean, deleteEmptyDir: boolean): string {
  const owner = pod.metadata.ownerReferences ?? []
  if (pod.metadata.annotations?.['kubernetes.io/config.mirror']) return '镜像 Pod（跳过）'
  if (owner.some((reference) => reference.kind === 'DaemonSet')) return 'DaemonSet（跳过）'
  const volumes = Array.isArray(pod.spec?.volumes) ? (pod.spec?.volumes as Record<string, unknown>[]) : []
  if (!owner.length && !force) return '无控制器（需强制）'
  if (volumes.some((volume) => volume.emptyDir !== undefined) && !deleteEmptyDir) return 'emptyDir（需允许删除）'
  return '将驱逐'
}

export default function NodeDetailPage() {
  const { cluster, name } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [drainOpen, setDrainOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [ignoreDaemonSets, setIgnoreDaemonSets] = useState(true)
  const [force, setForce] = useState(false)
  const [deleteEmptyDirData, setDeleteEmptyDirData] = useState(false)
  const [grace, setGrace] = useState(30)
  const [timeoutSeconds, setTimeoutSeconds] = useState(120)
  const [cordon, setCordon] = useState(true)
  const [report, setReport] = useState<DrainReport>()

  const nodeQuery = useQuery({
    queryKey: ['node-detail', cluster, name],
    queryFn: ({ signal }) =>
      api.detail({ cluster: cluster as string, gvr: NODE_GVR, name: name as string, signal }),
    enabled: !!cluster && !!name,
  })
  const podsQuery = useQuery({
    queryKey: ['node-pods', cluster, name],
    queryFn: ({ signal }) => api.nodePods(cluster as string, name as string, signal),
    enabled: !!cluster && !!name,
  })

  const node = nodeQuery.data?.object
  const pods = podsQuery.data?.items ?? []
  const unschedulable = record(node?.spec).unschedulable === true

  const conditions = useMemo(() => {
    const list = Array.isArray(node?.status?.conditions)
      ? (node?.status.conditions as { type: string; status: string; reason?: string; message?: string; lastTransitionTime?: string }[])
      : []
    return list
  }, [node])

  const setSchedulable = async (schedulable: boolean) => {
    if (!cluster || !name) return
    setBusy(true)
    try {
      if (schedulable) await api.uncordonNode(cluster, name)
      else await api.cordonNode(cluster, name)
      message.success(schedulable ? '已恢复调度' : '已停止调度')
      await nodeQuery.refetch()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  const runDrain = async () => {
    if (!cluster || !name) return
    setBusy(true)
    setReport(undefined)
    try {
      const result = await api.drainNode(cluster, name, {
        cordon,
        force,
        ignoreDaemonSets,
        deleteEmptyDirData,
        gracePeriodSeconds: grace,
        timeoutSeconds,
      })
      setReport(result)
      message.success(`已驱逐 ${result.evicted} 个 Pod，跳过 ${result.skipped} 个，失败 ${result.failed} 个`)
      await Promise.all([nodeQuery.refetch(), podsQuery.refetch()])
      await queryClient.invalidateQueries({ queryKey: ['resources'] })
    } catch (error) {
      if (error instanceof ApiError && error.code === 'CONFLICT') {
        const details = (error.details ?? {}) as { drain?: DrainReport }
        if (details.drain) setReport(details.drain)
      }
      message.error(error instanceof Error ? error.message : '驱逐失败')
    } finally {
      setBusy(false)
    }
  }

  if (nodeQuery.isLoading) return <PageSkeleton rows={8} />
  if (nodeQuery.isError) {
    const error = nodeQuery.error
    if (error instanceof ApiError && (error.code === 'NOT_FOUND' || error.code === 'FORBIDDEN')) {
      return (
        <ErrorState
          error={error}
          title={error.code === 'FORBIDDEN' ? '无访问权限' : '节点不存在'}
          onRetry={() => navigate(`/clusters/${encodeURIComponent(cluster ?? '')}/nodes`)}
        />
      )
    }
    return <ErrorState error={error} onRetry={() => void nodeQuery.refetch()} />
  }
  if (!node) return null

  const allocatable = record(node.status?.allocatable)
  const capacity = record(node.status?.capacity)
  const nodeInfo = record(node.status?.nodeInfo)
  const taints = Array.isArray(node.spec?.taints)
    ? (node.spec.taints as { key: string; value?: string; effect: string }[])
    : []
  const addresses = Array.isArray(node.status?.addresses)
    ? (node.status.addresses as { type: string; address: string }[])
    : []
  const internalIP = addresses.find((address) => address.type === 'InternalIP')?.address ?? '-'

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `/clusters/${encodeURIComponent(cluster ?? '')}/overview` },
          { label: '节点', to: `/clusters/${encodeURIComponent(cluster ?? '')}/nodes` },
          { label: name ?? '' },
        ]}
        title={
          <Space size={10}>
            <NodeIndexOutlined style={{ color: '#64748b' }} />
            {node.metadata.name}
          </Space>
        }
        tags={
          <Space size={6} wrap>
            <StatusPill value={nodeStatus(node)} />
            {unschedulable ? <StatusPill value="已停止调度" /> : <StatusPill value="可调度" />}
            {nodeRoles(node).map((role) => (
              <Tag key={role} style={{ marginInlineEnd: 0 }}>
                {role}
              </Tag>
            ))}
          </Space>
        }
        subtitle={
          <span className="kd-muted">
            {String(nodeInfo.kubeletVersion ?? '-')} · {String(nodeInfo.osImage ?? '-')} · 创建于{' '}
            {ageOf(node.metadata.creationTimestamp)} 前
          </span>
        }
        actions={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void nodeQuery.refetch()}>
              刷新
            </Button>
            {unschedulable ? (
              <Button
                type="primary"
                icon={<ThunderboltOutlined />}
                loading={busy}
                onClick={() => void setSchedulable(true)}
              >
                恢复调度
              </Button>
            ) : (
              <Button
                icon={<PoweroffOutlined />}
                loading={busy}
                onClick={() => void setSchedulable(false)}
              >
                停止调度
              </Button>
            )}
            <Button danger icon={<DisconnectOutlined />} onClick={() => setDrainOpen(true)}>
              驱逐节点
            </Button>
          </Space>
        }
      />

      <div className="kd-workbench">
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Card title="Conditions" size="small">
            <Table
              size="small"
              rowKey="type"
              pagination={false}
              dataSource={conditions}
              columns={[
                { title: '类型', dataIndex: 'type', width: 180 },
                {
                  title: '状态',
                  dataIndex: 'status',
                  width: 110,
                  render: (value: string) => (
                    <StatusPill value={value === 'True' ? 'True' : value === 'False' ? 'False' : value} />
                  ),
                },
                { title: '原因', dataIndex: 'reason', width: 180 },
                { title: '消息', dataIndex: 'message', ellipsis: true },
                {
                  title: '变更时间',
                  dataIndex: 'lastTransitionTime',
                  width: 160,
                  render: (value: string) => (value ? ageOf(value) + ' 前' : '-'),
                },
              ]}
            />
          </Card>

          <Card title="容量与可分配" size="small">
            <Table<{ key: string; label: string; capacity: number; allocatable: number }>
              size="small"
              pagination={false}
              rowKey="key"
              dataSource={[
                { key: 'cpu', label: 'CPU', capacity: parseCPU(capacity.cpu), allocatable: parseCPU(allocatable.cpu) },
                { key: 'memory', label: '内存', capacity: parseMemory(capacity.memory), allocatable: parseMemory(allocatable.memory) },
                { key: 'pods', label: 'Pod', capacity: Number(capacity.pods ?? 0), allocatable: Number(allocatable.pods ?? 0) },
              ]}
              columns={[
                { title: '资源', dataIndex: 'label' },
                {
                  title: '容量',
                  dataIndex: 'capacity',
                  render: (value: number, row) =>
                    row.key === 'pods' ? value : row.key === 'cpu' ? formatCPU(value) : formatMemory(value),
                },
                {
                  title: '可分配',
                  dataIndex: 'allocatable',
                  render: (value: number, row) =>
                    row.key === 'pods' ? value : row.key === 'cpu' ? formatCPU(value) : formatMemory(value),
                },
              ]}
            />
          </Card>

          <Card
            title="节点上的 Pod"
            size="small"
            extra={
              <Space>
                <span className="kd-muted" style={{ fontSize: 12 }}>
                  {pods.length} 个
                </span>
                <Button size="small" onClick={() => void podsQuery.refetch()}>
                  刷新
                </Button>
              </Space>
            }
          >
            {podsQuery.isError ? (
              <ErrorState error={podsQuery.error} onRetry={() => void podsQuery.refetch()} />
            ) : (
              <Table<ResourceItem>
                size="small"
                rowKey={(pod) => pod.metadata.uid ?? `${pod.metadata.namespace}/${pod.metadata.name}`}
                loading={podsQuery.isFetching}
                dataSource={pods}
                pagination={false}
                onRow={(pod) => ({
                  style: { cursor: 'pointer' },
                  onClick: () =>
                    navigate(
                      `/clusters/${encodeURIComponent(cluster ?? '')}/pods/${encodeURIComponent(pod.metadata.namespace ?? '_')}/${encodeURIComponent(pod.metadata.name)}?namespace=${encodeURIComponent(pod.metadata.namespace ?? '')}`,
                    ),
                })}
                columns={[
                  { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 150 },
                  { title: '名称', dataIndex: ['metadata', 'name'], ellipsis: true },
                  {
                    title: '状态',
                    key: 'status',
                    width: 150,
                    render: (_, pod) => <StatusPill value={podPhase(pod)} />,
                  },
                  { title: '控制器', key: 'owner', width: 120, render: (_, pod) => ownerKind(pod) },
                  { title: '重启', key: 'restarts', width: 70, render: (_, pod) => restarts(pod) },
                  { title: 'Age', key: 'age', width: 80, render: (_, pod) => ageOf(pod.metadata.creationTimestamp) },
                ]}
              />
            )}
          </Card>
        </Space>

        <div className="kd-rail">
          <div className="kd-rail-block">
            <div className="kd-rail-label">基本信息</div>
            <div className="kd-kv">
              <div>
                <span>内部 IP</span>
                <span className="kd-mono">{internalIP}</span>
              </div>
              <div>
                <span>Pod CIDR</span>
                <span className="kd-mono">{String(record(node.spec).podCIDR ?? '-')}</span>
              </div>
              <div>
                <span>容器运行时</span>
                <span className="kd-mono">{String(nodeInfo.containerRuntimeVersion ?? '-')}</span>
              </div>
              <div>
                <span>内核</span>
                <span className="kd-mono">{String(nodeInfo.kernelVersion ?? '-')}</span>
              </div>
            </div>
          </div>

          <div className="kd-rail-block">
            <div className="kd-rail-label">Taints</div>
            {taints.length === 0 ? (
              <span className="kd-muted">无</span>
            ) : (
              <Space direction="vertical" size={6} style={{ width: '100%' }}>
                {taints.map((taint) => (
                  <div key={`${taint.key}-${taint.effect}`} className="kd-mono" style={{ fontSize: 11.5 }}>
                    {taint.key}
                    {taint.value ? `=${taint.value}` : ''}
                    <Tag style={{ marginLeft: 6 }}>{taint.effect}</Tag>
                  </div>
                ))}
              </Space>
            )}
          </div>

          <div className="kd-rail-block">
            <div className="kd-rail-label">标签</div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
              {Object.entries(node.metadata.labels ?? {}).map(([key, value]) => (
                <Tag key={key} style={{ marginInlineEnd: 0, fontSize: 11 }}>
                  {key}={value}
                </Tag>
              ))}
            </div>
          </div>
        </div>
      </div>

      <Drawer
        title={`驱逐节点 ${name}`}
        width={760}
        open={drainOpen}
        onClose={() => {
          setDrainOpen(false)
          setReport(undefined)
        }}
        extra={
          <Button type="primary" danger loading={busy} onClick={() => void runDrain()}>
            开始驱逐
          </Button>
        }
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
          message="驱逐会逐个创建 Eviction 请求"
          description="受 PodDisruptionBudget 约束；DaemonSet 与镜像 Pod 默认跳过。"
        />

        <Space direction="vertical" size={8} style={{ width: '100%', marginBottom: 16 }}>
          <Checkbox checked={cordon} onChange={(event) => setCordon(event.target.checked)}>
            驱逐前先停止调度（cordon）
          </Checkbox>
          <Checkbox checked={ignoreDaemonSets} onChange={(event) => setIgnoreDaemonSets(event.target.checked)}>
            跳过 DaemonSet 管理的 Pod
          </Checkbox>
          <Checkbox checked={force} onChange={(event) => setForce(event.target.checked)}>
            强制驱逐没有控制器 Owner 的 Pod
          </Checkbox>
          <Checkbox
            checked={deleteEmptyDirData}
            onChange={(event) => setDeleteEmptyDirData(event.target.checked)}
          >
            允许驱逐使用 emptyDir 的 Pod（会删除本地数据）
          </Checkbox>
          <Space>
            <span className="kd-muted">宽限期（秒）</span>
            <InputNumber min={0} max={3600} value={grace} onChange={(value) => setGrace(value ?? 30)} />
            <span className="kd-muted">整体超时（秒）</span>
            <InputNumber
              min={10}
              max={300}
              value={timeoutSeconds}
              onChange={(value) => setTimeoutSeconds(value ?? 120)}
            />
          </Space>
        </Space>

        <div className="kd-section-title">影响预览</div>
        <Table<ResourceItem>
          size="small"
          rowKey={(pod) => pod.metadata.uid ?? `${pod.metadata.namespace}/${pod.metadata.name}`}
          dataSource={pods}
          pagination={false}
          scroll={{ y: 260 }}
          columns={[
            { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 130 },
            { title: 'Pod', dataIndex: ['metadata', 'name'], ellipsis: true },
            {
              title: '处理',
              key: 'action',
              width: 180,
              render: (_, pod) => {
                const label = previewClass(pod, force, deleteEmptyDirData)
                return (
                  <span className={label === '将驱逐' ? '' : 'kd-muted'}>{label}</span>
                )
              },
            },
          ]}
        />

        {report && (
          <>
            <div className="kd-section-title" style={{ marginTop: 18 }}>
              执行结果
            </div>
            <Space style={{ marginBottom: 10 }}>
              <Tag color="green">已驱逐 {report.evicted}</Tag>
              <Tag>跳过 {report.skipped}</Tag>
              <Tag color="orange">阻止 {report.blocked}</Tag>
              <Tag color="red">失败 {report.failed}</Tag>
            </Space>
            <Table
              size="small"
              rowKey={(row) => `${row.namespace}/${row.name}`}
              dataSource={report.results}
              pagination={false}
              scroll={{ y: 240 }}
              columns={[
                { title: '命名空间', dataIndex: 'namespace', width: 130 },
                { title: 'Pod', dataIndex: 'name', ellipsis: true },
                {
                  title: '结果',
                  dataIndex: 'result',
                  width: 90,
                  render: (value: string) => (
                    <StatusPill
                      value={
                        value === 'evicted'
                          ? 'evicted'
                          : value === 'blocked'
                            ? 'blocked'
                            : value === 'failed'
                              ? 'failed'
                              : 'skipped'
                      }
                    />
                  ),
                },
                { title: '原因', dataIndex: 'message', ellipsis: true },
              ]}
            />
          </>
        )}
      </Drawer>
    </div>
  )
}