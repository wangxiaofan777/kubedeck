import { useEffect, useState } from 'react'
import { Alert, Button, Modal, Space, Typography, message } from 'antd'
import { parse } from 'yaml'
import { api, ApiError, type ResourceItem } from '../../lib/api'
import YamlCodeEditor from './YamlCodeEditor'
import { createTemplate, type ResourceType } from './resourceTypes'

interface Props {
  open: boolean
  cluster: string
  type: ResourceType
  namespace: string
  onCancel: () => void
  onCreated: (item: ResourceItem) => void
}

/** ResourceCreateModal 提供「模板 → 本地校验 → 服务端 dryRun → 创建」闭环。 */
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
  const [busy, setBusy] = useState<'validate' | 'create'>()

  useEffect(() => {
    if (!open) return
    setCode(createTemplate(type, namespace))
    setParseError(undefined)
    setBusy(undefined)
  }, [open, type, namespace])

  const validate = (value: string): boolean => {
    try {
      parse(value)
      return true
    } catch (error) {
      setParseError(error instanceof Error ? error.message : 'YAML 解析失败')
      return false
    }
  }

  const submit = async (dryRun: boolean) => {
    if (!validate(code)) return
    setBusy(dryRun ? 'validate' : 'create')
    try {
      const created = await api.create({
        cluster,
        gvr: type.gvr,
        namespace: type.namespaced ? namespace : undefined,
        yaml: code,
        dryRun,
      })
      if (dryRun) {
        message.success('服务端校验通过（dryRun）')
      } else {
        message.success(`已创建 ${type.kind}/${created.metadata.name}`)
        onCreated(created)
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
            将创建到命名空间 <Typography.Text code>{namespace}</Typography.Text>
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">集群级资源，不需要命名空间。</Typography.Text>
        )}
        <Alert
          type="info"
          showIcon
          message="模板只预填 apiVersion、kind 与 metadata，提交前请补全 spec"
        />
        {parseError && (
          <Alert type="error" showIcon message={`YAML 语法错误：${parseError}`} />
        )}
        <YamlCodeEditor
          value={code}
          onChange={(value) => {
            setCode(value)
            setParseError(undefined)
          }}
          height={360}
        />
      </Space>
    </Modal>
  )
}