import { useMemo, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Descriptions,
  Drawer,
  Input,
  Popconfirm,
  Result,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
} from 'antd'
import { DeleteOutlined, KeyOutlined, ReloadOutlined, SaveOutlined, UserAddOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type RoleBinding } from '../../lib/api'
import { useAuth } from '../../app/AuthProvider'
import PageHeader from '../../components/PageHeader'
import StatusPill from '../../components/StatusPill'
import { ErrorState, PageSkeleton } from '../../components/StateViews'
import { formatDateTime } from '../../lib/format'
import CredentialInput, { credentialInput, emptyCredential } from './CredentialInput'

export default function ClusterSettingsPage() {
  const { cluster: name } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { me } = useAuth()
  const { message } = App.useApp()
  const [rotateOpen, setRotateOpen] = useState(false)
  const [credential, setCredential] = useState(emptyCredential)
  const [rotating, setRotating] = useState(false)
  const [displayName, setDisplayName] = useState('')
  const [savingMeta, setSavingMeta] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState('')
  const [deleting, setDeleting] = useState(false)

  const [newUser, setNewUser] = useState<number>()
  const [newRole, setNewRole] = useState<string>('developer')
  const [newNamespace, setNewNamespace] = useState('*')
  const [userQuery, setUserQuery] = useState('')
  const [granting, setGranting] = useState(false)

  const detail = useQuery({
    queryKey: ['cluster-settings', name],
    queryFn: ({ signal }) => api.clusterDetail(name as string, signal),
    enabled: !!name,
  })
  const bindings = useQuery({
    queryKey: ['admin-bindings'],
    queryFn: ({ signal }) => api.adminRoleBindings(signal),
    enabled: !!me?.user.isPlatformAdmin,
  })
  const adminClusters = useQuery({
    queryKey: ['admin-clusters'],
    queryFn: ({ signal }) => api.adminClusters(signal),
    enabled: !!me?.user.isPlatformAdmin,
  })
  const users = useQuery({
    queryKey: ['admin-users', userQuery],
    queryFn: ({ signal }) => api.adminUsers(userQuery, signal),
    enabled: !!me?.user.isPlatformAdmin,
  })
  const roles = useQuery({
    queryKey: ['admin-roles'],
    queryFn: ({ signal }) => api.adminRoles(signal),
    enabled: !!me?.user.isPlatformAdmin,
  })

  const clusterId = adminClusters.data?.clusters.find((item) => item.name === name)?.id
  const accessError = bindings.error ?? roles.error ?? users.error ?? adminClusters.error
  const clusterBindings = useMemo(
    () => (bindings.data?.bindings ?? []).filter((binding) => binding.clusterName === name),
    [bindings.data, name],
  )
  const userMap = useMemo(
    () => new Map((users.data?.users ?? []).map((user) => [user.id, user])),
    [users.data],
  )

  if (!me?.user.isPlatformAdmin) {
    return <Result status="403" title="需要平台管理员权限" subTitle="集群设置仅对平台管理员开放。" />
  }
  if (detail.isLoading) return <PageSkeleton rows={8} />
  if (detail.isError) return <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />

  const health = detail.data?.health
  const credentialMeta = detail.data?.credential

  const rotate = async () => {
    if (!name) return
    setRotating(true)
    try {
      await api.rotateCredential(name, credentialInput(credential))
      message.success('凭据已轮换')
      setRotateOpen(false)
      setCredential(emptyCredential())
      await Promise.all([
        detail.refetch(),
        queryClient.invalidateQueries({ queryKey: ['clusters'] }),
      ])
    } catch (error) {
      message.error(error instanceof Error ? error.message : '轮换失败')
    } finally {
      setRotating(false)
    }
  }

  const saveMeta = async () => {
    if (!name) return
    setSavingMeta(true)
    try {
      await api.patchCluster(name, { displayName: displayName || name })
      message.success('已更新集群信息')
      await Promise.all([
        detail.refetch(),
        queryClient.invalidateQueries({ queryKey: ['clusters'] }),
      ])
    } catch (error) {
      message.error(error instanceof Error ? error.message : '更新失败')
    } finally {
      setSavingMeta(false)
    }
  }

  const grant = async () => {
    if (!clusterId || !newUser) {
      message.error('请选择用户')
      return
    }
    setGranting(true)
    try {
      const existing = clusterBindings
        .filter((binding) => binding.userId === newUser)
        .map((binding) => ({ namespace: binding.namespace, roleCode: binding.roleCode }))
      await api.putUserAccess({
        userId: newUser,
        clusterId,
        bindings: [...existing, { namespace: newNamespace.trim() || '*', roleCode: newRole }],
      })
      message.success('访问绑定已更新，60 秒内生效')
      await bindings.refetch()
      setNewUser(undefined)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    } finally {
      setGranting(false)
    }
  }

  const revoke = async (binding: RoleBinding) => {
    try {
      await api.deleteRoleBinding(binding.id)
      message.success('已回收绑定')
      await bindings.refetch()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '回收失败')
    }
  }

  const remove = async () => {
    if (!name || deleteConfirm !== name) return
    setDeleting(true)
    try {
      await api.deleteCluster(name, deleteConfirm)
      message.success('集群已移除')
      navigate('/clusters')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '移除失败')
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群管理', to: '/clusters' },
          { label: name ?? '' },
          { label: '设置' },
        ]}
        title={`${name} 设置`}
        tags={<StatusPill value={health?.status ?? 'unknown'} />}
        subtitle={<span className="kd-muted">{health?.server}</span>}
        actions={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void detail.refetch()}>
              刷新
            </Button>
            <Button
              icon={<KeyOutlined />}
              onClick={() => {
                setCredential(emptyCredential())
                setRotateOpen(true)
              }}
            >
              轮换凭据
            </Button>
          </Space>
        }
      />

      <Tabs
        items={[
          {
            key: 'overview',
            label: '概览',
            children: (
              <Space direction="vertical" size={14} style={{ width: '100%' }}>
                <Card title="健康状态" size="small">
                  <Descriptions column={2} size="small" bordered>
                    <Descriptions.Item label="状态">
                      <StatusPill value={health?.status ?? 'unknown'} />
                    </Descriptions.Item>
                    <Descriptions.Item label="版本">{health?.version || 'unknown'}</Descriptions.Item>
                    <Descriptions.Item label="Server" span={2}>
                      <span className="kd-mono">{health?.server}</span>
                    </Descriptions.Item>
                    <Descriptions.Item label="消息" span={2}>
                      {health?.message || '-'}
                    </Descriptions.Item>
                  </Descriptions>
                </Card>
                <Card title="凭据元数据" size="small">
                  {credentialMeta ? (
                    <Descriptions column={2} size="small" bordered>
                      <Descriptions.Item label="类型">{credentialMeta.authType}</Descriptions.Item>
                      <Descriptions.Item label="密钥版本">{credentialMeta.keyVersion}</Descriptions.Item>
                      <Descriptions.Item label="指纹" span={2}>
                        <span className="kd-mono">{credentialMeta.fingerprint}</span>
                      </Descriptions.Item>
                      <Descriptions.Item label="轮换时间">
                        {formatDateTime(credentialMeta.rotatedAt)}
                      </Descriptions.Item>
                      <Descriptions.Item label="过期时间">
                        {credentialMeta.expiresAt ? formatDateTime(credentialMeta.expiresAt) : '未设置'}
                      </Descriptions.Item>
                    </Descriptions>
                  ) : (
                    <Alert type="info" showIcon message="没有可展示的凭据元数据" />
                  )}
                </Card>
                <Card title="展示信息" size="small">
                  <Space>
                    <Input
                      value={displayName || health?.name || ''}
                      onChange={(event) => setDisplayName(event.target.value)}
                      placeholder="展示名"
                      style={{ width: 260 }}
                    />
                    <Button icon={<SaveOutlined />} loading={savingMeta} onClick={() => void saveMeta()}>
                      保存
                    </Button>
                  </Space>
                </Card>
              </Space>
            ),
          },
          {
            key: 'access',
            label: '访问控制',
            children: (
              <Space direction="vertical" size={14} style={{ width: '100%' }}>
                {(bindings.isError || roles.isError || users.isError || adminClusters.isError) && (
                  <Alert
                    type="warning"
                    showIcon
                    message="平台权限数据不可用"
                    description={
                      accessError instanceof Error
                        ? accessError.message
                        : '请确认平台数据库已配置后重试'
                    }
                    action={
                      <Button
                        size="small"
                        onClick={() => {
                          void bindings.refetch()
                          void roles.refetch()
                          void users.refetch()
                          void adminClusters.refetch()
                        }}
                      >
                        重试
                      </Button>
                    }
                  />
                )}
                <Card title="授予访问" size="small">
                  <Space wrap>
                    <Select
                      showSearch
                      value={newUser}
                      placeholder="搜索用户"
                      style={{ width: 260 }}
                      filterOption={false}
                      onSearch={setUserQuery}
                      onChange={setNewUser}
                      options={(users.data?.users ?? []).map((user) => ({
                        value: user.id,
                        label: `${user.displayName || user.email || user.subject} (${user.id})`,
                      }))}
                    />
                    <Select
                      value={newRole}
                      style={{ width: 160 }}
                      onChange={setNewRole}
                      options={(roles.data?.roles ?? []).map((role) => ({
                        value: role.code,
                        label: role.name,
                      }))}
                    />
                    <Input
                      value={newNamespace}
                      onChange={(event) => setNewNamespace(event.target.value)}
                      placeholder="命名空间，* 表示全部"
                      style={{ width: 180 }}
                    />
                    <Button
                      type="primary"
                      icon={<UserAddOutlined />}
                      loading={granting}
                      onClick={() => void grant()}
                    >
                      授予
                    </Button>
                  </Space>
                </Card>
                <Card title={`当前绑定（${clusterBindings.length}）`} size="small">
                  <Table<RoleBinding>
                    size="small"
                    rowKey="id"
                    dataSource={clusterBindings}
                    pagination={false}
                    columns={[
                      {
                        title: '用户',
                        key: 'user',
                        render: (_, binding) => {
                          const user = userMap.get(binding.userId)
                          return user ? (
                            <span>
                              {user.displayName || user.email}{' '}
                              <span className="kd-muted">#{binding.userId}</span>
                            </span>
                          ) : (
                            `#${binding.userId}`
                          )
                        },
                      },
                      {
                        title: '命名空间',
                        dataIndex: 'namespace',
                        width: 140,
                        render: (value: string) => (value === '*' ? <Tag>全部</Tag> : value),
                      },
                      {
                        title: '角色',
                        dataIndex: 'roleCode',
                        width: 130,
                        render: (value: string) => <Tag color="blue">{value}</Tag>,
                      },
                      {
                        title: '',
                        key: 'actions',
                        width: 90,
                        render: (_, binding) => (
                          <Popconfirm
                            title="回收该绑定？"
                            description="该用户将失去对应范围的访问。"
                            onConfirm={() => void revoke(binding)}
                          >
                            <Button size="small" danger type="text">
                              回收
                            </Button>
                          </Popconfirm>
                        ),
                      },
                    ]}
                  />
                  {clusterBindings.length === 0 && (
                    <div className="kd-muted" style={{ padding: 16 }}>
                      还没有用户绑定该集群
                    </div>
                  )}
                </Card>
              </Space>
            ),
          },
          {
            key: 'danger',
            label: '危险操作',
            children: (
              <Card title="移除集群" size="small" style={{ borderColor: '#fecaca' }}>
                <Alert
                  type="error"
                  showIcon
                  style={{ marginBottom: 14 }}
                  message="只会删除平台登记，不会修改目标集群中的任何资源"
                  description="相关审计记录会保留；健康探测会立即停止。"
                />
                <Space>
                  <Input
                    value={deleteConfirm}
                    onChange={(event) => setDeleteConfirm(event.target.value)}
                    placeholder={`输入集群名 ${name} 确认`}
                    style={{ width: 260 }}
                  />
                  <Button
                    danger
                    type="primary"
                    icon={<DeleteOutlined />}
                    loading={deleting}
                    disabled={deleteConfirm !== name}
                    onClick={() => void remove()}
                  >
                    移除
                  </Button>
                </Space>
              </Card>
            ),
          },
        ]}
      />

      <Drawer
        title="轮换凭据"
        width={680}
        open={rotateOpen}
        onClose={() => setRotateOpen(false)}
        extra={
          <Button type="primary" loading={rotating} onClick={() => void rotate()}>
            提交
          </Button>
        }
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
          message="新凭据必须能连通目标集群"
          description="探测失败时平台会保留旧凭据，不会覆盖。"
        />
        <CredentialInput value={credential} onChange={setCredential} />
      </Drawer>
    </div>
  )
}