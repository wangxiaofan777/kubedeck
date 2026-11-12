import { useMemo, useState } from 'react'
import { Input, List, Modal, Tag, Typography } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, type ResourceItem } from '../../lib/api'
import { RESOURCE_TYPES, type ResourceType } from '../resources/resourceTypes'

const SEARCH_TYPES = RESOURCE_TYPES.filter((t) =>
  ['pods', 'deployments', 'statefulsets', 'daemonsets', 'services'].includes(t.key),
)

interface Hit {
  type: ResourceType
  item: ResourceItem
}

function matches(item: ResourceItem, query: string): boolean {
  const q = query.toLowerCase()
  return (
    item.metadata.name.toLowerCase().includes(q) ||
    Object.values(item.metadata.labels ?? {}).some((v) => v.toLowerCase().includes(q))
  )
}

/** GlobalSearch 在当前集群范围内按名称检索常用资源类型。 */
export default function GlobalSearch({
  cluster,
  namespace,
}: {
  cluster?: string
  namespace: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const navigate = useNavigate()

  const searchQuery = useQuery({
    queryKey: ['global-search', cluster, namespace, query],
    queryFn: async ({ signal }) => {
      const results = await Promise.all(
        SEARCH_TYPES.map(async (type) => {
          const response = await api.list({
            cluster: cluster!,
            gvr: type.gvr,
            namespace: type.namespaced ? namespace : '',
            limit: 200,
            signal,
          })
          return response.items
            .filter((item) => matches(item, query))
            .map((item) => ({ type, item }))
        }),
      )
      return results.flat()
    },
    enabled: open && !!cluster && query.trim().length >= 2,
    staleTime: 10_000,
  })

  const hits = useMemo<Hit[]>(() => searchQuery.data ?? [], [searchQuery.data])

  return (
    <>
      <Input
        allowClear
        aria-label="全局搜索"
        prefix={<SearchOutlined />}
        placeholder="搜索资源名称（回车）"
        style={{ width: 260 }}
        onPressEnter={(event) => {
          const value = (event.target as HTMLInputElement).value.trim()
          if (!value || !cluster) return
          setQuery(value)
          setOpen(true)
        }}
        onChange={(event) => {
          if (event.target.value === '') setOpen(false)
        }}
      />
      <Modal
        open={open}
        title={`搜索：${query}`}
        footer={null}
        onCancel={() => setOpen(false)}
        width={720}
      >
        <List
          loading={searchQuery.isFetching}
          locale={{ emptyText: '没有匹配的资源' }}
          dataSource={hits}
          renderItem={(hit) => {
            const ns = hit.item.metadata.namespace ?? '_'
            return (
              <List.Item
                style={{ cursor: 'pointer' }}
                onClick={() => {
                  setOpen(false)
                  navigate(
                    `/clusters/${encodeURIComponent(cluster!)}/${hit.type.key}/${encodeURIComponent(ns)}/${encodeURIComponent(hit.item.metadata.name)}${hit.item.metadata.namespace ? `?namespace=${encodeURIComponent(hit.item.metadata.namespace)}` : ''}`,
                  )
                }}
              >
                <List.Item.Meta
                  title={
                    <span>
                      {hit.item.metadata.name} <Tag style={{ marginLeft: 8 }}>{hit.type.label}</Tag>
                    </span>
                  }
                  description={
                    <Typography.Text type="secondary">
                      {hit.item.metadata.namespace ?? '集群级'}
                    </Typography.Text>
                  }
                />
              </List.Item>
            )
          }}
        />
      </Modal>
    </>
  )
}
