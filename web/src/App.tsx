import { Navigate, Outlet, Route, Routes, Link } from 'react-router-dom'
import { Button, Spin } from 'antd'
import { useAuth } from './app/AuthProvider'
import ConsoleShell from './app/ConsoleShell'
import { useClusters } from './features/clusters/useClusters'
import { ApiError } from './lib/api'
import LoginPage from './features/auth/LoginPage'
import ResourcesPage from './features/resources/ResourcesPage'
import ResourceDetailPage from './features/resources/ResourceDetailPage'
import CreateResourcePage from './features/resources/CreateResourcePage'
import AuditPage from './features/audit/AuditPage'
import OverviewPage from './features/overview/OverviewPage'
import NodesPage from './features/nodes/NodesPage'
import NodeDetailPage from './features/nodes/NodeDetailPage'
import ClustersPage from './features/clusters/ClustersPage'
import ClusterOnboardingPage from './features/clusters/ClusterOnboardingPage'
import ClusterSettingsPage from './features/clusters/ClusterSettingsPage'
import ErrorBoundary from './components/ErrorBoundary'
import { ErrorState } from './components/StateViews'

function RequireAuth() {
  const { loading, error } = useAuth()
  if (loading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" />
      </div>
    )
  }
  if (error instanceof ApiError && error.code === 'UNAUTHENTICATED') {
    return <LoginPage />
  }
  if (error) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <ErrorState error={error} title="无法获取登录状态" />
      </div>
    )
  }
  return <Outlet />
}

function HomeRedirect() {
  const { me } = useAuth()
  const clusters = useClusters()
  if (clusters.isLoading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: 240 }}>
        <Spin />
      </div>
    )
  }
  if (clusters.isError && !clusters.data) {
    return <ErrorState error={clusters.error} onRetry={() => void clusters.refetch()} />
  }
  const first = clusters.data?.[0]
  if (first) {
    return <Navigate to={`/clusters/${encodeURIComponent(first.name)}/overview`} replace />
  }
  return <Navigate to={me?.user.isPlatformAdmin ? '/clusters' : '/no-cluster'} replace />
}

function NoCluster() {
  const { me } = useAuth()
  const isAdmin = !!me?.user.isPlatformAdmin
  return (
    <div className="kd-card kd-card-pad" style={{ maxWidth: 560, margin: '80px auto', textAlign: 'center' }}>
      <h2 style={{ marginBottom: 6 }}>尚未接入集群</h2>
      <p className="kd-muted" style={{ marginTop: 0 }}>
        {isAdmin
          ? '接入一个集群后即可浏览资源、查看日志并执行受控变更。'
          : '请联系平台管理员接入集群并为你分配访问权限；接入后这里会自动出现可用集群。'}
      </p>
      {isAdmin && (
        <Link to="/clusters/new">
          <Button type="primary">接入第一个集群</Button>
        </Link>
      )}
    </div>
  )
}

export default function App() {
  return (
    <ErrorBoundary>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<RequireAuth />}>
          <Route element={<ConsoleShell />}>
            <Route index element={<HomeRedirect />} />
            <Route path="/clusters" element={<ClustersPage />} />
            <Route path="/clusters/new" element={<ClusterOnboardingPage />} />
            <Route path="/clusters/:cluster/overview" element={<OverviewPage />} />
            <Route path="/clusters/:cluster/nodes" element={<NodesPage />} />
            <Route path="/clusters/:cluster/nodes/:name" element={<NodeDetailPage />} />
            <Route path="/clusters/:cluster/new/:typeKey" element={<CreateResourcePage />} />
            <Route path="/clusters/:cluster/settings" element={<ClusterSettingsPage />} />
            <Route path="/clusters/:cluster/audit" element={<AuditPage />} />
            <Route path="/clusters/:cluster/:typeKey" element={<ResourcesPage />} />
            <Route
              path="/clusters/:cluster/:typeKey/:namespace/:name"
              element={<ResourceDetailPage />}
            />
            <Route path="/no-cluster" element={<NoCluster />} />
          </Route>
        </Route>
        <Route
          path="*"
          element={
            <div style={{ padding: 48, textAlign: 'center' }}>
              <h2>页面不存在</h2>
              <Link to="/">
                <Button type="primary">返回控制台</Button>
              </Link>
            </div>
          }
        />
      </Routes>
    </ErrorBoundary>
  )
}