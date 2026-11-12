import type { ReactNode } from 'react'
import { Badge, Button, Layout, Menu, Select, Space, Tag, Typography } from 'antd'
import type { MenuProps } from 'antd'
import { ClusterOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ClusterInfo } from '../lib/api'

const { Header, Sider, Content } = Layout
const { Title, Text } = Typography

interface Props {
  clusters: ClusterInfo[]
  cluster?: ClusterInfo
  onSelectCluster: (name: string) => void
  namespaces: string[]
  namespace: string
  onSelectNamespace: (namespace: string) => void
  namespaced: boolean
  menuItems: MenuProps['items']
  typeKey: string
  onSelectType: (key: string) => void
  onRefresh: () => void
  children: ReactNode
}

/** 控制台外壳：只负责布局与顶栏/侧栏交互，不持有业务数据。 */
export default function AppLayout({
  clusters,
  cluster,
  onSelectCluster,
  namespaces,
  namespace,
  onSelectNamespace,
  namespaced,
  menuItems,
  typeKey,
  onSelectType,
  onRefresh,
  children,
}: Props) {
  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          background: '#fff',
          borderBottom: '1px solid #f0f0f0',
          paddingInline: 20,
        }}
      >
        <ClusterOutlined style={{ fontSize: 20, color: '#1677ff' }} />
        <Title level={4} style={{ margin: 0 }}>
          KubeDeck
        </Title>
        <Text type="secondary">集群驾驶舱</Text>
        <div style={{ flex: 1 }} />
        {clusters.length > 1 && (
          <Select
            aria-label="切换集群"
            value={cluster?.name}
            onChange={onSelectCluster}
            style={{ width: 160 }}
            options={clusters.map((c) => ({ value: c.name, label: c.name }))}
          />
        )}
        {cluster && (
          <Space size={12}>
            <Badge
              status={cluster.status === 'healthy' ? 'success' : 'error'}
              text={cluster.name}
            />
            <Tag>{cluster.version || 'unknown'}</Tag>
          </Space>
        )}
        <Button icon={<ReloadOutlined />} onClick={onRefresh}>
          刷新
        </Button>
      </Header>

      <Layout>
        <Sider width={224} theme="light" style={{ borderRight: '1px solid #f0f0f0' }}>
          <div style={{ padding: '12px 16px' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              命名空间
            </Text>
            <Select
              aria-label="命名空间"
              style={{ width: '100%', marginTop: 6 }}
              value={namespace}
              onChange={onSelectNamespace}
              disabled={!namespaced}
              options={[
                { value: '', label: '全部命名空间' },
                ...namespaces.map((n) => ({ value: n, label: n })),
              ]}
            />
          </div>
          <nav aria-label="资源类型">
            <Menu
              mode="inline"
              selectedKeys={[typeKey]}
              items={menuItems}
              onClick={(e) => onSelectType(e.key)}
              style={{ borderInlineEnd: 0 }}
            />
          </nav>
        </Sider>

        <Content style={{ padding: 16, background: '#fff' }}>{children}</Content>
      </Layout>
    </Layout>
  )
}
