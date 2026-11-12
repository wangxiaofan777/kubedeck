import { Alert, Descriptions, Drawer, Spin, Tabs, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { api, type ApiError, type ResourceItem } from '../api'
import { ageOf, type ResourceType } from '../resourceTypes'

interface Props {
  cluster?: string
  type: ResourceType
  item?: ResourceItem
  onClose: () => void
}

export default function DetailDrawer({ cluster, type, item, onClose }: Props) {
  const yamlQuery = useQuery({
    queryKey: ['yaml', cluster, type.key, item?.metadata.namespace, item?.metadata.name],
    queryFn: () =>
      api.yaml(
        cluster!,
        type.path[0],
        type.path[1],
        type.path[2],
        item!.metadata.namespace ?? '',
        item!.metadata.name,
      ),
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
                  <Descriptions.Item label="命名空间">{item.metadata.namespace ?? '-'}</Descriptions.Item>
                  <Descriptions.Item label="创建时间">
                    {item.metadata.creationTimestamp
                      ? `${new Date(item.metadata.creationTimestamp).toLocaleString()} (${ageOf(item.metadata.creationTimestamp)})`
                      : '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="资源版本">{item.metadata.resourceVersion ?? '-'}</Descriptions.Item>
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
                <Alert type="error" showIcon message={(yamlQuery.error as ApiError).message} />
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