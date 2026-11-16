import { Button, Table } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { api, type EventItem } from '../../lib/api'
import { ErrorState } from '../../components/StateViews'
import StatusPill from '../../components/StatusPill'
import { formatDateTime } from '../../lib/format'
import { useResourceDetail } from './ResourceDetailLayout'

const eventColumns = [
  {
    title: '类型',
    dataIndex: 'type',
    width: 90,
    render: (value: string) => <StatusPill value={value} />,
  },
  { title: '原因', dataIndex: 'reason', width: 180 },
  { title: '消息', dataIndex: 'message', ellipsis: true },
  { title: '次数', dataIndex: 'count', width: 70 },
  {
    title: '最后发生',
    dataIndex: 'lastTimestamp',
    width: 170,
    render: (value: string) => formatDateTime(value),
  },
]

export default function ResourceEventsPage() {
  const { cluster, type, namespace, name } = useResourceDetail()
  const eventsQuery = useQuery({
    queryKey: ['events', cluster, namespace, type.kind, name],
    queryFn: ({ signal }) =>
      api.events(cluster, { namespace, kind: type.kind, name, signal }),
  })

  if (eventsQuery.isError) {
    return (
      <ErrorState
        error={eventsQuery.error}
        title="无法加载事件"
        onRetry={() => void eventsQuery.refetch()}
      />
    )
  }

  return (
    <Table<EventItem>
      size="small"
      rowKey={(event) =>
        `${event.reason}-${event.lastTimestamp}-${event.involvedObject?.name ?? ''}`
      }
      loading={eventsQuery.isFetching && !eventsQuery.isLoading}
      dataSource={eventsQuery.data?.items ?? []}
      columns={eventColumns}
      pagination={false}
      locale={{
        emptyText: (
          <div style={{ padding: 24 }}>
            <div className="kd-muted">该对象最近没有事件。</div>
            <Button size="small" style={{ marginTop: 8 }} onClick={() => void eventsQuery.refetch()}>
              刷新
            </Button>
          </div>
        ),
      }}
    />
  )
}