import { useState } from 'react'
import { Alert, App, Button, Card, Result, Space, Spin, Typography } from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { DiffEditor, loader } from '@monaco-editor/react'
import { api, ApiError } from '../../lib/api'
import { monaco } from '../../lib/monaco'
import { useResourceDetail } from './ResourceDetailLayout'
import { clearYamlDraft, loadYamlDraft, yamlDraftKey } from './yamlDraft'

loader.config({ monaco })

export default function ResourceDiffPage() {
  const { cluster, type, namespace, name, updateCan } = useResourceDetail()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const draftKey = yamlDraftKey(cluster, type.gvr, namespace, name)
  const [draft] = useState(() => loadYamlDraft(draftKey))
  const [busy, setBusy] = useState<'dryrun' | 'submit'>()
  const [conflict, setConflict] = useState(false)

  const yamlQuery = useQuery({
    queryKey: ['yaml', cluster, type.gvr.group, type.gvr.version, type.gvr.resource, namespace, name],
    queryFn: ({ signal }) => api.yaml({ cluster, gvr: type.gvr, namespace, name, signal }),
  })

  const diffQuery = useQuery({
    queryKey: ['yaml-diff', draftKey, draft?.savedAt],
    queryFn: () => api.diff({ cluster, gvr: type.gvr, namespace, name, yaml: draft!.code }),
    enabled: !!draft,
    retry: false,
  })

  if (!draft) {
    return (
      <Result
        status="info"
        title="没有待比较的修改"
        subTitle="请先在 YAML 编辑页修改内容，再点击“比较变更”。"
        extra={
          <Link to="../yaml">
            <Button type="primary">去编辑 YAML</Button>
          </Link>
        }
      />
    )
  }

  const submit = async (dryRun: boolean) => {
    setBusy(dryRun ? 'dryrun' : 'submit')
    setConflict(false)
    try {
      if (dryRun) {
        await api.updateYAML({ cluster, gvr: type.gvr, namespace, name, yaml: draft.code, dryRun: true })
        message.success('服务端校验通过（dryRun）')
        return
      }
      // 提交前强制再执行一次 dryRun，避免绕过比较页的校验。
      await api.updateYAML({ cluster, gvr: type.gvr, namespace, name, yaml: draft.code, dryRun: true })
      await api.updateYAML({ cluster, gvr: type.gvr, namespace, name, yaml: draft.code })
      clearYamlDraft(draftKey)
      await queryClient.invalidateQueries({ queryKey: ['yaml', cluster, type.gvr.group, type.gvr.version, type.gvr.resource, namespace, name] })
      await queryClient.invalidateQueries({ queryKey: ['detail', cluster, type.key, type.gvr.group, type.gvr.version, namespace, name] })
      await queryClient.invalidateQueries({ queryKey: ['resources'] })
      message.success('提交成功')
      navigate('..')
    } catch (error) {
      if (error instanceof ApiError && error.code === 'CONFLICT') {
        setConflict(true)
        message.error('对象已被他人修改，请刷新后重试')
      } else if (error instanceof ApiError && error.code === 'UNPROCESSABLE') {
        message.error(error.message)
      } else {
        message.error(error instanceof Error ? error.message : '提交失败')
      }
    } finally {
      setBusy(undefined)
    }
  }

  const diff = diffQuery.data
  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {conflict && (
        <Alert
          type="error"
          showIcon
          message="资源版本冲突"
          description="对象已被修改。请刷新原对象后返回编辑页，避免覆盖他人变更。"
          action={
            <Button size="small" onClick={() => void yamlQuery.refetch()}>
              刷新原对象
            </Button>
          }
        />
      )}
      {draft.code && !updateCan.loading && !updateCan.allowed && (
        <Alert type="info" showIcon message="当前账号只有只读权限，无法提交变更。" />
      )}
      {diff?.immutableFields && diff.immutableFields.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message="不可变字段提示"
          description={
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {diff.immutableFields.map((warning) => (
                <li key={warning}>{warning}</li>
              ))}
            </ul>
          }
        />
      )}

      <Card
        size="small"
        title="变更比较"
        extra={<span className="kd-muted" style={{ fontSize: 11 }}>左：当前对象 · 右：待提交草稿</span>}
      >
        {diffQuery.isLoading || yamlQuery.isLoading ? (
          <div style={{ display: 'grid', placeItems: 'center', minHeight: 360 }}>
            <Spin />
          </div>
        ) : diffQuery.error ? (
          <Alert
            type="error"
            showIcon
            message="无法生成 Diff"
            description={diffQuery.error instanceof Error ? diffQuery.error.message : '请稍后重试'}
            action={
              <Button size="small" onClick={() => void diffQuery.refetch()}>
                重试
              </Button>
            }
          />
        ) : (
          <DiffEditor
            height="460px"
            language="yaml"
            original={yamlQuery.data ?? ''}
            modified={draft.code}
            options={{ readOnly: true, minimap: { enabled: false }, fontSize: 12 }}
          />
        )}
      </Card>

      <Space wrap>
        <Link to="../yaml">
          <Button>返回编辑</Button>
        </Link>
        <Button
          loading={busy === 'dryrun'}
          disabled={!diffQuery.data || !updateCan.allowed}
          onClick={() => void submit(true)}
        >
          服务端校验
        </Button>
        <Button
          type="primary"
          loading={busy === 'submit'}
          disabled={!diffQuery.data || !updateCan.allowed || diffQuery.isFetching}
          onClick={() => void submit(false)}
        >
          提交变更
        </Button>
        <Typography.Text type="secondary">
          提交会携带原对象的 metadata.resourceVersion；冲突时不会覆盖。
        </Typography.Text>
      </Space>
    </Space>
  )
}