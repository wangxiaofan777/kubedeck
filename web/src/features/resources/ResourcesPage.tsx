import { useMemo, useState } from 'react'
import { Empty, Spin } from 'antd'
import type { MenuProps } from 'antd'
import type { ResourceItem } from '../../lib/api'
import AppLayout from '../../app/AppLayout'
import ErrorAlert from '../../components/ErrorAlert'
import ResourceTable from '../../components/ResourceTable'
import DetailDrawer from '../../components/DetailDrawer'
import { useClusters } from '../clusters/useClusters'
import { useNamespaces } from '../namespaces/useNamespaces'
import { useResourceList } from './useResourceList'
import { RESOURCE_TYPES, type ResourceType } from './resourceTypes'

function buildMenuItems(): MenuProps['items'] {
  const groups = new Map<string, ResourceType[]>()
  for (const item of RESOURCE_TYPES) {
    const list = groups.get(item.group) ?? []
    list.push(item)
    groups.set(item.group, list)
  }
  return Array.from(groups, ([label, types]) => ({
    key: label,
    type: 'group' as const,
    label,
    children: types.map((t) => ({ key: t.key, label: t.label })),
  }))
}

/** 资源浏览页：持有集群/命名空间/资源类型上下文，组合查询与视图。 */
export default function ResourcesPage() {
  const [clusterName, setClusterName] = useState<string>()
  const [namespace, setNamespace] = useState('')
  const [typeKey, setTypeKey] = useState('pods')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<ResourceItem>()

  const clustersQuery = useClusters()
  const clusters = useMemo(() => clustersQuery.data ?? [], [clustersQuery.data])
  const cluster = useMemo(
    () => clusters.find((c) => c.name === clusterName) ?? clusters[0],
    [clusters, clusterName],
  )
  const type = RESOURCE_TYPES.find((t) => t.key === typeKey) ?? RESOURCE_TYPES[0]

  const namespacesQuery = useNamespaces(cluster?.name)
  const listQuery = useResourceList(cluster?.name, type, namespace)

  const items = useMemo(() => {
    const all = listQuery.data?.items ?? []
    const q = search.trim().toLowerCase()
    return q ? all.filter((i) => i.metadata.name.toLowerCase().includes(q)) : all
  }, [listQuery.data, search])

  const menuItems = useMemo(buildMenuItems, [])

  const selectCluster = (name: string) => {
    setClusterName(name)
    setNamespace('')
    setSelected(undefined)
  }
  const selectNamespace = (value: string) => {
    setNamespace(value)
    setSelected(undefined)
  }
  const selectType = (key: string) => {
    const next = RESOURCE_TYPES.find((t) => t.key === key)
    setTypeKey(key)
    setSearch('')
    setSelected(undefined)
    if (next && !next.namespaced) setNamespace('')
  }
  const refresh = () => {
    void clustersQuery.refetch()
    void namespacesQuery.refetch()
    void listQuery.refetch()
  }

  if (clustersQuery.isLoading) {
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  const error = clustersQuery.error ?? namespacesQuery.error ?? listQuery.error

  return (
    <>
      <AppLayout
        clusters={clusters}
        cluster={cluster}
        onSelectCluster={selectCluster}
        namespaces={namespacesQuery.data?.namespaces ?? []}
        namespace={namespace}
        onSelectNamespace={selectNamespace}
        namespaced={type.namespaced}
        menuItems={menuItems}
        typeKey={type.key}
        onSelectType={selectType}
        onRefresh={refresh}
      >
        <ErrorAlert error={error} />
        {!cluster ? (
          <Empty description="未注册任何集群：请用 KUBEDECK_DEV_KUBECONFIG 指定 kubeconfig 启动" />
        ) : (
          <ResourceTable
            type={type}
            items={items}
            loading={listQuery.isLoading}
            search={search}
            onSearch={setSearch}
            onSelect={setSelected}
            namespace={namespace}
          />
        )}
      </AppLayout>

      <DetailDrawer
        cluster={cluster?.name}
        type={type}
        item={selected}
        onClose={() => setSelected(undefined)}
      />
    </>
  )
}
