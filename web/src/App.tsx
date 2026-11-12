import { useMemo, useState } from 'react'
import { Alert, Badge, Button, Empty, Layout, Menu, Select, Space, Spin, Tag, Typography } from 'antd'
import type { MenuProps } from 'antd'
import { ClusterOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { api, type ApiError, type ClusterInfo, type ResourceItem } from './api'
import { RESOURCE_TYPES } from './resourceTypes'
import ResourceTable from './components/ResourceTable'
import DetailDrawer from './components/DetailDrawer'

const { Header, Sider, Content } = Layout
const { Title, Text } = Typography

const menuItems: MenuProps['items'] = Object.entries(
  RESOURCE_TYPES.reduce<Record<string, typeof RESOURCE_TYPES>>((acc, item) => {
    ;(acc[item.group] ??= []).push(item)
    return acc
  }, {}),
).map(([group, types]) => ({
  key: group,
  type: 'group' as const,
  label: group,
  children: types.map((t) => ({ key: t.key, label: t.label })),
}))

export default function App() {
  const [clusterName, setClusterName] = useState<string>()
  const [namespace, setNamespace] = useState('')
  const [typeKey, setTypeKey] = useState('pods')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<ResourceItem>()

  const clusters = useQuery({ queryKey: ['clusters'], queryFn: api.clusters, refetchInterval: 30_000 })
  const cluster: ClusterInfo | undefined = useMemo(() => {
    const list = clusters.data ?? []
    return list.find((c) => c.name === clusterName) ?? list[0]
  }, [clusters.data, clusterName])

  const type = RESOURCE_TYPES.find((t) => t.key === typeKey) ?? RESOURCE_TYPES[0]

  const namespaces = useQuery({
    queryKey: ['namespaces', cluster?.name],
    queryFn: () => api.namespaces(cluster!.name),
    enabled: !!cluster,
  })

  const list = useQuery({
    queryKey: ['list', cluster?.name, type.key, namespace],
    queryFn: () => api.list(cluster!.name, type.path[0], type.path[1], type.path[2], namespace),
    enabled: !!cluster,
    refetchInterval: 15_000,
  })

  const items = useMemo(() => {
    const all = list.data?.items ?? []
    const q = search.trim().toLowerCase()
    return q ? all.filter((i) => i.metadata.name.toLowerCase().includes(q)) : all
  }, [list.data, search])

  if (clusters.isLoading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  const error = (clusters.error ?? list.error) as ApiError | null

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          background: '#fff',
          borderBottom: '1px solid #f0f0f0',
          paddingInline: 20,
        }}
      >
        <ClusterOutlined style={{ fontSize: 20, color: '#1677ff' }} />
        <Title level={4} style={{ margin: 0 }}>
          KubeDeck
        </Title>
        <Text type="secondary">集群驾驶舱</Text>
        <div style={{ flex: 1 }} />
        {clusters.data && clusters.data.length > 1 && (
          <Select
            value={cluster?.name}
            onChange={setClusterName}
            style={{ width: 160 }}
            options={clusters.data.map((c) => ({ value: c.name, label: c.name }))}
          />
        )}
        {cluster && (
          <Space size={12}>
            <Badge status={cluster.status === 'healthy' ? 'success' : 'error'} text={cluster.name} />
            <Tag>{cluster.version || 'unknown'}</Tag>
          </Space>
        )}
        <Button
          icon={<ReloadOutlined />}
          onClick={() => {
            void clusters.refetch()
            void namespaces.refetch()
            void list.refetch()
          }}
        >
          刷新
        </Button>
      </Header>

      <Layout>
        <Sider width={224} theme="light" style={{ borderRight: '1px solid #f0f0f0' }}>
          <div style={{ padding: '12px 16px' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              命名空间
            </Text>
            <Select
              style={{ width: '100%', marginTop: 6 }}
              value={namespace}
              onChange={setNamespace}
              disabled={!type.namespaced}
              options={[
                { value: '', label: '全部命名空间' },
                ...(namespaces.data?.namespaces ?? []).map((n) => ({ value: n, label: n })),
              ]}
            />
          </div>
          <Menu
            mode="inline"
            selectedKeys={[type.key]}
            items={menuItems}
            onClick={(e) => {
              setTypeKey(e.key)
              setSearch('')
            }}
            style={{ borderInlineEnd: 0 }}
          />
        </Sider>

        <Content style={{ padding: 16, background: '#fff' }}>
          {error && (
            <Alert
              type="error"
              showIcon
              style={{ marginBottom: 16 }}
              message={`${error.code}: ${error.message}`}
              description={error.requestId ? `requestId: ${error.requestId}` : undefined}
            />
          )}
          {!cluster ? (
            <Empty description="未注册任何集群：请用 KUBEDECK_DEV_KUBECONFIG 指定 kubeconfig 启动" />
          ) : (
            <ResourceTable
              type={type}
              items={items}
              loading={list.isFetching}
              search={search}
              onSearch={setSearch}
              onSelect={setSelected}
              namespace={namespace}
            />
          )}
        </Content>
      </Layout>

      <DetailDrawer cluster={cluster?.name} type={type} item={selected} onClose={() => setSelected(undefined)} />
    </Layout>
  )
}