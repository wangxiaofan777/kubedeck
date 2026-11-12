import { Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { ResourceItem } from '../lib/api'
import { ageOf, statusColor } from '../lib/format'
import { statusOf, type ResourceType } from '../features/resources/resourceTypes'

interface Props {
  type: ResourceType
  items: ResourceItem[]
  loading: boolean
  namespace: string
  selectedKeys: string[]
  onSelectionChange: (keys: string[]) => void
  onSelect: (item: ResourceItem) => void
}

export function itemKey(item: ResourceItem): string {
  return item.metadata.uid ?? `${item.metadata.namespace ?? ''}/${item.metadata.name}`
}

export default function ResourceTable({
  type,
  items,
  loading,
  namespace,
  selectedKeys,
  onSelectionChange,
  onSelect,
}: Props) {
  const columns: ColumnsType<ResourceItem> = [
    {
      title: '名称',
      key: 'name',
      ellipsis: true,
      sorter: (a, b) => a.metadata.name.localeCompare(b.metadata.name),
      render: (_, item) => (
        <Typography.Link onClick={() => onSelect(item)}>{item.metadata.name}</Typography.Link>
      ),
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
      width: 160,
      render: (_, item) => {
        const value = statusOf(item, type)
        return value === '-' ? '-' : <Tag color={statusColor(value)}>{value}</Tag>
      },
    },
    {
      title: 'Age',
      key: 'age',
      width: 90,
      sorter: (a, b) =>
        new Date(a.metadata.creationTimestamp ?? 0).getTime() -
        new Date(b.metadata.creationTimestamp ?? 0).getTime(),
      render: (_, item) => ageOf(item.metadata.creationTimestamp),
    },
  ]

  return (
    <Table<ResourceItem>
      size="middle"
      rowKey={itemKey}
      columns={columns}
      dataSource={items}
      loading={loading}
      virtual={items.length > 200}
      scroll={items.length > 200 ? { y: 560, x: 'max-content' } : undefined}
      rowSelection={
        type.key === 'namespaces'
          ? undefined
          : {
              selectedRowKeys: selectedKeys,
              onChange: (keys) => onSelectionChange(keys.map(String)),
            }
      }
      pagination={false}
      onRow={(record) => ({
        tabIndex: 0,
        style: { cursor: 'pointer' },
        onClick: (event) => {
          if ((event.target as HTMLElement).closest('.ant-table-selection-column')) return
          onSelect(record)
        },
        onKeyDown: (event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            onSelect(record)
          }
        },
      })}
    />
  )
}
