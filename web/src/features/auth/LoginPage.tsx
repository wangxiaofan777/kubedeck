import { Button, Divider, Space, Typography } from 'antd'
import {
  ApiOutlined,
  ClusterOutlined,
  LoginOutlined,
  SafetyCertificateOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useLocation } from 'react-router-dom'
import { api } from '../../lib/api'

const FEATURES = [
  { icon: <ClusterOutlined />, title: '多集群统一驾驶舱', text: '一个入口浏览工作负载、网络与配置。' },
  { icon: <ThunderboltOutlined />, title: '实时状态与日志', text: '事件、指标与日志流保持同步。' },
  { icon: <SafetyCertificateOutlined />, title: '以你的身份访问', text: '授权由 Kubernetes RBAC 判定，不做第二套权限表。' },
]

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
        background: 'radial-gradient(circle at 20% 20%, #e8eefc 0, #f5f7fa 45%, #eef2f8 100%)',
        padding: 24,
      }}
    >
      <div
        className="kd-card"
        style={{ width: 'min(920px, 100%)', display: 'flex', overflow: 'hidden', minHeight: 480 }}
      >
        <div
          style={{
            flex: '1 1 52%',
            background: 'linear-gradient(150deg, #0b1220 0%, #15213a 55%, #1d3357 100%)',
            color: '#e6edf7',
            padding: '48px 44px',
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'space-between',
          }}
        >
          <div>
            <Space align="center" size={10}>
              <span
                style={{
                  display: 'grid',
                  placeItems: 'center',
                  width: 38,
                  height: 38,
                  borderRadius: 10,
                  background: 'rgba(255,255,255,.12)',
                  fontSize: 20,
                }}
              >
                <ClusterOutlined />
              </span>
              <div>
                <div style={{ fontSize: 20, fontWeight: 650, letterSpacing: '.2px' }}>KubeDeck</div>
                <div style={{ fontSize: 12, color: '#93a4bf' }}>集群驾驶舱</div>
              </div>
            </Space>
            <h1 style={{ fontSize: 28, lineHeight: 1.3, margin: '36px 0 10px', color: '#fff' }}>
              把集群状态与日常操作
              <br />
              放进同一个工作面
            </h1>
            <p style={{ color: '#a9b7cd', margin: 0, fontSize: 13.5 }}>
              面向平台团队的多集群 Kubernetes 控制台。
            </p>
          </div>
          <Space direction="vertical" size={16} style={{ marginTop: 36 }}>
            {FEATURES.map((item) => (
              <div key={item.title} style={{ display: 'flex', gap: 12 }}>
                <span style={{ color: '#8ab4ff', fontSize: 16, marginTop: 2 }}>{item.icon}</span>
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, color: '#e6edf7' }}>{item.title}</div>
                  <div style={{ fontSize: 12, color: '#8fa1bb' }}>{item.text}</div>
                </div>
              </div>
            ))}
          </Space>
        </div>

        <div style={{ flex: '1 1 48%', padding: '56px 46px', display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
          <Typography.Title level={3} style={{ marginBottom: 6 }}>
            登录控制台
          </Typography.Title>
          <Typography.Paragraph type="secondary" style={{ marginBottom: 26 }}>
            使用企业统一身份认证。平台不保存你的令牌，所有集群请求都以你的身份发起。
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
          <Divider style={{ margin: '26px 0 18px' }} />
          <Space align="start" size={10}>
            <SafetyCertificateOutlined style={{ color: '#2563eb', marginTop: 3 }} />
            <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>
              无法登录时请联系平台管理员确认账号状态与集群绑定。
            </Typography.Text>
          </Space>
          <div className="kd-muted" style={{ marginTop: 'auto', paddingTop: 28, fontSize: 11.5 }}>
            <ApiOutlined /> KubeDeck · {new Date().getFullYear()}
          </div>
        </div>
      </div>
    </div>
  )
}