import { useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Col,
  DatePicker,
  Empty,
  Input,
  Progress,
  Row,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import { CheckCircleOutlined, DownloadOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import dayjs, { type Dayjs } from 'dayjs'
import {
  api,
  type AuditCount,
  type AuditDayCount,
  type AuditEntry,
  type AuditStats,
} from '../../lib/api'

export default function AuditPage() {
  const { cluster } = useParams()
  const [range, setRange] = useState<[Dayjs, Dayjs]>()
  const [actor, setActor] = useState('')
  const [resource, setResource] = useState('')
  const [result, setResult] = useState<string>()
  const [offset, setOffset] = useState(0)
  const [statsDays, setStatsDays] = useState<7 | 30>(7)
  const limit = 50

  const query = useQuery({
    queryKey: [
      'audit',
      cluster,
      range?.[0]?.toISOString(),
      range?.[1]?.toISOString(),
      actor,
      resource,
      result,
      offset,
    ],
    queryFn: ({ signal }) =>
      api.auditLogs({
        cluster,
        from: range?.[0]?.toISOString(),
        to: range?.[1]?.toISOString(),
        actor: actor || undefined,
        resource: resource || undefined,
        result,
        limit,
        offset,
        signal,
      }),
  })

  const statsQuery = useQuery({
    queryKey: ['audit-stats', cluster, statsDays],
    queryFn: ({ signal }) =>
      api.auditStats({
        cluster,
        from: dayjs().subtract(statsDays, 'day').toISOString(),
        signal,
      }),
  })

  const verify = useQuery({
    queryKey: ['audit-verify'],
    queryFn: () => api.verifyAudit(),
    enabled: false,
  })

  const columns = [
    {
      title: '时间',
      dataIndex: 'occurredAt',
      width: 190,
      render: (value: string) => new Date(value).toLocaleString(),
    },
    { title: '操作者', dataIndex: 'actorSubject', width: 150, ellipsis: true },
    { title: '集群', dataIndex: 'clusterName', width: 110 },
    { title: '动作', dataIndex: 'verb', width: 110 },
    {
      title: '资源',
      key: 'target',
      ellipsis: true,
      render: (_: unknown, entry: AuditEntry) =>
        [entry.resource, entry.namespace, entry.objectName].filter(Boolean).join(' / ') || '-',
    },
    {
      title: '结果',
      key: 'result',
      width: 120,
      render: (_: unknown, entry: AuditEntry) => (
        <Tag color={entry.result === 'success' ? 'green' : 'red'}>
          {entry.result} {entry.resultCode}
        </Tag>
      ),
    },
    { title: '耗时', dataIndex: 'latencyMs', width: 80, render: (v: number) => `${v}ms` },
    {
      title: '请求号',
      dataIndex: 'requestId',
      width: 190,
      ellipsis: true,
      render: (value: string) => <Typography.Text copyable>{value}</Typography.Text>,
    },
  ]

  return (
    <div>
      <Space style={{ marginBottom: 12 }} align="center" wrap>
        <Typography.Title level={5} style={{ margin: 0 }}>
          审计日志
        </Typography.Title>
        {cluster && <Tag>{cluster}</Tag>}
        <div style={{ flex: 1 }} />
        <DatePicker.RangePicker
          showTime
          value={range}
          onChange={(value) => {
            setOffset(0)
            setRange(value?.[0] && value[1] ? [value[0], value[1]] : undefined)
          }}
        />
        <Input
          allowClear
          placeholder="操作者"
          value={actor}
          onChange={(event) => {
            setOffset(0)
            setActor(event.target.value)
          }}
          style={{ width: 160 }}
          aria-label="按操作者过滤"
        />
        <Input
          allowClear
          placeholder="资源类型"
          value={resource}
          onChange={(event) => {
            setOffset(0)
            setResource(event.target.value)
          }}
          style={{ width: 150 }}
          aria-label="按资源过滤"
        />
        <Select
          allowClear
          placeholder="结果"
          value={result}
          onChange={(value) => {
            setOffset(0)
            setResult(value)
          }}
          style={{ width: 120 }}
          options={[
            { value: 'success', label: '成功' },
            { value: 'failure', label: '失败' },
          ]}
        />
        <Button
          icon={<SafetyCertificateOutlined />}
          loading={verify.isFetching}
          onClick={async () => {
            const data = await verify.refetch()
            const value = data.data
            if (!value) return
            if (value.intact) {
              message.success(`校验通过，共 ${value.checked} 条记录`)
            } else {
              message.error(`校验失败：第 ${value.brokenId} 条记录（${value.brokenKind}）断裂`)
            }
          }}
        >
          校验完整性
        </Button>
        <Button
          icon={<DownloadOutlined />}
          onClick={() => {
            window.location.assign(
              api.auditExportURL('csv', {
                cluster,
                from: range?.[0]?.toISOString(),
                to: range?.[1]?.toISOString(),
                actor: actor || undefined,
                resource: resource || undefined,
                result,
              }),
            )
          }}
        >
          导出 CSV
        </Button>
        <Button
          icon={<DownloadOutlined />}
          onClick={() => {
            window.location.assign(
              api.auditExportURL('json', {
                cluster,
                from: range?.[0]?.toISOString(),
                to: range?.[1]?.toISOString(),
                actor: actor || undefined,
                resource: resource || undefined,
                result,
              }),
            )
          }}
        >
          导出 JSON
        </Button>
      </Space>

      {verify.data && (
        <Alert
          type={verify.data.intact ? 'success' : 'error'}
          showIcon
          icon={<CheckCircleOutlined />}
          style={{ marginBottom: 12 }}
          message={
            verify.data.intact
              ? `哈希链完整（${verify.data.checked} 条）`
              : `哈希链在记录 ${verify.data.brokenId} 处断裂（${verify.data.brokenKind}）`
          }
        />
      )}
      <AuditStatsCard
        days={statsDays}
        onDaysChange={setStatsDays}
        loading={statsQuery.isLoading}
        error={statsQuery.error}
        stats={statsQuery.data}
        onRetry={() => void statsQuery.refetch()}
      />

      {query.data?.retentionWarning && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={query.data.retentionWarning}
        />
      )}

      <Table<AuditEntry>
        size="small"
        rowKey="id"
        loading={query.isFetching}
        dataSource={query.data?.items ?? []}
        columns={columns}
        pagination={{
          total: query.data?.total ?? 0,
          pageSize: limit,
          current: Math.floor(offset / limit) + 1,
          showSizeChanger: false,
          onChange: (page) => setOffset((page - 1) * limit),
        }}
      />
    </div>
  )
}

interface AuditStatsCardProps {
  days: 7 | 30
  onDaysChange: (days: 7 | 30) => void
  loading: boolean
  error: unknown
  stats?: AuditStats
  onRetry: () => void
}

const COUNT_COLUMNS = [
  { title: '键', dataIndex: 'key', ellipsis: true },
  { title: '次数', dataIndex: 'count', width: 72 },
]

const DAY_COLUMNS = [
  { title: '日期', dataIndex: 'day', width: 110 },
  { title: '总数', dataIndex: 'total', width: 72 },
  {
    title: '失败',
    dataIndex: 'failed',
    width: 72,
    render: (value: number) => (value > 0 ? <Tag color="error">{value}</Tag> : 0),
  },
]

/** AuditStatsCard 展示聚合统计，失败或为空时不影响下方审计列表。 */
function AuditStatsCard({
  days,
  onDaysChange,
  loading,
  error,
  stats,
  onRetry,
}: AuditStatsCardProps) {
  const failureRate = stats && stats.total > 0 ? Math.round((stats.failed / stats.total) * 100) : 0

  return (
    <Card
      size="small"
      title="审计概览"
      style={{ marginBottom: 12 }}
      extra={
        <Select<7 | 30>
          aria-label="统计时间范围"
          value={days}
          onChange={onDaysChange}
          style={{ width: 120 }}
          options={[
            { value: 7, label: '最近 7 天' },
            { value: 30, label: '最近 30 天' },
          ]}
        />
      }
    >
      {loading ? (
        <div style={{ padding: 24, textAlign: 'center' }}>
          <Spin />
        </div>
      ) : error ? (
        <Alert
          type="warning"
          showIcon
          message="审计统计加载失败"
          description={error instanceof Error ? error.message : undefined}
          action={
            <Button size="small" onClick={onRetry}>
              重试
            </Button>
          }
        />
      ) : !stats ? null : stats.total === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该时间范围内没有审计记录" />
      ) : (
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Row gutter={16}>
            <Col xs={12} md={6}>
              <Statistic title="总数" value={stats.total} />
            </Col>
            <Col xs={12} md={6}>
              <Statistic title="成功" value={stats.success} />
            </Col>
            <Col xs={12} md={6}>
              <Statistic
                title="失败"
                value={stats.failed}
                valueStyle={{ color: stats.failed > 0 ? '#ff4d4f' : undefined }}
              />
            </Col>
            <Col xs={12} md={6}>
              <Statistic title="失败率" value={failureRate} suffix="%" />
              <Progress
                percent={failureRate}
                size="small"
                status={failureRate > 0 ? 'exception' : 'success'}
              />
            </Col>
          </Row>
          <Row gutter={[16, 16]}>
            <Col xs={24} md={12} lg={6}>
              <Typography.Text strong>按天</Typography.Text>
              <Table<AuditDayCount>
                size="small"
                rowKey="day"
                columns={DAY_COLUMNS}
                dataSource={stats.byDay}
                pagination={false}
                scroll={{ y: 200 }}
              />
            </Col>
            <Col xs={24} md={12} lg={6}>
              <Typography.Text strong>高频动作</Typography.Text>
              <Table<AuditCount>
                size="small"
                rowKey="key"
                columns={COUNT_COLUMNS}
                dataSource={stats.byVerb}
                pagination={false}
                scroll={{ y: 200 }}
              />
            </Col>
            <Col xs={24} md={12} lg={6}>
              <Typography.Text strong>高频资源</Typography.Text>
              <Table<AuditCount>
                size="small"
                rowKey="key"
                columns={COUNT_COLUMNS}
                dataSource={stats.byResource}
                pagination={false}
                scroll={{ y: 200 }}
              />
            </Col>
            <Col xs={24} md={12} lg={6}>
              <Typography.Text strong>高频操作者</Typography.Text>
              <Table<AuditCount>
                size="small"
                rowKey="key"
                columns={COUNT_COLUMNS}
                dataSource={stats.byActor}
                pagination={false}
                scroll={{ y: 200 }}
              />
            </Col>
          </Row>
        </Space>
      )}
    </Card>
  )
}
