import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Input,
  Result,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import {
  DeleteOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type ResourceItem } from '../../lib/api'
import { useResourceRealtime } from '../../lib/useRealtime'
import PageHeader from '../../components/PageHeader'
import { EmptyState, ErrorState, PageSkeleton } from '../../components/StateViews'
import ResourceTable, { itemKey } from '../../components/ResourceTable'
import { useCan } from '../auth/useCan'
import { useResourceList } from './useResourceList'
import { isCreatable, isCrashLoop, resolveType } from './resourceTypes'

const PAGE_LIMIT = 500

export default function ResourcesPage() {
  const { cluster, typeKey } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()

  const type = resolveType(typeKey)
  const namespace = searchParams.get('namespace') ?? ''
  const continueToken = searchParams.get('continue') ?? ''
  const urlSearch = searchParams.get('search') ?? ''
  const labelSelector = searchParams.get('label') ?? ''
  const urlPodFilter = searchParams.get('pods') === 'crashloop' ? 'crashloop' : 'all'

  const [search, setSearch] = useState(urlSearch)
  const [labelInput, setLabelInput] = useState(labelSelector)
  const [podFilter, setPodFilter] = useState<'all' | 'crashloop'>(urlPodFilter)
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const [batchOpen, setBatchOpen] = useState(false)
  const [batchRunning, setBatchRunning] = useState(false)

  useEffect(() => setSearch(urlSearch), [urlSearch])
  useEffect(() => setLabelInput(labelSelector), [labelSelector])
  useEffect(() => setPodFilter(urlPodFilter), [urlPodFilter])
  useEffect(() => {
    setSelectedKeys([])
  }, [cluster, typeKey, namespace])

  const listQuery = useResourceList(cluster, type ?? { key: '', label: '', kind: '', group: '集群', gvr: { group: '', version: 'v1', resource: 'pods' }, namespaced: true }, {
    namespace,
    continueToken,
    labelSelector,
    limit: PAGE_LIMIT,
  })

  const queryKey = useMemo(
    () => [
      'resources',
      cluster,
      type?.key,
      namespace,
      labelSelector,
      '',
      continueToken,
      PAGE_LIMIT,
      '',
      '',
    ],
    [cluster, type?.key, namespace, labelSelector, continueToken],
  )

  useResourceRealtime({
    cluster,
    gvr: type?.gvr ?? { group: '', version: 'v1', resource: 'pods' },
    namespace,
    labelSelector,
    queryKey,
    resourceVersion: listQuery.data?.resourceVersion,
    enabled: !!cluster && !!type && !continueToken,
  })

  const creatable = type ? isCreatable(type) : false
  const createScopeReady = type?.namespaced ? !!namespace : true
  const createCan = useCan({
    cluster,
    gvr: type?.gvr ?? { group: '', version: 'v1', resource: 'pods' },
    namespace: type?.namespaced ? namespace : undefined,
    verb: 'create',
    enabled: !!type && creatable && createScopeReady,
  })

  const deleteCan = useCan({
    cluster,
    gvr: type?.gvr ?? { group: '', version: 'v1', resource: 'pods' },
    namespace,
    verb: type?.key === 'namespaces' ? 'delete' : 'deletecollection',
    enabled: !!type && type.key !== 'namespaces',
  })

  const items = useMemo(() => {
    let all = listQuery.data?.items ?? []
    const keyword = search.trim().toLowerCase()
    if (keyword) all = all.filter((item) => item.metadata.name.toLowerCase().includes(keyword))
    if (type?.key === 'pods' && podFilter === 'crashloop') all = all.filter(isCrashLoop)
    return all
  }, [listQuery.data, search, type?.key, podFilter])

  const updateSearch = (next: Record<string, string | undefined>) => {
    const params = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(next)) {
      if (value) params.set(key, value)
      else params.delete(key)
    }
    params.delete('continue')
    setSearchParams(params, { replace: true })
  }

  const openDetail = (item: ResourceItem) => {
    if (!type) return
    const ns = item.metadata.namespace ?? '_'
    const suffix = item.metadata.namespace
      ? `?namespace=${encodeURIComponent(item.metadata.namespace)}`
      : ''
    navigate(
      `/clusters/${encodeURIComponent(cluster!)}/${type.key}/${encodeURIComponent(ns)}/${encodeURIComponent(item.metadata.name)}${suffix}`,
    )
  }

  const runBatchDelete = async () => {
    if (!cluster || !type) return
    setBatchRunning(true)
    const targets = items.filter((item) => selectedKeys.includes(itemKey(item)))
    const results = await Promise.allSettled(
      targets.map((item) =>
        api.remove({
          cluster,
          gvr: type.gvr,
          namespace: item.metadata.namespace,
          name: item.metadata.name,
          propagationPolicy: 'Background',
        }),
      ),
    )
    const failed = results.filter((result) => result.status === 'rejected').length
    const succeeded = results.length - failed
    if (failed > 0) {
      message.warning(`已删除 ${succeeded} 项，失败 ${failed} 项`)
    } else {
      message.success(`已删除 ${succeeded} 项`)
    }
    setBatchRunning(false)
    setBatchOpen(false)
    setSelectedKeys([])
    await queryClient.invalidateQueries({ queryKey })
  }

  if (!type) {
    return (
      <Result
        status="404"
        title="未知的资源类型"
        subTitle={`地址中的类型 "${typeKey}" 不在导航中。`}
        extra={<Button onClick={() => navigate(-1)}>返回</Button>}
      />
    )
  }

  if (listQuery.error instanceof ApiError && listQuery.error.code === 'FORBIDDEN') {
    return (
      <Result
        status="403"
        title="无访问权限"
        subTitle="当前账号无权访问该命名空间或资源类型。"
        extra={
          <Button onClick={() => navigate(`/clusters/${encodeURIComponent(cluster ?? '')}/overview`)}>
            返回总览
          </Button>
        }
      />
    )
  }

  const filterActive = !!(search || labelSelector || podFilter !== 'all')
  const selectedItems = items.filter((item) => selectedKeys.includes(itemKey(item)))

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `/clusters/${encodeURIComponent(cluster ?? '')}/overview` },
          ...(namespace ? [{ label: namespace }] : []),
          { label: type.label },
        ]}
        title={type.label}
        subtitle={
          <span className="kd-muted">
            {items.length} 项
            {continueToken ? ' · 分页视图' : ''}
          </span>
        }
        tags={namespace ? <Tag>{namespace}</Tag> : <Tag>全部命名空间</Tag>}
        actions={
          <Space wrap>
            {creatable && createScopeReady && (createCan.loading || createCan.allowed) && (
              <Button
                type="primary"
                icon={<PlusOutlined />}
                disabled={createCan.loading}
                onClick={() =>
                  navigate(
                    `/clusters/${encodeURIComponent(cluster!)}/new/${type.key}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''}`,
                  )
                }
              >
                新建
              </Button>
            )}
            <Button icon={<ReloadOutlined />} onClick={() => void listQuery.refetch()}>
              刷新
            </Button>
          </Space>
        }
      />

      <Card className="kd-table-card" styles={{ body: { padding: 0 } }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            flexWrap: 'wrap',
            padding: '12px 16px',
            borderBottom: '1px solid #f1f5f9',
          }}
        >
          <Input
            allowClear
            aria-label="按名称过滤"
            placeholder="按名称过滤"
            prefix={<SearchOutlined />}
            value={search}
            onChange={(event) => {
              setSearch(event.target.value)
              updateSearch({ search: event.target.value || undefined })
            }}
            style={{ width: 220 }}
          />
          <Input
            allowClear
            aria-label="标签选择器"
            placeholder="labelSelector，如 app=web"
            value={labelInput}
            onChange={(event) => setLabelInput(event.target.value)}
            onPressEnter={() => updateSearch({ label: labelInput || undefined })}
            style={{ width: 220 }}
          />
          {type.key === 'pods' && (
            <Select
              aria-label="状态过滤"
              value={podFilter}
              onChange={(next) => {
                setPodFilter(next)
                updateSearch({ pods: next === 'crashloop' ? 'crashloop' : undefined })
              }}
              style={{ width: 160 }}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'crashloop', label: 'CrashLoopBackOff' },
              ]}
            />
          )}
          {filterActive && (
            <Button
              type="link"
              onClick={() => {
                setSearch('')
                setPodFilter('all')
                setLabelInput('')
                const params = new URLSearchParams(searchParams)
                params.delete('search')
                params.delete('label')
                params.delete('pods')
                params.delete('continue')
                setSearchParams(params, { replace: true })
              }}
            >
              清除筛选
            </Button>
          )}
        </div>

        {listQuery.isLoading ? (
          <div style={{ padding: 16 }}>
            <PageSkeleton rows={8} />
          </div>
        ) : listQuery.isError ? (
          <div style={{ padding: 16 }}>
            <ErrorState error={listQuery.error} onRetry={() => void listQuery.refetch()} />
          </div>
        ) : items.length === 0 ? (
          <EmptyState
            description={filterActive ? '没有匹配的资源' : '该视图暂无资源'}
            action={
              creatable && createScopeReady && createCan.allowed ? (
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() =>
                    navigate(
                      `/clusters/${encodeURIComponent(cluster!)}/new/${type.key}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''}`,
                    )
                  }
                >
                  新建 {type.label}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <ResourceTable
            type={type}
            items={items}
            loading={listQuery.isFetching && !listQuery.isLoading}
            namespace={namespace}
            selectedKeys={selectedKeys}
            onSelectionChange={setSelectedKeys}
            onSelect={openDetail}
          />
        )}

        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            alignItems: 'center',
            gap: 8,
            padding: 12,
          }}
        >
          {continueToken && (
            <Button onClick={() => updateSearch({ continue: undefined })}>回到第一页</Button>
          )}
          {listQuery.data?.continue && (
            <Button
              type="primary"
              onClick={() => updateSearch({ continue: listQuery.data?.continue })}
            >
              下一页
            </Button>
          )}
        </div>
      </Card>

      {selectedItems.length > 0 && (
        <div className="kd-action-bar">
          <span style={{ fontWeight: 550 }}>已选 {selectedItems.length} 项</span>
          <Button type="text" size="small" onClick={() => setSelectedKeys([])}>
            清除选择
          </Button>
          <div style={{ flex: 1 }} />
          {(deleteCan.loading || deleteCan.allowed) && (
            <Button
              danger
              type="primary"
              icon={<DeleteOutlined />}
              disabled={deleteCan.loading}
              onClick={() => setBatchOpen(true)}
            >
              删除所选
            </Button>
          )}
        </div>
      )}

      <Drawer
        title={`删除 ${selectedItems.length} 个 ${type.label}`}
        width={560}
        open={batchOpen}
        onClose={() => setBatchOpen(false)}
        extra={
          <Button danger type="primary" loading={batchRunning} onClick={() => void runBatchDelete()}>
            确认删除
          </Button>
        }
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
          message="删除后无法撤销"
          description="每个对象会携带自己的命名空间提交；结果逐项返回。"
        />
        <Table
          size="small"
          rowKey={itemKey}
          pagination={false}
          dataSource={selectedItems}
          scroll={{ y: 320 }}
          columns={[
            {
              title: '命名空间',
              dataIndex: ['metadata', 'namespace'],
              width: 150,
              render: (value: string) => value ?? <span className="kd-muted">集群级</span>,
            },
            {
              title: '名称',
              dataIndex: ['metadata', 'name'],
              render: (value: string) => <span className="kd-mono">{value}</span>,
            },
          ]}
        />
        <Typography.Paragraph className="kd-muted" style={{ marginTop: 12, fontSize: 12 }}>
          当前视图：{namespace || '全部命名空间'} · {type.label}
        </Typography.Paragraph>
        {selectedItems.length > 20 && (
          <Alert
            type="info"
            showIcon
            style={{ marginTop: 12 }}
            message="对象较多时会逐个提交，请耐心等待结果"
          />
        )}
      </Drawer>
    </div>
  )
}