import { useMemo, useState } from 'react'
import { Button, Card, Input, Space, Table, Tag } from 'antd'
import { NodeIndexOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, type ResourceItem } from '../../lib/api'
import PageHeader from '../../components/PageHeader'
import StatusPill from '../../components/StatusPill'
import { ErrorState, PageSkeleton } from '../../components/StateViews'
import { ageOf, formatCPU, formatMemory, parseCPU, parseMemory } from '../../lib/format'

const NODE_GVR = { group: '', version: 'v1', resource: 'nodes' }

function record(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

export function nodeStatus(item: ResourceItem): string {
  const conditions = Array.isArray(item.status?.conditions)
    ? (item.status.conditions as { type: string; status: string }[])
    : []
  return conditions.find((condition) => condition.type === 'Ready')?.status === 'True'
    ? 'Ready'
    : 'NotReady'
}

export function nodeRoles(item: ResourceItem): string[] {
  const labels = item.metadata.labels ?? {}
  const roles = Object.keys(labels)
    .filter((key) => key.startsWith('node-role.kubernetes.io/'))
    .map((key) => key.slice('node-role.kubernetes.io/'.length))
    .filter(Boolean)
  return roles.length ? roles : ['worker']
}

export default function NodesPage() {
  const { cluster } = useParams()
  const navigate = useNavigate()
  const [search, setSearch] = useState('')

  const query = useQuery({
    queryKey: ['nodes', cluster],
    queryFn: ({ signal }) =>
      api.list({ cluster: cluster as string, gvr: NODE_GVR, limit: 500, signal }),
    enabled: !!cluster,
    refetchInterval: 30_000,
  })

  const items = useMemo(() => {
    const all = query.data?.items ?? []
    const keyword = search.trim().toLowerCase()
    return keyword ? all.filter((item) => item.metadata.name.toLowerCase().includes(keyword)) : all
  }, [query.data, search])

  if (query.isLoading) return <PageSkeleton rows={6} />
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `/clusters/${encodeURIComponent(cluster ?? '')}/overview` },
          { label: '节点' },
        ]}
        title="节点"
        subtitle={<span className="kd-muted">共 {items.length} 个节点，30 秒自动刷新</span>}
        actions={
          <Space>
            <Input
              allowClear
              prefix={<SearchOutlined />}
              placeholder="按名称过滤"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              style={{ width: 220 }}
            />
            <Button
              icon={<ReloadOutlined />}
              loading={query.isFetching && !query.isLoading}
              onClick={() => void query.refetch()}
            >
              刷新
            </Button>
          </Space>
        }
      />

      <Card className="kd-table-card" styles={{ body: { padding: 0 } }}>
        <Table<ResourceItem>
          rowKey={(item) => item.metadata.uid ?? item.metadata.name}
          dataSource={items}
          pagination={false}
          size="middle"
          onRow={(item) => ({
            style: { cursor: 'pointer' },
            onClick: () =>
              navigate(
                `/clusters/${encodeURIComponent(cluster ?? '')}/nodes/${encodeURIComponent(item.metadata.name)}`,
              ),
          })}
          columns={[
            {
              title: '名称',
              dataIndex: ['metadata', 'name'],
              render: (name: string) => (
                <Space size={6}>
                  <NodeIndexOutlined style={{ color: '#64748b' }} />
                  <span style={{ fontWeight: 550 }}>{name}</span>
                </Space>
              ),
            },
            {
              title: '状态',
              key: 'status',
              width: 110,
              render: (_, item) => <StatusPill value={nodeStatus(item)} />,
            },
            {
              title: '角色',
              key: 'roles',
              width: 150,
              render: (_, item) => (
                <Space size={4} wrap>
                  {nodeRoles(item).map((role) => (
                    <Tag key={role} style={{ marginInlineEnd: 0 }}>
                      {role}
                    </Tag>
                  ))}
                </Space>
              ),
            },
            {
              title: '调度',
              key: 'schedulable',
              width: 100,
              render: (_, item) => {
                const unschedulable = record(item.spec).unschedulable === true
                return unschedulable ? <StatusPill value="已停止调度" /> : <span className="kd-muted">可调度</span>
              },
            },
            {
              title: '版本',
              key: 'version',
              width: 130,
              render: (_, item) => {
                const info = record(item.status?.nodeInfo)
                return <span className="kd-mono">{String(info.kubeletVersion ?? '-')}</span>
              },
            },
            {
              title: '可分配 CPU',
              key: 'cpu',
              width: 120,
              render: (_, item) => {
                const allocatable = record(item.status?.allocatable)
                return formatCPU(parseCPU(allocatable.cpu))
              },
            },
            {
              title: '可分配内存',
              key: 'memory',
              width: 120,
              render: (_, item) => {
                const allocatable = record(item.status?.allocatable)
                return formatMemory(parseMemory(allocatable.memory))
              },
            },
            {
              title: 'Age',
              key: 'age',
              width: 80,
              render: (_, item) => ageOf(item.metadata.creationTimestamp),
            },
          ]}
        />
      </Card>
    </div>
  )
}