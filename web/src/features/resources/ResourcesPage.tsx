import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Empty,
  Input,
  Modal,
  Result,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd'
import { DeleteOutlined, PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type ResourceItem } from '../../lib/api'
import { useResourceRealtime } from '../../lib/useRealtime'
import ErrorAlert from '../../components/ErrorAlert'
import ResourceTable, { itemKey } from '../../components/ResourceTable'
import { useCan } from '../auth/useCan'
import { useResourceList } from './useResourceList'
import { isCreatable, isCrashLoop, resolveType, RESOURCE_TYPES } from './resourceTypes'
import ResourceCreateModal from './ResourceCreateModal'

const PAGE_LIMIT = 500

export default function ResourcesPage() {
  const { cluster, typeKey } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const type = resolveType(typeKey) ?? RESOURCE_TYPES[0]
  const namespace = searchParams.get('namespace') ?? ''
  const continueToken = searchParams.get('continue') ?? ''
  const urlSearch = searchParams.get('search') ?? ''
  const labelSelector = searchParams.get('label') ?? ''

  const [search, setSearch] = useState(urlSearch)
  const [labelInput, setLabelInput] = useState(labelSelector)
  const [podFilter, setPodFilter] = useState<'all' | 'crashloop'>('all')
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const [batchOpen, setBatchOpen] = useState(false)
  const [batchRunning, setBatchRunning] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)

  useEffect(() => setSearch(urlSearch), [urlSearch])
  useEffect(() => setLabelInput(labelSelector), [labelSelector])
  useEffect(() => {
    setSelectedKeys([])
    setPodFilter('all')
  }, [cluster, type.key, namespace])

  const listQuery = useResourceList(cluster, type, {
    namespace,
    continueToken,
    labelSelector,
    limit: PAGE_LIMIT,
  })

  const queryKey = useMemo(
    () => [
      'resources',
      cluster,
      type.key,
      namespace,
      labelSelector,
      '',
      continueToken,
      PAGE_LIMIT,
      '',
      '',
    ],
    [cluster, type.key, namespace, labelSelector, continueToken],
  )

  useResourceRealtime({
    cluster,
    gvr: type.gvr,
    namespace,
    queryKey,
    resourceVersion: listQuery.data?.resourceVersion,
    enabled: !!cluster && !continueToken,
  })

  const creatable = isCreatable(type)
  const createScopeReady = type.namespaced ? !!namespace : true
  const createCan = useCan({
    cluster,
    gvr: type.gvr,
    namespace: type.namespaced ? namespace : undefined,
    verb: 'create',
    enabled: creatable && createScopeReady,
  })

  const deleteCan = useCan({
    cluster,
    gvr: type.gvr,
    namespace,
    verb: type.key === 'namespaces' ? 'delete' : 'deletecollection',
    enabled: type.key !== 'namespaces',
  })

  const items = useMemo(() => {
    let all = listQuery.data?.items ?? []
    const q = search.trim().toLowerCase()
    if (q) all = all.filter((item) => item.metadata.name.toLowerCase().includes(q))
    if (type.key === 'pods' && podFilter === 'crashloop') all = all.filter(isCrashLoop)
    return all
  }, [listQuery.data, search, type.key, podFilter])

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
    const ns = item.metadata.namespace ?? '_'
    const suffix = item.metadata.namespace
      ? `?namespace=${encodeURIComponent(item.metadata.namespace)}`
      : ''
    navigate(
      `/clusters/${encodeURIComponent(cluster!)}/${type.key}/${encodeURIComponent(ns)}/${encodeURIComponent(item.metadata.name)}${suffix}`,
    )
  }

  const runBatchDelete = async () => {
    if (!cluster) return
    setBatchRunning(true)
    try {
      const result = await api.batchRemove({
        cluster,
        gvr: type.gvr,
        namespace,
        names: selectedKeys.map((key) => {
          const item = listQuery.data?.items.find((i) => itemKey(i) === key)
          return item?.metadata.name ?? key
        }),
        propagationPolicy: 'Background',
      })
      if (result.failed > 0) {
        message.warning(`已完成 ${result.succeeded} 项，失败 ${result.failed} 项`)
      } else {
        message.success(`已删除 ${result.succeeded} 项`)
      }
      setSelectedKeys([])
      setBatchOpen(false)
      await queryClient.invalidateQueries({ queryKey })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '批量删除失败')
    } finally {
      setBatchRunning(false)
    }
  }

  if (listQuery.error instanceof ApiError && listQuery.error.code === 'FORBIDDEN') {
    return (
      <Result status="403" title="无访问权限" subTitle="当前账号无权访问该命名空间或资源类型。" />
    )
  }

  const filterActive = !!(search || labelSelector || podFilter !== 'all')
  const selectedItems = (listQuery.data?.items ?? []).filter((item) =>
    selectedKeys.includes(itemKey(item)),
  )

  return (
    <div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
          marginBottom: 12,
          flexWrap: 'wrap',
        }}
      >
        <Space size={8}>
          <Typography.Title level={5} style={{ margin: 0 }}>
            {type.label}
          </Typography.Title>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {items.length} 项
          </Typography.Text>
          {namespace && <Tag>{namespace}</Tag>}
        </Space>
        <Space wrap>
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
              onChange={setPodFilter}
              style={{ width: 150 }}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'crashloop', label: 'CrashLoopBackOff' },
              ]}
            />
          )}
          {creatable && createScopeReady && (createCan.loading || createCan.allowed) && (
            <Button
              type="primary"
              icon={<PlusOutlined />}
              disabled={createCan.loading}
              onClick={() => setCreateOpen(true)}
            >
              新建
            </Button>
          )}
          {creatable && type.namespaced && !namespace && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              选择命名空间后可新建
            </Typography.Text>
          )}
          <Button
            icon={<ReloadOutlined />}
            onClick={() => {
              void listQuery.refetch()
            }}
          >
            刷新
          </Button>
          {selectedKeys.length > 0 && (deleteCan.loading || deleteCan.allowed) && (
            <Button
              danger
              icon={<DeleteOutlined />}
              disabled={deleteCan.loading}
              onClick={() => setBatchOpen(true)}
            >
              删除所选（{selectedKeys.length}）
            </Button>
          )}
        </Space>
      </div>

      <ErrorAlert error={listQuery.error} />

      {listQuery.isLoading ? (
        <div style={{ padding: 64, textAlign: 'center' }}>
          <Spin />
        </div>
      ) : items.length === 0 ? (
        <Empty
          description={filterActive ? '没有匹配的资源' : '该视图暂无资源'}
          style={{ padding: 48 }}
        >
          {filterActive && (
            <Button
              onClick={() => {
                setSearch('')
                setPodFilter('all')
                setLabelInput('')
                const params = new URLSearchParams(searchParams)
                params.delete('search')
                params.delete('label')
                params.delete('continue')
                setSearchParams(params, { replace: true })
              }}
            >
              清除筛选
            </Button>
          )}
        </Empty>
      ) : (
        <>
          <ResourceTable
            type={type}
            items={items}
            loading={listQuery.isFetching}
            namespace={namespace}
            selectedKeys={selectedKeys}
            onSelectionChange={setSelectedKeys}
            onSelect={openDetail}
          />
          <div
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              alignItems: 'center',
              gap: 8,
              marginTop: 12,
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
        </>
      )}

      <Modal
        open={batchOpen}
        title={`删除 ${selectedItems.length} 个 ${type.label}`}
        okText="确认删除"
        okButtonProps={{ danger: true, loading: batchRunning }}
        onOk={() => void runBatchDelete()}
        onCancel={() => setBatchOpen(false)}
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="删除后无法撤销，请确认以下对象"
        />
        <ul style={{ maxHeight: 240, overflow: 'auto', paddingLeft: 20 }}>
          {selectedItems.map((item) => (
            <li key={itemKey(item)}>
              {item.metadata.namespace ? `${item.metadata.namespace}/` : ''}
              {item.metadata.name}
            </li>
          ))}
        </ul>
      </Modal>

      {cluster && (
        <ResourceCreateModal
          open={createOpen}
          cluster={cluster}
          type={type}
          namespace={namespace}
          onCancel={() => setCreateOpen(false)}
          onCreated={(item, pageKey) => {
            setCreateOpen(false)
            void queryClient.invalidateQueries({ queryKey: ['resources'] })
            if (!pageKey || pageKey === type.key) {
              openDetail(item)
              return
            }
            // YAML 创建了当前页面之外的类型：跳到该类型的内置页面。
            const ns = item.metadata.namespace ?? '_'
            const suffix = item.metadata.namespace
              ? `?namespace=${encodeURIComponent(item.metadata.namespace)}`
              : ''
            navigate(
              `/clusters/${encodeURIComponent(cluster!)}/${pageKey}/${encodeURIComponent(ns)}/${encodeURIComponent(item.metadata.name)}${suffix}`,
            )
          }}
        />
      )}
    </div>
  )
}
