import { useState } from 'react'
import { Alert, App, Button, Card, Input, Result, Space, Steps, Tag } from 'antd'
import { CheckCircleOutlined, CloudServerOutlined, LeftOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, type ProbeResult } from '../../lib/api'
import { useAuth } from '../../app/AuthProvider'
import PageHeader from '../../components/PageHeader'
import CredentialInput, {
  credentialInput,
  emptyCredential,
  type CredentialDraft,
} from './CredentialInput'

const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/

/** ClusterOnboardingPage 是三步接入向导：连接信息 → 校验 → 确认。 */
export default function ClusterOnboardingPage() {
  const { me } = useAuth()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [step, setStep] = useState(0)
  const [name, setName] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [credential, setCredential] = useState<CredentialDraft>(emptyCredential)
  const [probe, setProbe] = useState<ProbeResult>()
  const [busy, setBusy] = useState<'probe' | 'create'>()
  const [error, setError] = useState<unknown>()

  if (!me?.user.isPlatformAdmin) {
    return <Result status="403" title="需要平台管理员权限" subTitle="只有平台管理员可以接入集群。" />
  }

  const validateStep0 = (): string | undefined => {
    if (!NAME_RE.test(name)) return '集群名称必须是小写字母、数字或连字符'
    if (credential.mode === 'kubeconfig' && !credential.kubeconfig.trim()) return '请粘贴 kubeconfig'
    if (credential.mode === 'token' && (!credential.server.trim() || !credential.token.trim()))
      return '请填写 server 与 token'
    return undefined
  }

  const runProbe = async () => {
    const invalid = validateStep0()
    if (invalid) {
      message.error(invalid)
      return
    }
    setError(undefined)
    setBusy('probe')
    try {
      const result = await api.probeCluster(credentialInput(credential))
      setProbe(result)
      setStep(1)
      if (result.status !== 'healthy') {
        message.warning('探测未成功，请根据提示修正凭据')
      }
    } catch (err) {
      setError(err)
    } finally {
      setBusy(undefined)
    }
  }

  const create = async () => {
    setError(undefined)
    setBusy('create')
    try {
      await api.createCluster({
        name,
        displayName: displayName || name,
        ...credentialInput(credential),
      })
      message.success(`已接入集群 ${name}`)
      navigate(`/clusters/${encodeURIComponent(name)}/settings`)
    } catch (err) {
      setError(err)
      if (err instanceof ApiError && err.code === 'CONFLICT') {
        message.error('集群名称已存在，请换一个名称或使用轮换凭据')
      }
    } finally {
      setBusy(undefined)
    }
  }

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群管理', to: '/clusters' },
          { label: '接入集群' },
        ]}
        title="接入集群"
        subtitle={<span className="kd-muted">提交前会先验证连通性、版本与凭据，不会写入数据库</span>}
        actions={
          <Button icon={<LeftOutlined />} onClick={() => navigate('/clusters')}>
            返回列表
          </Button>
        }
      />

      <Card style={{ marginBottom: 16 }}>
        <Steps
          current={step}
          items={[
            { title: '连接信息', description: '凭据与集群标识' },
            { title: '校验', description: '连通性与版本' },
            { title: '确认', description: '创建集群' },
          ]}
        />
      </Card>

      {error ? (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 16 }}
          message="操作失败"
          description={
            error instanceof Error
              ? `${error.message}${error instanceof ApiError && error.requestId ? `（${error.requestId}）` : ''}`
              : undefined
          }
        />
      ) : null}

      {step === 0 && (
        <Card title="连接信息">
          <div style={{ maxWidth: 720 }}>
            <Space direction="vertical" size={14} style={{ width: '100%' }}>
              <div>
                <div style={{ fontSize: 12, color: '#475569', marginBottom: 5 }}>集群名称</div>
                <Input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="prod-shanghai"
                  aria-label="集群名称"
                />
                <div className="kd-muted" style={{ fontSize: 11, marginTop: 4 }}>
                  创建后不可修改，用于 URL 与审计记录
                </div>
              </div>
              <div>
                <div style={{ fontSize: 12, color: '#475569', marginBottom: 5 }}>展示名（可选）</div>
                <Input
                  value={displayName}
                  onChange={(event) => setDisplayName(event.target.value)}
                  placeholder="生产环境 · 上海"
                  aria-label="展示名"
                />
              </div>
              <CredentialInput value={credential} onChange={setCredential} />
            </Space>
          </div>
          <div style={{ marginTop: 18 }}>
            <Button type="primary" loading={busy === 'probe'} onClick={() => void runProbe()}>
              校验连接
            </Button>
          </div>
        </Card>
      )}

      {step === 1 && (
        <Card title="校验结果">
          {probe ? (
            <Space direction="vertical" size={14} style={{ width: '100%', maxWidth: 720 }}>
              <Alert
                type={probe.status === 'healthy' ? 'success' : 'warning'}
                showIcon
                icon={probe.status === 'healthy' ? <CheckCircleOutlined /> : <CloudServerOutlined />}
                message={
                  probe.status === 'healthy'
                    ? '连接成功'
                    : probe.status === 'degraded'
                      ? '已连接，但凭据或证书有问题'
                      : '无法连接目标集群'
                }
                description={probe.message || undefined}
              />
              <div className="kd-kv">
                <div>
                  <span>Server</span>
                  <span className="kd-mono">{probe.server}</span>
                </div>
                <div>
                  <span>版本</span>
                  <span className="kd-mono">{probe.version || 'unknown'}</span>
                </div>
                <div>
                  <span>耗时</span>
                  <span>{probe.latencyMs} ms</span>
                </div>
                <div>
                  <span>状态</span>
                  <Tag color={probe.status === 'healthy' ? 'green' : 'orange'}>{probe.status}</Tag>
                </div>
              </div>
              <Space>
                <Button onClick={() => setStep(0)}>返回修改</Button>
                <Button type="primary" onClick={() => setStep(2)} disabled={probe.status === 'unreachable'}>
                  继续
                </Button>
              </Space>
            </Space>
          ) : (
            <Button type="primary" loading={busy === 'probe'} onClick={() => void runProbe()}>
              重新校验
            </Button>
          )}
        </Card>
      )}

      {step === 2 && (
        <Card title="确认接入">
          <Space direction="vertical" size={16} style={{ width: '100%', maxWidth: 720 }}>
            <div className="kd-kv">
              <div>
                <span>集群名称</span>
                <span className="kd-mono">{name}</span>
              </div>
              <div>
                <span>展示名</span>
                <span>{displayName || name}</span>
              </div>
              <div>
                <span>版本</span>
                <span className="kd-mono">{probe?.version || 'unknown'}</span>
              </div>
              <div>
                <span>凭据类型</span>
                <span>{credential.mode === 'kubeconfig' ? 'kubeconfig' : 'token'}</span>
              </div>
            </div>
            <Alert
              type="info"
              showIcon
              message="平台会以最终用户身份访问该集群"
              description="平台 ServiceAccount 只具备 impersonate 权限；能否读取或修改资源由目标集群 RBAC 决定。"
            />
            <Space>
              <Button onClick={() => setStep(1)}>上一步</Button>
              <Button type="primary" loading={busy === 'create'} onClick={() => void create()}>
                创建集群
              </Button>
            </Space>
          </Space>
        </Card>
      )}
    </div>
  )
}