import { useMemo, useState } from 'react'
import {
  Alert,
  App,
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
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd'
import {
  CheckCircleOutlined,
  DownloadOutlined,
  SafetyCertificateOutlined,
  SearchOutlined,
} from '@ant-design/icons'
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
import PageHeader from '../../components/PageHeader'
import StatusPill from '../../components/StatusPill'
import { EmptyState, ErrorState, PageSkeleton } from '../../components/StateViews'
import { formatDateTime } from '../../lib/format'
import { AreaSpark } from '../../components/Charts'

function download(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

export default function AuditPage() {
  const { cluster } = useParams()
  const { message } = App.useApp()
  const [range, setRange] = useState<[Dayjs, Dayjs]>()
  const [actor, setActor] = useState('')
  const [resource, setResource] = useState('')
  const [result, setResult] = useState<string>()
  const [offset, setOffset] = useState(0)
  const [statsDays, setStatsDays] = useState<7 | 30>(7)
  const [exporting, setExporting] = useState<'csv' | 'json'>()
  const [verifyState, setVerifyState] = useState<{
    checked: number
    intact: boolean
    truncated?: boolean
    brokenId?: number
    brokenKind?: string
  }>()
  const [verifying, setVerifying] = useState(false)
  const limit = 50

  const filters = useMemo(
    () => ({
      cluster,
      from: range?.[0]?.toISOString(),
      to: range?.[1]?.toISOString(),
      actor: actor || undefined,
      resource: resource || undefined,
      result,
    }),
    [cluster, range, actor, resource, result],
  )

  const query = useQuery({
    queryKey: ['audit', filters, offset],
    queryFn: ({ signal }) => api.auditLogs({ ...filters, limit, offset, signal }),
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

  const runVerify = async () => {
    setVerifying(true)
    try {
      const value = await api.verifyAudit()
      setVerifyState(value)
      if (value.intact) {
        message.success(
          `校验通过，共 ${value.checked} 条${value.truncated ? '（起点已按保留期截断）' : ''}`,
        )
      } else {
        message.error(`校验失败：第 ${value.brokenId} 条记录（${value.brokenKind}）断裂`)
      }
    } catch (error) {
      message.error(error instanceof Error ? error.message : '校验失败')
    } finally {
      setVerifying(false)
    }
  }

  const runExport = async (format: 'csv' | 'json') => {
    setExporting(format)
    try {
      const { blob, count, truncated } = await api.exportAudit(format, filters)
      download(blob, `kubedeck-audit-${dayjs().format('YYYYMMDD-HHmmss')}.${format}`)
      message.success(
        `已导出 ${count} 条${truncated ? '（达到单次上限，请缩小时间范围）' : ''}`,
      )
    } catch (error) {
      message.error(error instanceof Error ? error.message : '导出失败')
    } finally {
      setExporting(undefined)
    }
  }

  const columns = [
    {
      title: '时间',
      dataIndex: 'occurredAt',
      width: 160,
      render: (value: string) => formatDateTime(value),
    },
    { title: '操作者', dataIndex: 'actorSubject', width: 150, ellipsis: true },
    { title: '集群', dataIndex: 'clusterName', width: 110 },
    { title: '动作', dataIndex: 'verb', width: 120 },
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
        <StatusPill value={`${entry.result} ${entry.resultCode}`} />
      ),
    },
    { title: '耗时', dataIndex: 'latencyMs', width: 80, render: (value: number) => `${value}ms` },
    {
      title: '请求号',
      dataIndex: 'requestId',
      width: 180,
      ellipsis: true,
      render: (value: string) => <Typography.Text copyable>{value}</Typography.Text>,
    },
  ]

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `/clusters/${encodeURIComponent(cluster ?? '')}/overview` },
          { label: '审计日志' },
        ]}
        title="审计日志"
        subtitle={<span className="kd-muted">所有写操作与敏感读都可追溯</span>}
        tags={cluster ? <Tag>{cluster}</Tag> : undefined}
        actions={
          <Space>
            <Button
              icon={<SafetyCertificateOutlined />}
              loading={verifying}
              onClick={() => void runVerify()}
            >
              校验完整性
            </Button>
            <Button
              icon={<DownloadOutlined />}
              loading={exporting === 'csv'}
              onClick={() => void runExport('csv')}
            >
              导出 CSV
            </Button>
            <Button
              icon={<DownloadOutlined />}
              loading={exporting === 'json'}
              onClick={() => void runExport('json')}
            >
              导出 JSON
            </Button>
          </Space>
        }
      />

      {verifyState && (
        <Alert
          type={verifyState.intact ? 'success' : 'error'}
          showIcon
          icon={<CheckCircleOutlined />}
          style={{ marginBottom: 14 }}
          message={
            verifyState.intact
              ? `哈希链完整（${verifyState.checked} 条${verifyState.truncated ? '，起点已按保留期截断' : ''}）`
              : `哈希链在记录 ${verifyState.brokenId} 处断裂（${verifyState.brokenKind}）`
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

      <Card className="kd-table-card" styles={{ body: { padding: 0 } }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            flexWrap: 'wrap',
            padding: '12px 16px',
            borderBottom: '1px solid #f1f5f9',
          }}
        >
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
            prefix={<SearchOutlined />}
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
        </div>

        {query.data?.retentionWarning && (
          <Alert
            type="warning"
            showIcon
            style={{ margin: 12 }}
            message={query.data.retentionWarning}
          />
        )}

        {query.isLoading ? (
          <div style={{ padding: 16 }}>
            <PageSkeleton rows={8} />
          </div>
        ) : query.isError ? (
          <div style={{ padding: 16 }}>
            <ErrorState error={query.error} onRetry={() => void query.refetch()} />
          </div>
        ) : (query.data?.items.length ?? 0) === 0 ? (
          <EmptyState description="该条件下没有审计记录" />
        ) : (
          <Table<AuditEntry>
            size="small"
            rowKey="id"
            loading={query.isFetching && !query.isLoading}
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
        )}
      </Card>
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

function AuditStatsCard({ days, onDaysChange, loading, error, stats, onRetry }: AuditStatsCardProps) {
  const failureRate = stats && stats.total > 0 ? Math.round((stats.failed / stats.total) * 100) : 0

  return (
    <Card
      size="small"
      title="审计概览"
      style={{ marginBottom: 14 }}
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
        <PageSkeleton rows={3} />
      ) : error ? (
        <ErrorState error={error} title="审计统计加载失败" onRetry={onRetry} />
      ) : !stats ? null : stats.total === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该时间范围内没有审计记录" />
      ) : (
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
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
                valueStyle={{ color: stats.failed > 0 ? '#b91c1c' : undefined }}
              />
            </Col>
            <Col xs={12} md={6}>
              <div className="kd-chart-label">失败率</div>
              <div style={{ fontSize: 22, fontWeight: 650 }}>{failureRate}%</div>
              <Progress
                percent={failureRate}
                size="small"
                status={failureRate > 0 ? 'exception' : 'success'}
                showInfo={false}
              />
            </Col>
          </Row>
          <div>
            <div className="kd-chart-label" style={{ marginBottom: 4 }}>
              按天趋势
            </div>
            <AreaSpark
              values={stats.byDay.map((day) => day.total)}
              color="#2563eb"
              height={64}
              label="审计记录趋势"
            />
          </div>
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