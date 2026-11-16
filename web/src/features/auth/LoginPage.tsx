import { useState } from 'react'
import { Alert, Button, Divider, Spin, Space, Tag, Typography } from 'antd'
import {
  ApiOutlined,
  ClusterOutlined,
  LoginOutlined,
  SafetyCertificateOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'

const FEATURES = [
  { icon: <ClusterOutlined />, title: '多集群统一驾驶舱', text: '一个入口浏览工作负载、网络与配置。' },
  { icon: <ThunderboltOutlined />, title: '实时状态与日志', text: '事件、指标与日志流保持同步。' },
  {
    icon: <SafetyCertificateOutlined />,
    title: '以你的身份访问',
    text: '平台按绑定收敛范围，最终权限由 Kubernetes RBAC 判定。',
  },
]

/** safeReturnTo 只接受站内相对路径，避免登录后被带到外部站点。 */
export function safeReturnTo(raw: string | null): string {
  if (!raw || !raw.startsWith('/') || raw.startsWith('//') || raw.includes('\\')) return '/'
  return raw
}

/** LoginPage 按服务端登录模式渲染真实入口：OIDC、开发登录或未配置说明。 */
export default function LoginPage() {
  const [params] = useSearchParams()
  const queryClient = useQueryClient()
  const returnTo = safeReturnTo(params.get('returnTo'))
  const [busy, setBusy] = useState(false)
  const [loginError, setLoginError] = useState<unknown>()

  const modeQuery = useQuery({
    queryKey: ['auth-mode'],
    queryFn: ({ signal }) => api.authMode(signal),
    retry: 1,
    staleTime: 60_000,
  })

  const devLogin = async () => {
    setBusy(true)
    setLoginError(undefined)
    try {
      await api.devLogin()
      queryClient.clear()
      window.location.assign(returnTo)
    } catch (error) {
      setLoginError(error)
    } finally {
      setBusy(false)
    }
  }

  const mode = modeQuery.data?.mode
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

        <div
          style={{
            flex: '1 1 48%',
            padding: '56px 46px',
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'center',
          }}
        >
          {modeQuery.isLoading ? (
            <Space direction="vertical" align="center" style={{ width: '100%' }} size={16}>
              <Spin />
              <Typography.Text type="secondary">正在检测登录方式…</Typography.Text>
            </Space>
          ) : modeQuery.isError ? (
            <Alert
              type="error"
              showIcon
              message="无法获取登录方式"
              description="请检查服务端是否可达，或稍后重试。"
              action={
                <Button size="small" onClick={() => void modeQuery.refetch()}>
                  重试
                </Button>
              }
            />
          ) : mode === 'unconfigured' ? (
            <Alert
              type="warning"
              showIcon
              message="未配置登录方式"
              description="当前服务端既未配置 OIDC，也未启用开发模式。请联系平台管理员设置 OIDC 或 dev_kubeconfig 后重启服务。"
            />
          ) : mode === 'dev' ? (
            <>
              <Space size={8} align="center" style={{ marginBottom: 6 }}>
                <Typography.Title level={3} style={{ margin: 0 }}>
                  开发模式登录
                </Typography.Title>
                <Tag color="orange">本地身份</Tag>
              </Space>
              <Typography.Paragraph type="secondary" style={{ marginBottom: 18 }}>
                当前服务端以本地身份运行，不会经过企业 IdP。该入口仅用于本机开发与验收，不能暴露到网络。
              </Typography.Paragraph>
              {modeQuery.data?.devUser && (
                <Typography.Paragraph>
                  登录身份：<Typography.Text strong>{modeQuery.data.devUser}</Typography.Text>
                </Typography.Paragraph>
              )}
              {loginError ? (
                <Alert
                  type="error"
                  showIcon
                  style={{ marginBottom: 14 }}
                  message="开发登录失败"
                  description={loginError instanceof Error ? loginError.message : '请稍后重试'}
                />
              ) : null}
              <Button
                type="primary"
                size="large"
                block
                icon={<LoginOutlined />}
                loading={busy}
                onClick={() => void devLogin()}
              >
                以本地身份进入
              </Button>
            </>
          ) : (
            <>
              <Typography.Title level={3} style={{ marginBottom: 6 }}>
                登录控制台
              </Typography.Title>
              <Typography.Paragraph type="secondary" style={{ marginBottom: 26 }}>
                使用企业统一身份认证。平台不保存你的令牌，所有集群请求都以你的身份发起。
              </Typography.Paragraph>
              {modeQuery.data?.issuer && (
                <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
                  身份提供方：{modeQuery.data.issuer}
                </Typography.Paragraph>
              )}
              <Button
                type="primary"
                size="large"
                block
                icon={<LoginOutlined />}
                href={api.loginURL(returnTo)}
              >
                使用 OIDC 登录
              </Button>
            </>
          )}

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