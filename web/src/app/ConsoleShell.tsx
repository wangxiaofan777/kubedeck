import {
  Navigate,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom'
import {
  Alert,
  Avatar,
  Badge,
  Button,
  Dropdown,
  Layout,
  Menu,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd'
import {
  AuditOutlined,
  ClusterOutlined,
  LogoutOutlined,
  ReloadOutlined,
  UserOutlined,
} from '@ant-design/icons'
import type { MenuProps } from 'antd'
import { useAuth } from './AuthProvider'
import { useClusters } from '../features/clusters/useClusters'
import { useNamespaces } from '../features/namespaces/useNamespaces'
import GlobalSearch from '../features/search/GlobalSearch'
import { RESOURCE_TYPES, resolveType, type ResourceType } from '../features/resources/resourceTypes'

const { Header, Sider, Content } = Layout
const { Title, Text } = Typography

function buildMenuItems(): MenuProps['items'] {
  const groups = new Map<string, ResourceType[]>()
  for (const item of RESOURCE_TYPES) {
    const list = groups.get(item.group) ?? []
    list.push(item)
    groups.set(item.group, list)
  }
  return Array.from(groups, ([label, types]) => ({
    key: label,
    type: 'group' as const,
    label,
    children: types.map((t) => ({ key: t.key, label: t.label })),
  }))
}

const MENU_ITEMS = buildMenuItems()

/** ConsoleShell 负责登录校验、上下文导航与全局状态提示。 */
export default function ConsoleShell() {
  const { me, loading, error, isDev, logout } = useAuth()
  const params = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const clustersQuery = useClusters()

  const clusters = clustersQuery.data ?? []
  const cluster = clusters.find((c) => c.name === params.cluster)
  const type = resolveType(params.typeKey) ?? RESOURCE_TYPES[0]
  const namespace = searchParams.get('namespace') ?? ''
  const namespacesQuery = useNamespaces(cluster?.name)
  const isAuditPage = location.pathname.endsWith('/audit')

  if (loading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" />
      </div>
    )
  }
  if (error) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <Alert
          type="error"
          showIcon
          message="无法获取登录状态"
          description="请刷新页面或重新登录"
        />
      </div>
    )
  }
  if (!me) return null

  if (!params.cluster && clusters.length > 0) {
    return <Navigate to={`/clusters/${encodeURIComponent(clusters[0].name)}/pods`} replace />
  }

  const selectCluster = (name: string) => {
    const nextNamespace = type.namespaced ? namespace : ''
    navigate(
      `/clusters/${encodeURIComponent(name)}/${type.key}${nextNamespace ? `?namespace=${encodeURIComponent(nextNamespace)}` : ''}`,
    )
  }

  const selectType = (key: string) => {
    const next = resolveType(key)
    const nextNamespace = next?.namespaced ? namespace : ''
    navigate(
      `/clusters/${encodeURIComponent(cluster!.name)}/${key}${nextNamespace ? `?namespace=${encodeURIComponent(nextNamespace)}` : ''}`,
    )
  }

  const selectNamespace = (value: string) => {
    if (!cluster) return
    const next = new URLSearchParams(searchParams)
    if (value) next.set('namespace', value)
    else next.delete('namespace')
    next.delete('continue')
    // 详情页切换命名空间时回到列表，避免打开其他命名空间下的同名资源。
    const onDetail = location.pathname.split('/').filter(Boolean).length >= 5
    const pathname = onDetail
      ? `/clusters/${encodeURIComponent(cluster.name)}/${type.key}`
      : location.pathname
    navigate({ pathname, search: next.toString() })
  }

  const menuItems: MenuProps['items'] = [
    ...(MENU_ITEMS ?? []),
    ...(me.user.isPlatformAdmin
      ? [
          { type: 'divider' as const },
          {
            key: 'audit',
            icon: <AuditOutlined />,
            label: '审计日志',
          },
        ]
      : []),
  ]

  const onMenuClick: MenuProps['onClick'] = (event) => {
    if (!cluster) return
    if (event.key === 'audit') {
      navigate(`/clusters/${encodeURIComponent(cluster.name)}/audit`)
      return
    }
    selectType(event.key)
  }

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
        {isDev && <Tag color="orange">开发模式</Tag>}
        <div style={{ flex: 1 }} />

        {clusters.length > 0 && <GlobalSearch cluster={cluster?.name} namespace={namespace} />}
        {clusters.length > 1 && (
          <Select
            aria-label="切换集群"
            value={cluster?.name}
            onChange={selectCluster}
            style={{ width: 160 }}
            options={clusters.map((c) => ({ value: c.name, label: c.name }))}
          />
        )}
        {cluster && (
          <Space size={12}>
            <Badge
              status={cluster.status === 'healthy' ? 'success' : 'error'}
              text={cluster.name}
            />
            <Tag>{cluster.version || 'unknown'}</Tag>
          </Space>
        )}
        <Button
          aria-label="刷新"
          icon={<ReloadOutlined />}
          onClick={() => {
            void clustersQuery.refetch()
            void namespacesQuery.refetch()
          }}
        />
        <Dropdown
          menu={{
            items: [
              {
                key: 'logout',
                icon: <LogoutOutlined />,
                label: '退出登录',
                onClick: () => void logout(),
              },
            ],
          }}
        >
          <Button type="text" icon={<Avatar size="small" icon={<UserOutlined />} />}>
            {me.user.displayName || me.user.email || me.user.subject}
          </Button>
        </Dropdown>
      </Header>

      <Layout>
        <Sider width={224} theme="light" style={{ borderRight: '1px solid #f0f0f0' }}>
          <div style={{ padding: '12px 16px' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              命名空间
            </Text>
            <Select
              aria-label="命名空间"
              style={{ width: '100%', marginTop: 6 }}
              value={namespace}
              onChange={selectNamespace}
              disabled={!type.namespaced || !cluster || isAuditPage}
              options={[
                { value: '', label: '全部命名空间' },
                ...(namespacesQuery.data?.namespaces ?? []).map((n) => ({
                  value: n,
                  label: n,
                })),
              ]}
            />
          </div>
          <nav aria-label="资源类型">
            <Menu
              mode="inline"
              selectedKeys={[isAuditPage ? 'audit' : type.key]}
              items={menuItems}
              onClick={onMenuClick}
              style={{ borderInlineEnd: 0 }}
            />
          </nav>
        </Sider>

        <Content style={{ padding: 16, background: '#fff' }}>
          {clustersQuery.isLoading ? (
            <div style={{ display: 'grid', placeItems: 'center', minHeight: 240 }}>
              <Spin />
            </div>
          ) : !cluster ? (
            clusters.length === 0 ? (
              clustersQuery.isError ? (
                <Alert
                  type="error"
                  showIcon
                  message="无法加载集群列表"
                  description={
                    clustersQuery.error instanceof Error
                      ? clustersQuery.error.message
                      : '请检查网络或稍后重试。'
                  }
                  action={
                    <Button size="small" onClick={() => void clustersQuery.refetch()}>
                      重试
                    </Button>
                  }
                />
              ) : (
                <Alert
                  type="info"
                  showIcon
                  message="未注册任何集群"
                  description="请以管理员身份接入集群，或使用 KUBEDECK_DEV_KUBECONFIG 启动开发模式。"
                />
              )
            ) : (
              <Alert
                type="warning"
                showIcon
                message={`集群不存在：${params.cluster ?? ''}`}
                description="该集群可能已被移除，或当前账号无权访问。"
                action={
                  <Button
                    size="small"
                    onClick={() =>
                      navigate(`/clusters/${encodeURIComponent(clusters[0].name)}/pods`, {
                        replace: true,
                      })
                    }
                  >
                    切换到 {clusters[0].name}
                  </Button>
                }
              />
            )
          ) : (
            <>
              {clustersQuery.isError && (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 12 }}
                  message="集群列表刷新失败，正在显示上次结果"
                  description={
                    clustersQuery.error instanceof Error ? clustersQuery.error.message : undefined
                  }
                  action={
                    <Button size="small" onClick={() => void clustersQuery.refetch()}>
                      重试
                    </Button>
                  }
                />
              )}
              {cluster?.status === 'unreachable' && (
                <Alert
                  type="error"
                  showIcon
                  style={{ marginBottom: 12 }}
                  message="当前集群不可达"
                  description={cluster.message || '平台会在后台持续探测，恢复后自动刷新。'}
                />
              )}
              {cluster?.status === 'degraded' && (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 12 }}
                  message="当前集群状态降级"
                  description={cluster.message || '部分操作可能失败。'}
                />
              )}
              <Outlet />
            </>
          )}
        </Content>
      </Layout>
    </Layout>
  )
}
