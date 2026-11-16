import { Card, Descriptions, Space, Table, Tag } from 'antd'
import StatusPill from '../../components/StatusPill'
import { ageOf, formatDateTime } from '../../lib/format'
import WorkloadActions from '../workloads/WorkloadActions'
import { useResourceDetail } from './ResourceDetailLayout'
import { statusOf } from './resourceTypes'

export default function ResourceOverviewPage() {
  const { cluster, type, namespace, item, refetch, updateCan, selector } = useResourceDetail()
  const status = statusOf(item, type)
  const isWorkload = ['deployments', 'statefulsets', 'daemonsets', 'replicasets'].includes(type.key)

  return (
    <Space direction="vertical" size={14} style={{ width: '100%' }}>
      <Card size="small" title="元数据">
        <Descriptions column={2} size="small">
          <Descriptions.Item label="名称">{item.metadata.name}</Descriptions.Item>
          <Descriptions.Item label="命名空间">
            {item.metadata.namespace ?? <span className="kd-muted">集群级</span>}
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            <StatusPill value={status} />
          </Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {item.metadata.creationTimestamp
              ? `${formatDateTime(item.metadata.creationTimestamp)}（${ageOf(item.metadata.creationTimestamp)} 前）`
              : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="资源版本" span={2}>
            <span className="kd-mono">{item.metadata.resourceVersion ?? '-'}</span>
          </Descriptions.Item>
          <Descriptions.Item label="UID" span={2}>
            <span className="kd-mono">{item.metadata.uid ?? '-'}</span>
          </Descriptions.Item>
          {selector && (
            <Descriptions.Item label="标签选择器" span={2}>
              <span className="kd-mono">{selector}</span>
            </Descriptions.Item>
          )}
          <Descriptions.Item label="标签" span={2}>
            {Object.entries(item.metadata.labels ?? {}).length === 0 ? (
              <span className="kd-muted">-</span>
            ) : (
              <Space size={4} wrap>
                {Object.entries(item.metadata.labels ?? {}).map(([key, value]) => (
                  <Tag key={key} style={{ marginInlineEnd: 0 }}>
                    {key}={value}
                  </Tag>
                ))}
              </Space>
            )}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {type.key === 'pods' && Array.isArray(item.spec?.containers) && (
        <Card size="small" title="容器">
          <Table
            rowKey={(row) => (row as { name?: string }).name ?? ''}
            size="small"
            pagination={false}
            dataSource={item.spec.containers as Record<string, unknown>[]}
            columns={[
              { title: '名称', dataIndex: 'name' },
              {
                title: '镜像',
                dataIndex: 'image',
                ellipsis: true,
                render: (value: string) => <span className="kd-mono">{value}</span>,
              },
              {
                title: '就绪',
                key: 'ready',
                width: 90,
                render: (_, row) => {
                  const statuses = Array.isArray(item.status?.containerStatuses)
                    ? (item.status.containerStatuses as {
                        name?: string
                        ready?: boolean
                        restartCount?: number
                      }[])
                    : []
                  const found = statuses.find(
                    (entry) => entry.name === (row as { name?: string }).name,
                  )
                  return found?.ready ? (
                    <StatusPill value="Ready" />
                  ) : (
                    <StatusPill value="NotReady" />
                  )
                },
              },
              {
                title: '重启',
                key: 'restarts',
                width: 80,
                render: (_, row) => {
                  const statuses = Array.isArray(item.status?.containerStatuses)
                    ? (item.status.containerStatuses as {
                        name?: string
                        restartCount?: number
                      }[])
                    : []
                  return (
                    statuses.find(
                      (entry) => entry.name === (row as { name?: string }).name,
                    )?.restartCount ?? 0
                  )
                },
              },
            ]}
          />
        </Card>
      )}

      {isWorkload && (
        <WorkloadActions
          cluster={cluster}
          gvr={type.gvr}
          namespace={namespace}
          item={item}
          onChanged={refetch}
        />
      )}

      {!updateCan.loading && !updateCan.allowed && (
        <Card size="small">
          <span className="kd-muted">当前账号对该资源只有只读权限。</span>
        </Card>
      )}
    </Space>
  )
}