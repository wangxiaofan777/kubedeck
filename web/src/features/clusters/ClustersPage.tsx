import { Button, Card, Col, Empty, Result, Row, Space, Tag } from 'antd'
import { ApartmentOutlined, ArrowRightOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useClusters } from './useClusters'
import { useAuth } from '../../app/AuthProvider'
import PageHeader from '../../components/PageHeader'
import StatusPill from '../../components/StatusPill'
import { ErrorState, PageSkeleton } from '../../components/StateViews'

export default function ClustersPage() {
  const { me } = useAuth()
  const navigate = useNavigate()
  const query = useClusters()

  if (!me?.user.isPlatformAdmin) {
    return <Result status="403" title="需要平台管理员权限" subTitle="只有平台管理员可以管理集群接入。" />
  }
  if (query.isLoading) return <PageSkeleton rows={6} />
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />

  const clusters = query.data ?? []

  return (
    <div>
      <PageHeader
        crumbs={[{ label: '集群管理' }]}
        title="集群管理"
        subtitle={<span className="kd-muted">接入、查看健康状态与轮换凭据</span>}
        actions={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()}>
              刷新
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/clusters/new')}>
              接入集群
            </Button>
          </Space>
        }
      />

      {clusters.length === 0 ? (
        <Card>
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <div>
                <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 4 }}>还没有接入集群</div>
                <div className="kd-muted">接入后即可浏览资源、查看日志与执行工作负载操作。</div>
              </div>
            }
          >
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/clusters/new')}>
              接入第一个集群
            </Button>
          </Empty>
        </Card>
      ) : (
        <Row gutter={[16, 16]}>
          {clusters.map((cluster) => (
            <Col key={cluster.name} xs={24} lg={12} xxl={8}>
              <Card
                hoverable
                onClick={() =>
                  navigate(`/clusters/${encodeURIComponent(cluster.name)}/overview`)
                }
                title={
                  <Space>
                    <ApartmentOutlined style={{ color: '#64748b' }} />
                    <span>{cluster.name}</span>
                  </Space>
                }
                extra={<StatusPill value={cluster.status} />}
                actions={[
                  <span
                    key="open"
                    onClick={(event) => {
                      event.stopPropagation()
                      navigate(`/clusters/${encodeURIComponent(cluster.name)}/overview`)
                    }}
                  >
                    打开 <ArrowRightOutlined />
                  </span>,
                  <span
                    key="settings"
                    onClick={(event) => {
                      event.stopPropagation()
                      navigate(`/clusters/${encodeURIComponent(cluster.name)}/settings`)
                    }}
                  >
                    设置
                  </span>,
                ]}
              >
                <div className="kd-kv">
                  <div>
                    <span>版本</span>
                    <span className="kd-mono">{cluster.version || 'unknown'}</span>
                  </div>
                  <div>
                    <span>Server</span>
                    <span className="kd-mono" style={{ maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {cluster.server}
                    </span>
                  </div>
                  {cluster.message && (
                    <div>
                      <span>状态消息</span>
                      <span style={{ color: '#b45309', textAlign: 'right' }}>{cluster.message}</span>
                    </div>
                  )}
                </div>
              </Card>
            </Col>
          ))}
          <Col xs={24} lg={12} xxl={8}>
            <Card
              style={{ borderStyle: 'dashed', background: 'transparent', minHeight: 160 }}
              hoverable
              onClick={() => navigate('/clusters/new')}
            >
              <div style={{ textAlign: 'center', padding: '22px 0' }}>
                <PlusOutlined style={{ fontSize: 22, color: '#94a3b8' }} />
                <div style={{ marginTop: 8, fontWeight: 550 }}>接入新集群</div>
                <div className="kd-muted" style={{ fontSize: 12, marginTop: 2 }}>
                  kubeconfig 或 ServiceAccount Token
                </div>
              </div>
            </Card>
          </Col>
        </Row>
      )}

      <div className="kd-muted" style={{ fontSize: 12, marginTop: 16 }}>
        <Tag color="blue">提示</Tag>
        移除集群只会删除平台登记，不会修改目标集群中的任何资源。
      </div>
    </div>
  )
}