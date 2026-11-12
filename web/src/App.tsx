import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { Alert, Button, Spin } from 'antd'
import { useAuth } from './app/AuthProvider'
import ConsoleShell from './app/ConsoleShell'
import { useClusters } from './features/clusters/useClusters'
import { ApiError } from './lib/api'
import LoginPage from './features/auth/LoginPage'
import ResourcesPage from './features/resources/ResourcesPage'
import ResourceDetailPage from './features/resources/ResourceDetailPage'
import AuditPage from './features/audit/AuditPage'

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
  return <Outlet />
}

function ClusterListError() {
  const clusters = useClusters()
  return (
    <Alert
      type="error"
      showIcon
      message="无法加载集群列表"
      description={
        clusters.error instanceof Error ? clusters.error.message : '请检查网络或稍后重试'
      }
      action={
        <Button size="small" onClick={() => void clusters.refetch()}>
          重试
        </Button>
      }
    />
  )
}

function HomeRedirect() {
  const { loading } = useAuth()
  const clusters = useClusters()
  if (loading || clusters.isLoading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: 240 }}>
        <Spin />
      </div>
    )
  }
  if (clusters.isError && !clusters.data) return <ClusterListError />
  const first = clusters.data?.[0]
  if (!first) return <Navigate to="/no-cluster" replace />
  return <Navigate to={`/clusters/${encodeURIComponent(first.name)}/pods`} replace />
}

function NoCluster() {
  const clusters = useClusters()
  if (clusters.isLoading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: 240 }}>
        <Spin />
      </div>
    )
  }
  if (clusters.isError && !clusters.data) return <ClusterListError />
  if (clusters.data && clusters.data.length > 0) return <Navigate to="/" replace />
  return (
    <div style={{ padding: 48, textAlign: 'center' }}>
      <h2>尚未接入集群</h2>
      <p>请联系平台管理员接入集群，或使用开发模式启动。</p>
    </div>
  )
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<RequireAuth />}>
        <Route element={<ConsoleShell />}>
          <Route index element={<HomeRedirect />} />
          <Route path="/clusters/:cluster/:typeKey" element={<ResourcesPage />} />
          <Route
            path="/clusters/:cluster/:typeKey/:namespace/:name"
            element={<ResourceDetailPage />}
          />
          <Route path="/clusters/:cluster/audit" element={<AuditPage />} />
          <Route path="/no-cluster" element={<NoCluster />} />
        </Route>
      </Route>
      <Route
        path="*"
        element={
          <div style={{ padding: 48, textAlign: 'center' }}>
            <h2>页面不存在</h2>
            <a href="/">返回控制台</a>
          </div>
        }
      />
    </Routes>
  )
}
