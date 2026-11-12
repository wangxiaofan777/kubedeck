import { Input, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { SearchOutlined } from '@ant-design/icons'
import type { ResourceItem } from '../lib/api'
import { ageOf, statusColor } from '../lib/format'
import { statusOf, type ResourceType } from '../features/resources/resourceTypes'

interface Props {
  type: ResourceType
  items: ResourceItem[]
  loading: boolean
  search: string
  onSearch: (value: string) => void
  onSelect: (item: ResourceItem) => void
  namespace: string
}

export default function ResourceTable({
  type,
  items,
  loading,
  search,
  onSearch,
  onSelect,
  namespace,
}: Props) {
  const columns: ColumnsType<ResourceItem> = [
    {
      title: '名称',
      key: 'name',
      ellipsis: true,
      render: (_, item) => <Typography.Link>{item.metadata.name}</Typography.Link>,
    },
    ...(type.namespaced && !namespace
      ? [
          {
            title: '命名空间',
            key: 'namespace',
            width: 160,
            render: (_: unknown, item: ResourceItem) => item.metadata.namespace ?? '-',
          },
        ]
      : []),
    {
      title: '状态',
      key: 'status',
      width: 150,
      render: (_, item) => {
        const value = statusOf(item, type)
        return value === '-' ? '-' : <Tag color={statusColor(value)}>{value}</Tag>
      },
    },
    {
      title: 'Age',
      key: 'age',
      width: 90,
      render: (_, item) => ageOf(item.metadata.creationTimestamp),
    },
  ]

  return (
    <div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 12,
        }}
      >
        <Typography.Title level={5} style={{ margin: 0 }}>
          {type.label}
          <Typography.Text
            type="secondary"
            style={{ marginLeft: 8, fontSize: 12, fontWeight: 400 }}
          >
            {items.length} 项
          </Typography.Text>
        </Typography.Title>
        <Input
          allowClear
          aria-label="按名称过滤"
          placeholder="按名称过滤"
          prefix={<SearchOutlined />}
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          style={{ width: 260 }}
        />
      </div>
      <Table<ResourceItem>
        size="middle"
        rowKey={(item) => `${item.metadata.namespace ?? ''}/${item.metadata.name}`}
        columns={columns}
        dataSource={items}
        loading={loading}
        pagination={{ pageSize: 50, showSizeChanger: false }}
        onRow={(record) => ({
          tabIndex: 0,
          style: { cursor: 'pointer' },
          onClick: () => onSelect(record),
          onKeyDown: (event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault()
              onSelect(record)
            }
          },
        })}
      />
    </div>
  )
}
