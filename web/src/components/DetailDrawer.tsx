import { Alert, Descriptions, Drawer, Spin, Tabs, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { api, type ResourceItem } from '../lib/api'
import { ageOf } from '../lib/format'
import type { ResourceType } from '../features/resources/resourceTypes'

interface Props {
  cluster?: string
  type: ResourceType
  item?: ResourceItem
  onClose: () => void
}

export default function DetailDrawer({ cluster, type, item, onClose }: Props) {
  const yamlQuery = useQuery({
    queryKey: ['yaml', cluster, type.key, item?.metadata.namespace, item?.metadata.name],
    queryFn: ({ signal }) =>
      api.yaml({
        cluster: cluster!,
        gvr: type.gvr,
        namespace: item!.metadata.namespace ?? '',
        name: item!.metadata.name,
        signal,
      }),
    enabled: !!item && !!cluster,
  })

  const title = item
    ? `${type.label} / ${item.metadata.namespace ? `${item.metadata.namespace}/` : ''}${item.metadata.name}`
    : ''

  return (
    <Drawer width={760} open={!!item} onClose={onClose} title={title} destroyOnHidden>
      {item && (
        <Tabs
          items={[
            {
              key: 'overview',
              label: '概览',
              children: (
                <Descriptions column={1} size="small" bordered styles={{ label: { width: 140 } }}>
                  <Descriptions.Item label="名称">{item.metadata.name}</Descriptions.Item>
                  <Descriptions.Item label="命名空间">
                    {item.metadata.namespace ?? '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="创建时间">
                    {item.metadata.creationTimestamp
                      ? `${new Date(item.metadata.creationTimestamp).toLocaleString()} (${ageOf(item.metadata.creationTimestamp)})`
                      : '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="资源版本">
                    {item.metadata.resourceVersion ?? '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="标签">
                    {Object.entries(item.metadata.labels ?? {}).length === 0
                      ? '-'
                      : Object.entries(item.metadata.labels ?? {}).map(([k, v]) => (
                          <Tag key={k} style={{ marginBottom: 4 }}>
                            {k}={v}
                          </Tag>
                        ))}
                  </Descriptions.Item>
                  <Descriptions.Item label="所有者">
                    {item.metadata.ownerReferences?.length
                      ? item.metadata.ownerReferences.map((o) => `${o.kind}/${o.name}`).join(', ')
                      : '-'}
                  </Descriptions.Item>
                </Descriptions>
              ),
            },
            {
              key: 'yaml',
              label: 'YAML',
              children: yamlQuery.isLoading ? (
                <Spin />
              ) : yamlQuery.error ? (
                <Alert
                  type="error"
                  showIcon
                  message={
                    yamlQuery.error instanceof Error ? yamlQuery.error.message : 'YAML 加载失败'
                  }
                />
              ) : (
                <pre
                  style={{
                    margin: 0,
                    padding: 12,
                    background: '#fafafa',
                    border: '1px solid #f0f0f0',
                    borderRadius: 6,
                    fontSize: 12,
                    lineHeight: 1.6,
                    overflow: 'auto',
                    maxHeight: 'calc(100vh - 220px)',
                  }}
                >
                  <Typography.Text copyable={{ text: yamlQuery.data ?? '' }} />
                  {yamlQuery.data}
                </pre>
              ),
            },
          ]}
        />
      )}
    </Drawer>
  )
}
