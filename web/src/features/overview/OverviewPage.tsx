import { Alert, Button, Card, Col, Empty, Progress, Row, Space, Spin, Statistic, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useParams } from 'react-router-dom'
import { ApiError, type OverviewEvent } from '../../lib/api'
import { useOverview } from './useOverview'

const { Title, Text } = Typography

const UNAVAILABLE_LABELS: Record<string, string> = {
  nodes: '节点',
  namespaces: '命名空间',
  pods: 'Pod',
  deployments: 'Deployment',
  statefulsets: 'StatefulSet',
  daemonsets: 'DaemonSet',
  events: '事件',
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

/** OverviewPage 是进入集群后的默认落地页，只读、按权限范围统计。 */
export default function OverviewPage() {
  const { cluster } = useParams<{ cluster: string }>()
  const overview = useOverview(cluster)

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