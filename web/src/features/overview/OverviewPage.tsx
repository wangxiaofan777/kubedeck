import { Alert, Button, Card, Col, Empty, Row, Space, Table, Tag } from 'antd'
import { ArrowRightOutlined, ReloadOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import type { ColumnsType } from 'antd/es/table'
import { ApiError, type ClusterUsage, type OverviewEvent } from '../../lib/api'
import PageHeader from '../../components/PageHeader'
import { AreaSpark, DistributionBar, MiniBar } from '../../components/Charts'
import StatusPill from '../../components/StatusPill'
import { ErrorState, PageSkeleton } from '../../components/StateViews'
import { formatCPU, formatDateTime, formatMemory } from '../../lib/format'
import { useOverview, useUsageTrend, type UsageSample } from './useOverview'

const UNAVAILABLE_LABELS: Record<string, string> = {
  nodes: '节点',
  namespaces: '命名空间',
  pods: 'Pod',
  deployments: 'Deployment',
  statefulsets: 'StatefulSet',
  daemonsets: 'DaemonSet',
  events: '事件',
  metrics: '资源用量',
}

function StatCard({
  label,
  value,
  suffix,
  hint,
  tone,
  onClick,
}: {
  label: string
  value: string | number
  suffix?: string
  hint?: string
  tone?: 'success' | 'warning' | 'danger'
  onClick?: () => void
}) {
  const color =
    tone === 'danger'
      ? '#b91c1c'
      : tone === 'warning'
        ? '#b45309'
        : tone === 'success'
          ? '#15803d'
          : '#0f172a'
  return (
    <Card
      hoverable={!!onClick}
      onClick={onClick}
      styles={{ body: { padding: 18 } }}
      style={{ cursor: onClick ? 'pointer' : undefined }}
    >
      <div
        style={{
          fontSize: 11,
          letterSpacing: '0.05em',
          textTransform: 'uppercase',
          color: '#94a3b8',
        }}
      >
        {label}
      </div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, marginTop: 6 }}>
        <span style={{ fontSize: 26, fontWeight: 650, color, lineHeight: 1.1 }}>{value}</span>
        {suffix && <span style={{ fontSize: 12, color: '#64748b' }}>{suffix}</span>}
      </div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginTop: 6,
        }}
      >
        <span style={{ fontSize: 12, color: '#64748b' }}>{hint}</span>
        {onClick && <ArrowRightOutlined style={{ fontSize: 11, color: '#94a3b8' }} />}
      </div>
    </Card>
  )
}

function UsagePanel({ usage, samples }: { usage?: ClusterUsage | null; samples: UsageSample[] }) {
  if (!usage) {
    return (
      <Card title="资源用量" size="small" extra={<Tag>不可用</Tag>}>
        <Alert
          type="info"
          showIcon
          message="指标不可用"
          description="集群未启用 metrics-server，或当前账号没有指标读取权限。其余统计不受影响。"
        />
      </Card>
    )
  }
  const scoped = usage.scope === 'pods'
  return (
    <Card
      title="资源用量"
      size="small"
      extra={<Tag color={scoped ? 'blue' : 'green'}>{scoped ? '仅可见命名空间' : '节点口径'}</Tag>}
    >
      <Row gutter={[20, 12]}>
        <Col span={12}>
          <div className="kd-chart-label">CPU 用量</div>
          <div style={{ fontSize: 22, fontWeight: 650, margin: '2px 0 4px' }}>
            {formatCPU(usage.cpuMilli)}
          </div>
          <div className="kd-chart-label">
            {usage.cpuPercent != null ? `${usage.cpuPercent.toFixed(1)}% 已分配` : '可分配基线未知'}
          </div>
          <AreaSpark
            values={samples.map((sample) => sample.cpuMilli)}
            color="#2563eb"
            label="CPU 趋势"
          />
        </Col>
        <Col span={12}>
          <div className="kd-chart-label">内存用量</div>
          <div style={{ fontSize: 22, fontWeight: 650, margin: '2px 0 4px' }}>
            {formatMemory(usage.memoryBytes)}
          </div>
          <div className="kd-chart-label">
            {usage.memoryPercent != null
              ? `${usage.memoryPercent.toFixed(1)}% 已分配`
              : '可分配基线未知'}
          </div>
          <AreaSpark
            values={samples.map((sample) => sample.memoryBytes)}
            color="#16a34a"
            label="内存趋势"
          />
        </Col>
      </Row>
      <div className="kd-chart-label" style={{ marginTop: 8 }}>
        会话内采样 {samples.length} 个点，刷新页面后重新开始
      </div>
    </Card>
  )
}

export default function OverviewPage() {
  const { cluster } = useParams()
  const navigate = useNavigate()
  const overview = useOverview(cluster)
  const samples = useUsageTrend(cluster, overview.data?.usage)

  if (overview.isLoading) return <PageSkeleton rows={10} />
  if (overview.isError && !overview.data) {
    const unreachable =
      overview.error instanceof ApiError && overview.error.code === 'CLUSTER_UNREACHABLE'
    return (
      <ErrorState
        error={overview.error}
        title={unreachable ? '当前集群不可达' : '总览加载失败'}
        onRetry={() => void overview.refetch()}
      />
    )
  }
  const data = overview.data
  if (!data) return null

  const base = `/clusters/${encodeURIComponent(cluster ?? '')}`
  const podTotal = data.pods.total
  const podHealthy = data.pods.running + data.pods.succeeded
  const workloadUnavailable =
    data.workloads.deployments.unavailable +
    data.workloads.statefulSets.unavailable +
    data.workloads.daemonSets.unavailable
  const warningEvents = data.events.filter((event) => event.type === 'Warning').slice(0, 6)

  const eventColumns: ColumnsType<OverviewEvent> = [
    {
      title: '时间',
      dataIndex: 'lastTimestamp',
      width: 150,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 80,
      render: (value: string) => <StatusPill value={value} />,
    },
    { title: '原因', dataIndex: 'reason', width: 150 },
    { title: '对象', dataIndex: 'object', width: 220, ellipsis: true },
    { title: '消息', dataIndex: 'message', ellipsis: true },
  ]

  return (
    <div>
      <PageHeader
        crumbs={[{ label: cluster ?? '', to: `${base}/overview` }, { label: '总览' }]}
        title="集群总览"
        subtitle={
          <span className="kd-muted">
            最近更新 {formatDateTime(data.fetchedAt)}
            {!data.scope.allNamespaces && ` · 统计范围：${data.scope.namespaces} 个可见命名空间`}
          </span>
        }
        tags={
          <>
            {data.scope.truncated && <Tag color="orange">部分统计已截断</Tag>}
            {data.unavailable.length > 0 && (
              <Tag color="warning">
                不可用：{data.unavailable.map((key) => UNAVAILABLE_LABELS[key] ?? key).join('、')}
              </Tag>
            )}
          </>
        }
        actions={
          <Button
            icon={<ReloadOutlined />}
            loading={overview.isFetching && !overview.isLoading}
            onClick={() => void overview.refetch()}
          >
            刷新
          </Button>
        }
      />

      {overview.isError && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
          message="刷新失败，正在显示上一次结果"
          description={overview.error instanceof Error ? overview.error.message : undefined}
        />
      )}

      <Row gutter={[14, 14]} style={{ marginBottom: 14 }}>
        <Col xs={24} sm={12} xl={6}>
          <StatCard
            label="Pod 运行"
            value={`${data.pods.running}/${podTotal}`}
            hint={`Pending ${data.pods.pending} · 异常 ${data.pods.crashLoop}`}
            tone={data.pods.crashLoop > 0 ? 'warning' : 'success'}
            onClick={() => navigate(`${base}/pods`)}
          />
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <StatCard
            label="节点就绪"
            value={`${data.nodes.ready}/${data.nodes.total}`}
            hint={`可分配 ${formatCPU(data.nodes.allocatable.cpuMilli)} · ${formatMemory(data.nodes.allocatable.memoryBytes)}`}
            tone={data.nodes.ready < data.nodes.total ? 'warning' : 'success'}
            onClick={() => navigate(`${base}/nodes`)}
          />
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <StatCard
            label="异常工作负载"
            value={workloadUnavailable}
            suffix={`/ ${data.workloads.deployments.total + data.workloads.statefulSets.total + data.workloads.daemonSets.total}`}
            hint="Deployment / StatefulSet / DaemonSet"
            tone={workloadUnavailable > 0 ? 'danger' : 'success'}
            onClick={() => navigate(`${base}/deployments`)}
          />
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <StatCard
            label="命名空间"
            value={data.namespaces.total}
            hint={`Pod 健康率 ${podTotal > 0 ? Math.round((podHealthy / podTotal) * 100) : 0}%`}
            onClick={() => navigate(`${base}/namespaces`)}
          />
        </Col>
      </Row>

      <Row gutter={[14, 14]} style={{ marginBottom: 14 }}>
        <Col xs={24} xl={14}>
          <UsagePanel usage={data.usage} samples={samples} />
        </Col>
        <Col xs={24} xl={10}>
          <Card title="Pod 状态分布" size="small" style={{ height: '100%' }}>
            {podTotal === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="可见范围内没有 Pod" />
            ) : (
              <DistributionBar
                segments={[
                  { key: 'running', label: 'Running', value: data.pods.running, color: '#16a34a' },
                  { key: 'pending', label: 'Pending', value: data.pods.pending, color: '#f59e0b' },
                  { key: 'failed', label: 'Failed', value: data.pods.failed, color: '#dc2626' },
                  { key: 'crash', label: 'CrashLoop', value: data.pods.crashLoop, color: '#b91c1c' },
                  {
                    key: 'succeeded',
                    label: 'Succeeded',
                    value: data.pods.succeeded,
                    color: '#94a3b8',
                  },
                  { key: 'unknown', label: 'Unknown', value: data.pods.unknown, color: '#cbd5e1' },
                ]}
              />
            )}
            <div style={{ marginTop: 18 }}>
              <div className="kd-section-title">工作负载健康</div>
              <Space direction="vertical" size={10} style={{ width: '100%' }}>
                {[
                  { key: 'deployments', label: 'Deployment', value: data.workloads.deployments },
                  { key: 'statefulSets', label: 'StatefulSet', value: data.workloads.statefulSets },
                  { key: 'daemonSets', label: 'DaemonSet', value: data.workloads.daemonSets },
                ].map((item) => (
                  <div
                    key={item.key}
                    style={{ cursor: 'pointer' }}
                    onClick={() =>
                      navigate(
                        `${base}/${
                          item.key === 'statefulSets'
                            ? 'statefulsets'
                            : item.key === 'daemonSets'
                              ? 'daemonsets'
                              : 'deployments'
                        }`,
                      )
                    }
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12 }}>
                      <span>{item.label}</span>
                      <span className="kd-muted">
                        {item.value.total - item.value.unavailable}/{item.value.total}
                      </span>
                    </div>
                    <MiniBar
                      value={item.value.total - item.value.unavailable}
                      total={item.value.total}
                      danger={item.value.unavailable}
                    />
                  </div>
                ))}
              </Space>
            </div>
          </Card>
        </Col>
      </Row>

      <Card
        title="最近告警事件"
        size="small"
        extra={
          <span className="kd-muted" style={{ fontSize: 11 }}>
            最多 6 条 Warning
          </span>
        }
      >
        {warningEvents.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有 Warning 事件" />
        ) : (
          <Table
            size="small"
            rowKey={(record) => `${record.namespace ?? ''}/${record.object}/${record.lastTimestamp}`}
            dataSource={warningEvents}
            columns={eventColumns}
            pagination={false}
          />
        )}
      </Card>
    </div>
  )
}