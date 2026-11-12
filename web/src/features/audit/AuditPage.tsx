import { useState } from 'react'
import {
  Alert,
  Button,
  DatePicker,
  Input,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import { CheckCircleOutlined, DownloadOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import type { Dayjs } from 'dayjs'
import { api, type AuditEntry } from '../../lib/api'

export default function AuditPage() {
  const { cluster } = useParams()
  const [range, setRange] = useState<[Dayjs, Dayjs]>()
  const [actor, setActor] = useState('')
  const [resource, setResource] = useState('')
  const [result, setResult] = useState<string>()
  const [offset, setOffset] = useState(0)
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
