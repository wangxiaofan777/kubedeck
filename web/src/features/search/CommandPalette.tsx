import { useEffect, useMemo, useRef, useState } from 'react'
import { SearchOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, type ResourceItem } from '../../lib/api'
import { RESOURCE_TYPES, type ResourceType } from '../resources/resourceTypes'

interface Props {
  open: boolean
  cluster?: string
  namespace: string
  onClose: () => void
}

interface Item {
  id: string
  group: string
  label: string
  meta?: string
  run: () => void
}

const SEARCH_TYPES: ResourceType[] = RESOURCE_TYPES.filter((type) =>
  ['pods', 'deployments', 'statefulsets', 'daemonsets', 'services'].includes(type.key),
)

function detailPath(cluster: string, type: ResourceType, item: ResourceItem): string {
  const ns = item.metadata.namespace ?? '_'
  const suffix = item.metadata.namespace
    ? `?namespace=${encodeURIComponent(item.metadata.namespace)}`
    : ''
  return `/clusters/${encodeURIComponent(cluster)}/${type.key}/${encodeURIComponent(ns)}/${encodeURIComponent(item.metadata.name)}${suffix}`
}

/** CommandPalette 取代全局搜索弹窗：资源、导航与动作统一入口。 */
export default function CommandPalette({ open, cluster, namespace, onClose }: Props) {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setQuery('')
      setDebounced('')
      setActive(0)
      window.setTimeout(() => inputRef.current?.focus(), 20)
    }
  }, [open])

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(query.trim()), 280)
    return () => window.clearTimeout(timer)
  }, [query])

  const searchQuery = useQuery({
    queryKey: ['command-search', cluster, namespace, debounced],
    queryFn: async ({ signal }) => {
      if (!cluster || debounced.length < 2) return []
      const results = await Promise.all(
        SEARCH_TYPES.map((type) =>
          api
            .list({
              cluster,
              gvr: type.gvr,
              namespace: type.namespaced ? namespace || undefined : undefined,
              limit: 20,
              signal,
            })
            .then((response) =>
              response.items
                .filter((item) =>
                  item.metadata.name.toLowerCase().includes(debounced.toLowerCase()),
                )
                .slice(0, 8)
                .map((item) => ({ type, item })),
            ),
        ),
      )
      return results.flat()
    },
    enabled: open && !!cluster && debounced.length >= 2,
    staleTime: 10_000,
    retry: false,
  })

  const navigation: Item[] = useMemo(() => {
    if (!cluster) return []
    const base = `/clusters/${encodeURIComponent(cluster)}`
    const suffix = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
    return [
      { id: 'nav-overview', group: '导航', label: '集群总览', run: () => navigate(`${base}/overview`) },
      { id: 'nav-pods', group: '导航', label: 'Pods', run: () => navigate(`${base}/pods${suffix}`) },
      {
        id: 'nav-deployments',
        group: '导航',
        label: 'Deployments',
        run: () => navigate(`${base}/deployments${suffix}`),
      },
      { id: 'nav-nodes', group: '导航', label: '节点', run: () => navigate(`${base}/nodes`) },
      {
        id: 'nav-create',
        group: '动作',
        label: '新建 Deployment',
        meta: '表单',
        run: () => navigate(`${base}/new/deployments${suffix}`),
      },
      {
        id: 'nav-settings',
        group: '导航',
        label: '集群设置',
        run: () => navigate(`${base}/settings`),
      },
    ]
  }, [cluster, namespace, navigate])

  const resourceItems: Item[] = useMemo(() => {
    if (!cluster) return []
    return (searchQuery.data ?? []).map(({ type, item }) => ({
      id: `res-${type.key}-${item.metadata.uid ?? item.metadata.name}`,
      group: '资源',
      label: item.metadata.name,
      meta: `${type.label}${item.metadata.namespace ? ` · ${item.metadata.namespace}` : ''}`,
      run: () => navigate(detailPath(cluster, type, item)),
    }))
  }, [searchQuery.data, cluster, navigate])

  const items = useMemo(() => {
    const q = query.trim().toLowerCase()
    const nav = q
      ? navigation.filter((item) => item.label.toLowerCase().includes(q))
      : navigation.slice(0, 4)
    return [...resourceItems, ...nav]
  }, [resourceItems, navigation, query])

  useEffect(() => {
    setActive((current) => Math.min(current, Math.max(0, items.length - 1)))
  }, [items.length])

  if (!open) return null

  const closeAndRun = (item: Item) => {
    onClose()
    item.run()
  }

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Escape') {
      onClose()
      return
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActive((current) => (current + 1) % Math.max(1, items.length))
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActive((current) => (current - 1 + items.length) % Math.max(1, items.length))
      return
    }
    if (event.key === 'Enter' && items[active]) {
      event.preventDefault()
      closeAndRun(items[active])
    }
  }

  let lastGroup = ''

  return (
    <div className="kd-command-mask" onMouseDown={onClose}>
      <div className="kd-command kd-fade-in" onMouseDown={(event) => event.stopPropagation()}>
        <input
          ref={inputRef}
          className="kd-command-input"
          placeholder="搜索资源、跳转页面或执行动作…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={onKeyDown}
          aria-label="命令面板"
        />
        <div className="kd-command-list" role="listbox">
          {searchQuery.isFetching && <div className="kd-command-empty">搜索中…</div>}
          {searchQuery.isError && (
            <div className="kd-command-empty">
              搜索失败：
              {searchQuery.error instanceof Error ? searchQuery.error.message : '请稍后重试'}
            </div>
          )}
          {!searchQuery.isFetching && !searchQuery.isError && items.length === 0 && (
            <div className="kd-command-empty">
              <SearchOutlined /> 没有匹配结果
            </div>
          )}
          {items.map((item, index) => {
            const showGroup = item.group !== lastGroup
            lastGroup = item.group
            return (
              <div key={item.id}>
                {showGroup && <div className="kd-command-group">{item.group}</div>}
                <div
                  role="option"
                  aria-selected={index === active}
                  className={`kd-command-item ${index === active ? 'active' : ''}`}
                  onMouseEnter={() => setActive(index)}
                  onClick={() => closeAndRun(item)}
                >
                  <span>{item.label}</span>
                  {item.meta && <span className="meta">{item.meta}</span>}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}