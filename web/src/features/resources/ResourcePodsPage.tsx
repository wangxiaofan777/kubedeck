import { Button, Table } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, type ResourceItem } from '../../lib/api'
import { ErrorState } from '../../components/StateViews'
import StatusPill from '../../components/StatusPill'
import { ageOf } from '../../lib/format'
import { useResourceDetail } from './ResourceDetailLayout'

export default function ResourcePodsPage() {
  const { cluster, namespace, selector } = useResourceDetail()
  const navigate = useNavigate()
  const relatedQuery = useQuery({
    queryKey: ['related-pods', cluster, namespace, selector],
    queryFn: ({ signal }) =>
      api.list({
        cluster,
        gvr: { group: '', version: 'v1', resource: 'pods' },
        namespace,
        labelSelector: selector,
        limit: 200,
        signal,
      }),
    enabled: !!cluster && !!selector,
  })

  if (relatedQuery.isError) {
    return (
      <ErrorState
        error={relatedQuery.error}
        title="无法加载关联 Pod"
        onRetry={() => void relatedQuery.refetch()}
      />
    )
  }

  return (
    <Table<ResourceItem>
      size="small"
      rowKey={(pod) => pod.metadata.uid ?? `${pod.metadata.namespace}/${pod.metadata.name}`}
      loading={relatedQuery.isFetching}
      dataSource={relatedQuery.data?.items ?? []}
      pagination={false}
      locale={{
        emptyText: (
          <div style={{ padding: 24 }}>
            <div className="kd-muted">没有匹配该选择器的 Pod。</div>
            <Button size="small" style={{ marginTop: 8 }} onClick={() => void relatedQuery.refetch()}>
              刷新
            </Button>
          </div>
        ),
      }}
      columns={[
        {
          title: '名称',
          dataIndex: ['metadata', 'name'],
          ellipsis: true,
          render: (value: string, pod) => (
            <span
              className="kd-link kd-mono"
              onClick={() =>
                navigate(
                  `/clusters/${encodeURIComponent(cluster)}/pods/${encodeURIComponent(
                    pod.metadata.namespace ?? '',
                  )}/${encodeURIComponent(value)}`,
                )
              }
            >
              {value}
            </span>
          ),
        },
        {
          title: '状态',
          key: 'status',
          width: 170,
          render: (_, pod) => (
            <StatusPill value={typeof pod.status?.phase === 'string' ? pod.status.phase : 'Unknown'} />
          ),
        },
        {
          title: '节点',
          key: 'node',
          width: 160,
          render: (_, pod) =>
            typeof pod.spec?.nodeName === 'string' ? (
              <span
                className="kd-link kd-mono"
                onClick={() =>
                  navigate(
                    `/clusters/${encodeURIComponent(cluster)}/nodes/${encodeURIComponent(
                      String(pod.spec?.nodeName),
                    )}`,
                  )
                }
              >
                {pod.spec.nodeName}
              </span>
            ) : (
              '-'
            ),
        },
        {
          title: 'Age',
          key: 'age',
          width: 80,
          render: (_, pod) => ageOf(pod.metadata.creationTimestamp),
        },
      ]}
    />
  )
}