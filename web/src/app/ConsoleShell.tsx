import { useEffect, useMemo, useState } from 'react'
import {
  Outlet,
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom'
import { Alert, Avatar, Button, Dropdown, Layout, Menu, Select, Space, Spin, Tag } from 'antd'
import {
  ApartmentOutlined,
  AuditOutlined,
  ClusterOutlined,
  DashboardOutlined,
  LogoutOutlined,
  NodeIndexOutlined,
  PlusOutlined,
  SearchOutlined,
  SettingOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from '@ant-design/icons'
import type { MenuProps } from 'antd'
import { useAuth } from './AuthProvider'
import { useClusters } from '../features/clusters/useClusters'
import { useNamespaces } from '../features/namespaces/useNamespaces'
import CommandPalette from '../features/search/CommandPalette'
import { RESOURCE_TYPES } from '../features/resources/resourceTypes'
import { ApiError } from '../lib/api'
import { ErrorState } from '../components/StateViews'

const { Header, Sider, Content } = Layout

/** ConsoleShell 负责登录校验、全局导航与上下文切换。 */
export default function ConsoleShell() {
  const { me, loading, error, isDev, logout } = useAuth()
  const params = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const clustersQuery = useClusters()
  const [commandOpen, setCommandOpen] = useState(false)

  const clusters = clustersQuery.data ?? []
  const cluster = clusters.find((item) => item.name === params.cluster)
  const type = RESOURCE_TYPES.find((item) => item.key === params.typeKey)
  // 详情页把命名空间放在路径里，列表页放在查询参数里，两者都要反映到上下文条。
  const namespace = searchParams.get('namespace') ?? params.namespace ?? ''
  const namespacesQuery = useNamespaces(cluster?.name)

  const isAudit = location.pathname.endsWith('/audit')
  const isOverview = location.pathname.endsWith('/overview')
  const isNodes = location.pathname.includes('/nodes')
  const isSettings = location.pathname.endsWith('/settings')
  const isCreate = location.pathname.includes('/new/')
  const isAdminPage = location.pathname === '/clusters' || location.pathname === '/clusters/new'
  const isDetail = !!params.name && !isNodes

  const clusterWide = useMemo(
    () =>
      !!me?.user.isPlatformAdmin ||
      (me?.bindings ?? []).some(
        (binding) => binding.clusterName === cluster?.name && binding.namespace === '*',
      ),
    [me, cluster?.name],
  )
  const availableNamespaces = namespacesQuery.data?.namespaces ?? []

  // 命名空间级用户首次进入资源页时自动选中首个可访问命名空间。
  useEffect(() => {
    if (
      !me ||
      !cluster ||
      !type?.namespaced ||
      namespace ||
      clusterWide ||
      isAudit ||
      isOverview ||
      isNodes ||
      isSettings ||
      isCreate
    )
      return
    const first = namespacesQuery.data?.namespaces?.[0]
    if (!first) return
    const next = new URLSearchParams(searchParams)
    next.set('namespace', first)
    next.delete('continue')
    navigate({ pathname: location.pathname, search: next.toString() }, { replace: true })
  }, [
    me,
    cluster,
    type?.namespaced,
    namespace,
    clusterWide,
    isAudit,
    isOverview,
    isNodes,
    isSettings,
    isCreate,
    namespacesQuery.data,
    searchParams,
    navigate,
    location.pathname,
  ])

  // 全局快捷键：Ctrl/⌘ + K。
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setCommandOpen((open) => !open)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // 浏览器标签标题跟随当前层级。
  useEffect(() => {
    const parts = ['KubeDeck']
    if (cluster) parts.unshift(cluster.name)
    if (params.name) parts.unshift(params.name)
    else if (type) parts.unshift(type.label)
    document.title = parts.join(' · ')
  }, [cluster, type, params.name])

  const menuItems: MenuProps['items'] = useMemo(() => {
    const items: MenuProps['items'] = [
      { key: 'overview', icon: <DashboardOutlined />, label: '总览' },
    ]
    if (cluster) {
      const groups = new Map<string, typeof RESOURCE_TYPES>()
      for (const item of RESOURCE_TYPES) {
        if (!clusterWide && !item.namespaced) continue
        const list = groups.get(item.group) ?? []
        list.push(item)
        groups.set(item.group, list)
      }
      items.push({ type: 'divider' })
      for (const [label, types] of groups) {
        items.push({
          key: label,
          type: 'group' as const,
          label,
          children: types.map((item) =>
            item.key === 'nodes'
              ? { key: 'nodes', icon: <NodeIndexOutlined />, label: '节点' }
              : { key: item.key, label: item.label },
          ),
        })
      }
      items.push({ type: 'divider' })
      items.push({ key: 'settings', icon: <SettingOutlined />, label: '集群设置' })
    }
    if (me?.user.isPlatformAdmin) {
      items.push({ key: 'clusters', icon: <ApartmentOutlined />, label: '集群管理' })
      if (cluster) items.push({ key: 'audit', icon: <AuditOutlined />, label: '审计日志' })
    }
    return items
  }, [cluster, clusterWide, me?.user.isPlatformAdmin])

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
        <ErrorState error={error} title="无法获取登录状态" />
      </div>
    )
  }
  if (!me) return null

  const base = cluster ? `/clusters/${encodeURIComponent(cluster.name)}` : ''
  const selectedKey = isOverview
    ? 'overview'
    : isAudit
      ? 'audit'
      : isSettings
        ? 'settings'
        : isNodes
          ? 'nodes'
          : isAdminPage
            ? 'clusters'
            : (type?.key ?? '')

  const onMenuClick: MenuProps['onClick'] = (event) => {
    if (event.key === 'overview' && cluster) {
      navigate(`${base}/overview`)
      return
    }
    if (event.key === 'audit' && cluster) {
      navigate(`${base}/audit`)
      return
    }
    if (event.key === 'settings' && cluster) {
      navigate(`${base}/settings`)
      return
    }
    if (event.key === 'nodes' && cluster) {
      navigate(`${base}/nodes`)
      return
    }
    if (event.key === 'clusters') {
      navigate('/clusters')
      return
    }
    if (!cluster) return
    const next = RESOURCE_TYPES.find((item) => item.key === event.key)
    if (!next) return
    const nextNamespace = next.namespaced ? namespace : ''
    navigate(
      `${base}/${next.key}${nextNamespace ? `?namespace=${encodeURIComponent(nextNamespace)}` : ''}`,
    )
  }

  const selectNamespace = (value: string) => {
    if (!cluster || !type) return
    const next = new URLSearchParams(searchParams)
    if (value) next.set('namespace', value)
    else next.delete('namespace')
    next.delete('continue')
    // 详情页切换命名空间时回到列表，避免停留在其他命名空间的对象上。
    const pathname = isDetail ? `${base}/${type.key}` : location.pathname
    navigate({ pathname, search: next.toString() })
  }

  const showNamespaceBar =
    !!cluster &&
    !!type?.namespaced &&
    !isAudit &&
    !isOverview &&
    !isNodes &&
    !isSettings &&
    !isCreate

  const switchCluster = (name: string) => {
    const oldBase = `/clusters/${encodeURIComponent(params.cluster ?? '')}`
    const suffix = location.pathname.startsWith(oldBase)
      ? location.pathname.slice(oldBase.length)
      : '/overview'
    navigate(`/clusters/${encodeURIComponent(name)}${suffix}${location.search}`)
  }

  const clusterMenu: MenuProps['items'] = [
    ...clusters.map((item) => ({
      key: item.name,
      label: (
        <Space>
          <span className={`kd-health-dot ${item.status}`} />
          <span>{item.name}</span>
          <span className="kd-muted" style={{ fontSize: 11 }}>
            {item.version || 'unknown'}
          </span>
        </Space>
      ),
      onClick: () => switchCluster(item.name),
    })),
    ...(me.user.isPlatformAdmin
      ? [
          { type: 'divider' as const },
          {
            key: 'manage',
            icon: <ApartmentOutlined />,
            label: '管理集群',
            onClick: () => navigate('/clusters'),
          },
          {
            key: 'add',
            icon: <PlusOutlined />,
            label: '接入集群',
            onClick: () => navigate('/clusters/new'),
          },
        ]
      : []),
  ]

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        width={236}
        className="kd-sider"
        style={{ position: 'sticky', top: 0, height: '100vh' }}
      >
        <div style={{ padding: '16px 18px 12px' }}>
          <div className="kd-brand">
            <span className="kd-brand-mark">
              <ThunderboltOutlined />
            </span>
            KubeDeck
          </div>
          <div style={{ marginTop: 4, color: '#64748b', fontSize: 11 }}>集群驾驶舱</div>
        </div>
        {isDev && (
          <div style={{ padding: '0 18px 8px' }}>
            <Tag color="orange" style={{ marginInlineEnd: 0 }}>
              开发模式
            </Tag>
          </div>
        )}
        <nav aria-label="主导航">
          <Menu
            theme="dark"
            mode="inline"
            selectedKeys={[selectedKey]}
            items={menuItems}
            onClick={onMenuClick}
          />
        </nav>
        <div className="kd-sider-foot">
          {cluster ? (
            <>
              <div style={{ color: '#cbd5e1' }}>{cluster.name}</div>
              <div>
                {cluster.version || 'unknown'} · {cluster.status}
              </div>
            </>
          ) : (
            '尚未选择集群'
          )}
        </div>
      </Sider>

      <Layout>
        <Header className="kd-topbar">
          <Dropdown menu={{ items: clusterMenu }} trigger={['click']}>
            <div className="kd-cluster-switch">
              <span className={`kd-health-dot ${cluster?.status ?? 'unknown'}`} />
              <span style={{ fontWeight: 550 }}>{cluster?.name ?? '选择集群'}</span>
              <span className="kd-muted" style={{ fontSize: 11, marginLeft: 'auto' }}>
                {cluster?.version || '—'}
              </span>
            </div>
          </Dropdown>

          <button className="kd-search-trigger" onClick={() => setCommandOpen(true)}>
            <SearchOutlined />
            <span style={{ fontSize: 12.5 }}>搜索资源或执行动作</span>
            <span className="kd-kbd">⌘K</span>
          </button>

          <div style={{ flex: 1 }} />

          {me.user.isPlatformAdmin && (
            <Button icon={<PlusOutlined />} onClick={() => navigate('/clusters/new')}>
              接入集群
            </Button>
          )}

          <Dropdown
            menu={{
              items: [
                {
                  key: 'identity',
                  disabled: true,
                  label: (
                    <div style={{ padding: '4px 0' }}>
                      <div style={{ fontWeight: 600 }}>
                        {me.user.displayName || me.user.email || me.user.subject}
                      </div>
                      <div className="kd-muted" style={{ fontSize: 11 }}>
                        {me.user.email}
                      </div>
                    </div>
                  ),
                },
                { type: 'divider' },
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

        <Content className="kd-content">
          <div className="kd-page">
            {cluster?.status === 'unreachable' && (
              <Alert
                type="error"
                showIcon
                style={{ marginBottom: 14 }}
                message="当前集群不可达"
                description={cluster.message || '平台会在后台持续探测，恢复后自动刷新。'}
              />
            )}
            {cluster?.status === 'degraded' && (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 14 }}
                message="当前集群状态降级"
                description={cluster.message || '部分操作可能失败。'}
              />
            )}
            {clustersQuery.isError && (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 14 }}
                message="集群列表刷新失败，正在显示上次结果"
                description={
                  clustersQuery.error instanceof Error ? clustersQuery.error.message : undefined
                }
              />
            )}

            {showNamespaceBar && (
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  marginBottom: 14,
                  padding: '8px 12px',
                  background: '#fff',
                  border: '1px solid var(--kd-border)',
                  borderRadius: 10,
                }}
              >
                <span className="kd-muted" style={{ fontSize: 12 }}>
                  命名空间
                </span>
                <Select
                  aria-label="命名空间"
                  style={{ minWidth: 220 }}
                  value={namespace || (clusterWide ? '' : undefined)}
                  placeholder="选择命名空间"
                  onChange={selectNamespace}
                  options={[
                    ...(clusterWide ? [{ value: '', label: '全部命名空间' }] : []),
                    ...availableNamespaces.map((item) => ({ value: item, label: item })),
                  ]}
                />
                {namespacesQuery.isError && (
                  <span style={{ color: '#b91c1c', fontSize: 12 }}>
                    {namespacesQuery.error instanceof ApiError &&
                    namespacesQuery.error.code === 'FORBIDDEN'
                      ? '无权查看命名空间列表'
                      : '命名空间列表加载失败'}
                  </span>
                )}
                <span className="kd-muted" style={{ fontSize: 11, marginLeft: 'auto' }}>
                  切换命名空间会影响当前列表与操作范围
                </span>
              </div>
            )}

            {!cluster && !isAdminPage ? (
              clustersQuery.isLoading ? (
                <Spin />
              ) : clusters.length === 0 ? (
                <div className="kd-card kd-card-pad" style={{ textAlign: 'center', padding: 48 }}>
                  <ClusterOutlined style={{ fontSize: 32, color: '#94a3b8' }} />
                  <h2 style={{ margin: '12px 0 6px' }}>
                    {me.user.isPlatformAdmin ? '接入第一个集群' : '尚未接入集群'}
                  </h2>
                  <p className="kd-muted" style={{ marginTop: 0 }}>
                    {me.user.isPlatformAdmin
                      ? '填写 kubeconfig 或 ServiceAccount Token，平台会先做连通性与版本校验。'
                      : '请联系平台管理员接入集群后再访问。'}
                  </p>
                  {me.user.isPlatformAdmin && (
                    <Button
                      type="primary"
                      icon={<PlusOutlined />}
                      onClick={() => navigate('/clusters/new')}
                    >
                      开始接入
                    </Button>
                  )}
                </div>
              ) : (
                <ErrorState
                  error={new ApiError('NOT_FOUND', `集群不存在：${params.cluster ?? ''}`)}
                  title="无法打开集群"
                />
              )
            ) : (
              <Outlet />
            )}
          </div>
        </Content>
      </Layout>

      <CommandPalette
        open={commandOpen}
        cluster={cluster?.name}
        namespace={namespace}
        onClose={() => setCommandOpen(false)}
      />
    </Layout>
  )
}