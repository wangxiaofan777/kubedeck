import { Button, Card, Space, Typography } from 'antd'
import { ClusterOutlined, LoginOutlined } from '@ant-design/icons'
import { useLocation } from 'react-router-dom'
import { api } from '../../lib/api'

/** LoginPage 只负责跳转 IdP；开发模式不会走到这里。 */
export default function LoginPage() {
  const location = useLocation()
  const returnTo = `${location.pathname}${location.search}`
  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        background: '#f5f7fa',
        padding: 24,
      }}
    >
      <Card style={{ width: 420 }}>
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Space align="center">
            <ClusterOutlined style={{ fontSize: 28, color: '#1677ff' }} />
            <Typography.Title level={3} style={{ margin: 0 }}>
              KubeDeck
            </Typography.Title>
          </Space>
          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            使用企业身份登录后，平台会以你的身份访问集群，权限由 Kubernetes RBAC 决定。
          </Typography.Paragraph>
          <Button
            type="primary"
            size="large"
            block
            icon={<LoginOutlined />}
            href={api.loginURL(returnTo === '/login' ? '/' : returnTo)}
          >
            使用 OIDC 登录
          </Button>
        </Space>
      </Card>
    </div>
  )
}
