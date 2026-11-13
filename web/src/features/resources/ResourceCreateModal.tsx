import { useEffect, useState } from 'react'
import { Alert, Button, Modal, Space, Typography, message } from 'antd'
import { parse } from 'yaml'
import { api, ApiError, type ResourceItem } from '../../lib/api'
import YamlCodeEditor from './YamlCodeEditor'
import { useApiResources } from './useApiResources'
import {
  createTemplate,
  matchesResourceType,
  resolveCreateTarget,
  type CreateTarget,
  type ResourceType,
} from './resourceTypes'

interface Props {
  open: boolean
  cluster: string
  type: ResourceType
  namespace: string
  onCancel: () => void
  onCreated: (item: ResourceItem, pageKey?: string) => void
}

interface DocumentIdentity {
  apiVersion: string
  kind: string
  namespace?: string
}

function identityFrom(document: unknown): DocumentIdentity | undefined {
  if (!document || typeof document !== 'object' || Array.isArray(document)) return undefined
  const record = document as Record<string, unknown>
  const apiVersion = typeof record.apiVersion === 'string' ? record.apiVersion : ''
  const kind = typeof record.kind === 'string' ? record.kind : ''
  if (!apiVersion || !kind) return undefined
  const metadata = record.metadata
  const namespace =
    metadata && typeof metadata === 'object'
      ? (metadata as Record<string, unknown>).namespace
      : undefined
  return {
    apiVersion,
    kind,
    namespace: typeof namespace === 'string' ? namespace : undefined,
  }
}

function readIdentity(value: string): DocumentIdentity | undefined {
  try {
    return identityFrom(parse(value))
  } catch {
    return undefined
  }
}

/** ResourceCreateModal 提供「模板 → 本地校验 → 服务端 dryRun → 创建」闭环。
 * 创建目标以 YAML 的 apiVersion/kind 为准，不再固定为打开弹窗时的资源页类型。 */
export default function ResourceCreateModal({
  open,
  cluster,
  type,
  namespace,
  onCancel,
  onCreated,
}: Props) {
  const [code, setCode] = useState('')
  const [parseError, setParseError] = useState<string>()
  const [targetError, setTargetError] = useState<string>()
  const [targetHint, setTargetHint] = useState<string>()
  const [busy, setBusy] = useState<'validate' | 'create'>()
  const discovery = useApiResources(cluster)

  useEffect(() => {
    if (!open) return
    setCode(createTemplate(type, namespace))
    setParseError(undefined)
    setTargetError(undefined)
    setTargetHint(undefined)
    setBusy(undefined)
  }, [open, type, namespace])

  // 跨类型提示用防抖解析，避免每次按键都解析整段 YAML。
  useEffect(() => {
    if (!open) {
      setTargetHint(undefined)
      return
    }
    const timer = window.setTimeout(() => {
      const identity = readIdentity(code)
      if (!identity || matchesResourceType(identity.apiVersion, identity.kind, type)) {
        setTargetHint(undefined)
        return
      }
      const resolution = discovery.data
        ? resolveCreateTarget(discovery.data.resources, identity.apiVersion, identity.kind)
        : undefined
      if (resolution && !resolution.ok) {
        setTargetHint(`${resolution.reason}，提交会被阻止`)
        return
      }
      setTargetHint(
        `YAML 目标是 ${identity.kind}（${identity.apiVersion}），提交后将按该类型创建，不受当前 ${type.kind} 页面限制。`,
      )
    }, 300)
    return () => window.clearTimeout(timer)
  }, [code, open, type, discovery.data])

  const submit = async (dryRun: boolean) => {
    setParseError(undefined)
    setTargetError(undefined)

    let document: unknown
    try {
      document = parse(code)
    } catch (error) {
      setParseError(error instanceof Error ? error.message : 'YAML 解析失败')
      return
    }
    const identity = identityFrom(document)
    if (!identity) {
      setTargetError('YAML 缺少 apiVersion 或 kind，无法确定创建目标')
      return
    }

    let target: CreateTarget
    if (matchesResourceType(identity.apiVersion, identity.kind, type)) {
      target = { gvr: type.gvr, kind: type.kind, namespaced: type.namespaced, pageKey: type.key }
    } else {
      let resources = discovery.data?.resources
      if (!resources) {
        const refreshed = await discovery.refetch()
        resources = refreshed.data?.resources
      }
      if (!resources) {
        setTargetError('无法加载集群资源目录，请稍后重试')
        return
      }
      const resolution = resolveCreateTarget(resources, identity.apiVersion, identity.kind)
      if (!resolution.ok) {
        setTargetError(resolution.reason)
        return
      }
      target = resolution.target
    }

    if (!target.namespaced && identity.namespace) {
      setTargetError('集群级资源不能设置 metadata.namespace')
      return
    }
    const targetNamespace = target.namespaced ? identity.namespace || namespace : undefined
    if (target.namespaced && !targetNamespace) {
      setTargetError('命名空间级资源需要 metadata.namespace')
      return
    }

    setBusy(dryRun ? 'validate' : 'create')
    try {
      const created = await api.create({
        cluster,
        gvr: target.gvr,
        namespace: targetNamespace,
        yaml: code,
        dryRun,
      })
      if (dryRun) {
        message.success(`服务端校验通过（dryRun，${target.kind}）`)
      } else {
        message.success(`已创建 ${target.kind}/${created.metadata.name}`)
        onCreated(created, target.pageKey)
      }
    } catch (error) {
      if (error instanceof ApiError && error.code === 'CONFLICT') {
        message.error('同名对象已存在，请修改 metadata.name')
      } else {
        message.error(error instanceof Error ? error.message : '创建失败')
      }
    } finally {
      setBusy(undefined)
    }
  }

  return (
    <Modal
      open={open}
      title={`新建 ${type.kind}`}
      width={760}
      destroyOnClose
      onCancel={onCancel}
      maskClosable={!busy}
      footer={
        <Space>
          <Button onClick={onCancel} disabled={!!busy}>
            取消
          </Button>
          <Button loading={busy === 'validate'} onClick={() => void submit(true)}>
            服务端校验
          </Button>
          <Button type="primary" loading={busy === 'create'} onClick={() => void submit(false)}>
            创建
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        {type.namespaced ? (
          <Typography.Text type="secondary">
            当前资源页：{type.kind} · 命名空间 <Typography.Text code>{namespace}</Typography.Text>
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">
            当前资源页：{type.kind}（集群级资源，不需要命名空间）
          </Typography.Text>
        )}
        <Alert
          type="info"
          showIcon
          message="模板只预填 apiVersion、kind 与 metadata，提交前请补全 spec"
        />
        {targetHint && <Alert type="warning" showIcon message={targetHint} />}
        {targetError && <Alert type="error" showIcon message={targetError} />}
        {parseError && (
          <Alert type="error" showIcon message={`YAML 语法错误：${parseError}`} />
        )}
        <YamlCodeEditor
          value={code}
          onChange={(value) => {
            setCode(value)
            setParseError(undefined)
            setTargetError(undefined)
          }}
          height={360}
        />
      </Space>
    </Modal>
  )
}