import { Alert, Button, Card, Col, Empty, Progress, Row, Space, Spin, Statistic, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useParams } from 'react-router-dom'
import { ApiError, type ClusterUsage, type OverviewEvent } from '../../lib/api'
import { useOverview, useUsageTrend, type UsageSample } from './useOverview'

const { Title, Text } = Typography

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

const EVENT_COLUMNS: ColumnsType<OverviewEvent> = [
  {
    title: '时间',
    dataIndex: 'lastTimestamp',
    width: 170,
    render: (value: string) => formatTime(value),
  },
  {
    title: '类型',
    dataIndex: 'type',
    width: 90,
    render: (value: string) =>
      value ? <Tag color={value === 'Warning' ? 'warning' : 'default'}>{value}</Tag> : '-',
  },
  { title: '原因', dataIndex: 'reason', width: 140 },
  { title: '对象', dataIndex: 'object', width: 220, ellipsis: true },
  { title: '消息', dataIndex: 'message', ellipsis: true },
]

function formatTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '-'
  return date.toLocaleString()
}

function formatMilliCPU(milli: number): string {
  if (milli <= 0) return '0'
  if (milli % 1000 === 0) return `${milli / 1000} 核`
  return `${(milli / 1000).toFixed(2)} 核`
}

function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0'
  const gib = bytes / 1024 ** 3
  if (gib >= 1) return `${gib.toFixed(gib >= 10 ? 0 : 1)} GiB`
  return `${(bytes / 1024 ** 2).toFixed(0)} MiB`
}

function unavailableLabel(key: string): string {
  return UNAVAILABLE_LABELS[key] ?? key
}

function formatPercent(value: number): string {
  return `${value.toFixed(2)}%`
}

/** Sparkline 用内联 SVG 绘制，避免为单条趋势引入图表库。 */
function Sparkline({ values, color }: { values: number[]; color: string }) {
  if (values.length < 2) {
    return <Text type="secondary">采样中，稍后显示趋势</Text>
  }
  const max = Math.max(...values)
  const min = Math.min(...values)
  const span = max - min || 1
  const points = values
    .map((value, index) => {
      const x = (index / (values.length - 1)) * 240
      const y = 44 - ((value - min) / span) * 40
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
  return (
    <svg viewBox="0 0 240 48" width="100%" height="48" role="img" aria-label="用量趋势">
      <polyline points={points} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" />
    </svg>
  )
}

/** UsageCard 展示当前用量与会话内趋势；指标不可用或范围受限时给出明确提示。 */
function UsageCard({ usage, samples }: { usage?: ClusterUsage | null; samples: UsageSample[] }) {
  if (!usage) {
    return (
      <Card title="资源用量" extra={<Tag>不可用</Tag>}>
        <Alert
          type="info"
          showIcon
          message="指标不可用"
          description="集群未启用指标 API（metrics-server），或当前账号没有指标读取权限。其余统计不受影响，稍后可刷新重试。"
        />
      </Card>
    )
  }

  const scoped = usage.scope === 'pods'
  const cpuValues = samples.map((sample) => sample.cpuMilli)
  const memoryValues = samples.map((sample) => sample.memoryBytes)
  const windowSeconds = (samples.length - 1) * 30
  const windowLabel =
    windowSeconds > 0 ? (windowSeconds < 60 ? `约 ${windowSeconds} 秒` : `约 ${Math.round(windowSeconds / 60)} 分钟`) : ''

  return (
    <Card
      title="资源用量"
      extra={<Tag color={scoped ? 'blue' : 'green'}>{scoped ? '仅可见命名空间' : '节点口径'}</Tag>}
    >
      <Row gutter={[16, 16]}>
        <Col xs={24} md={12}>
          <Statistic title="CPU 用量" value={formatMilliCPU(usage.cpuMilli)} />
          {usage.cpuPercent != null && (
            <Progress
              percent={Math.min(100, usage.cpuPercent)}
              format={() => formatPercent(usage.cpuPercent as number)}
              size="small"
              style={{ marginTop: 8 }}
            />
          )}
          {scoped && <Text type="secondary">仅统计可见命名空间内 Pod</Text>}
        </Col>
        <Col xs={24} md={12}>
          <Statistic title="内存用量" value={formatBytes(usage.memoryBytes)} />
          {usage.memoryPercent != null && (
            <Progress
              percent={Math.min(100, usage.memoryPercent)}
              format={() => formatPercent(usage.memoryPercent as number)}
              size="small"
              style={{ marginTop: 8 }}
            />
          )}
          {scoped && <Text type="secondary">仅统计可见命名空间内 Pod</Text>}
        </Col>
      </Row>

      <Row gutter={[16, 8]} style={{ marginTop: 16 }}>
        <Col xs={24} md={12}>
          <Text type="secondary">CPU 趋势</Text>
          <Sparkline values={cpuValues} color="#1677ff" />
        </Col>
        <Col xs={24} md={12}>
          <Text type="secondary">内存趋势</Text>
          <Sparkline values={memoryValues} color="#52c41a" />
        </Col>
      </Row>

      <Text type="secondary">
        本次会话采样 {samples.length} 个点
        {windowLabel ? `（${windowLabel}）` : ''}，刷新或重新进入页面后重新开始。
      </Text>
    </Card>
  )
}

/** OverviewPage 是进入集群后的默认落地页，只读、按权限范围统计。 */
export default function OverviewPage() {
  const { cluster } = useParams<{ cluster: string }>()
  const overview = useOverview(cluster)
  const usageSamples = useUsageTrend(cluster, overview.data?.usage)

  if (overview.isLoading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: 240 }}>
        <Spin />
      </div>
    )
  }

  if (overview.isError && !overview.data) {
    const unreachable = overview.error instanceof ApiError && overview.error.code === 'CLUSTER_UNREACHABLE'
    return (
      <Alert
        type="error"
        showIcon
        message={unreachable ? '当前集群不可达' : '总览加载失败'}
        description={
          overview.error instanceof Error
            ? overview.error.message
            : '请检查网络或稍后重试。'
        }
        action={
          <Button size="small" onClick={() => void overview.refetch()}>
            重试
          </Button>
        }
      />
    )
  }

  const data = overview.data
  if (!data) return null

  const nodePercent = data.nodes.total > 0 ? Math.round((data.nodes.ready / data.nodes.total) * 100) : 0
  const hasWorkloads =
    data.pods.total > 0 ||
    data.workloads.deployments.total > 0 ||
    data.workloads.statefulSets.total > 0 ||
    data.workloads.daemonSets.total > 0

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Row justify="space-between" align="middle" gutter={12}>
        <Col>
          <Title level={4} style={{ margin: 0 }}>
            {cluster} 总览
          </Title>
          <Space size={8} wrap>
            <Text type="secondary">最近更新：{formatTime(data.fetchedAt)}</Text>
            {!data.scope.allNamespaces && (
              <Tag color="blue">统计范围：仅可见命名空间（{data.scope.namespaces} 个）</Tag>
            )}
            {data.scope.truncated && <Tag color="orange">部分统计已截断</Tag>}
          </Space>
        </Col>
        <Col>
          <Button
            icon={<ReloadOutlined />}
            loading={overview.isFetching && !overview.isLoading}
            onClick={() => void overview.refetch()}
          >
            刷新
          </Button>
        </Col>
      </Row>

      {data.unavailable.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message={`部分统计不可用：${data.unavailable.map(unavailableLabel).join('、')}`}
          description="其余统计仍按当前权限范围展示，可稍后刷新重试。"
        />
      )}

      {overview.isError && (
        <Alert
          type="warning"
          showIcon
          message="刷新失败，正在显示上一次结果"
          description={overview.error instanceof Error ? overview.error.message : undefined}
        />
      )}

      {!hasWorkloads && (
        <Alert
          type="info"
          showIcon
          message="当前可见范围内没有工作负载"
          description="节点与容量信息仍然可用；接入工作负载后这里会自动更新。"
        />
      )}

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="节点"
              value={data.nodes.ready}
              suffix={`/ ${data.nodes.total} 就绪`}
              valueStyle={{ color: data.nodes.total > data.nodes.ready ? '#faad14' : undefined }}
            />
            <Progress
              percent={nodePercent}
              status={nodePercent === 100 ? 'success' : 'active'}
              size="small"
              style={{ marginTop: 8 }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="可分配 CPU" value={formatMilliCPU(data.nodes.allocatable.cpuMilli)} />
            <Text type="secondary">全部就绪节点汇总</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="可分配内存" value={formatBytes(data.nodes.allocatable.memoryBytes)} />
            <Text type="secondary">全部就绪节点汇总</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="命名空间" value={data.namespaces.total} suffix="个" />
          </Card>
        </Col>
      </Row>

      <UsageCard usage={data.usage} samples={usageSamples} />

      <Card title="Pod 状态" extra={<Tag>{data.pods.total} 个</Tag>}>
        {data.pods.total === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="可见范围内没有 Pod" />
        ) : (
          <Row gutter={[16, 16]}>
            <Col xs={12} md={8} lg={4}>
              <Statistic title="Running" value={data.pods.running} />
            </Col>
            <Col xs={12} md={8} lg={4}>
              <Statistic
                title="Pending"
                value={data.pods.pending}
                valueStyle={{ color: data.pods.pending > 0 ? '#faad14' : undefined }}
              />
            </Col>
            <Col xs={12} md={8} lg={4}>
              <Statistic title="Succeeded" value={data.pods.succeeded} />
            </Col>
            <Col xs={12} md={8} lg={4}>
              <Statistic
                title="Failed"
                value={data.pods.failed}
                valueStyle={{ color: data.pods.failed > 0 ? '#ff4d4f' : undefined }}
              />
            </Col>
            <Col xs={12} md={8} lg={4}>
              <Statistic
                title="Unknown"
                value={data.pods.unknown}
                valueStyle={{ color: data.pods.unknown > 0 ? '#ff4d4f' : undefined }}
              />
            </Col>
            <Col xs={12} md={8} lg={4}>
              <Statistic
                title="CrashLoop / 拉取失败"
                value={data.pods.crashLoop}
                valueStyle={{ color: data.pods.crashLoop > 0 ? '#ff4d4f' : undefined }}
              />
            </Col>
          </Row>
        )}
      </Card>

      <Card title="工作负载">
        <Table
          size="small"
          pagination={false}
          rowKey="key"
          dataSource={[
            { key: 'deployments', name: 'Deployment', ...data.workloads.deployments },
            { key: 'statefulSets', name: 'StatefulSet', ...data.workloads.statefulSets },
            { key: 'daemonSets', name: 'DaemonSet', ...data.workloads.daemonSets },
          ]}
          columns={[
            { title: '类型', dataIndex: 'name' },
            { title: '总数', dataIndex: 'total', width: 120 },
            {
              title: '异常',
              dataIndex: 'unavailable',
              width: 120,
              render: (value: number) =>
                value > 0 ? <Tag color="error">{value}</Tag> : <Tag color="success">正常</Tag>,
            },
          ]}
        />
      </Card>

      <Card title="最近事件" extra={<Text type="secondary">最多 20 条</Text>}>
        {data.events.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有最近事件" />
        ) : (
          <Table
            size="small"
            rowKey={(record) => `${record.namespace ?? ''}/${record.object}/${record.lastTimestamp}`}
            dataSource={data.events}
            columns={EVENT_COLUMNS}
            pagination={false}
          />
        )}
      </Card>
    </Space>
  )
}