import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Button, Space, Spin, Typography, message } from 'antd'
import Editor, { DiffEditor, loader } from '@monaco-editor/react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { editor as monacoEditor } from 'monaco-editor'
import { parse } from 'yaml'
import { api, ApiError, type GVR } from '../../lib/api'
import { monaco } from '../../lib/monaco'

loader.config({ monaco })

interface Props {
  cluster: string
  gvr: GVR
  namespace?: string
  name: string
  canUpdate: boolean
  onSaved: () => void
}

/** YamlEditor 提供 Monaco 编辑、服务端 Diff、不可变字段提示与冲突处理。 */
export default function YamlEditor({ cluster, gvr, namespace, name, canUpdate, onSaved }: Props) {
  const queryClient = useQueryClient()
  const yamlQuery = useQuery({
    queryKey: ['yaml', cluster, gvr.group, gvr.version, gvr.resource, namespace, name],
    queryFn: ({ signal }) => api.yaml({ cluster, gvr, namespace, name, signal }),
  })

  const [code, setCode] = useState('')
  const [preview, setPreview] = useState<{ code: string; diff: string; warnings: string[] }>()
  const [parseError, setParseError] = useState<string>()
  const [conflict, setConflict] = useState(false)
  const [busy, setBusy] = useState<'diff' | 'dryrun' | 'submit'>()
  const [hostReady, setHostReady] = useState(false)

  const editorWrapperRef = useRef<HTMLDivElement | null>(null)
  const editorRef = useRef<monacoEditor.IStandaloneCodeEditor>()
  const nodeObserverRef = useRef<ResizeObserver>()
  const wrapperObserverRef = useRef<ResizeObserver>()

  // Ant Tabs 切换或受控 value 更新时，Monaco 偶尔会按 0 尺寸布局并塌缩成 5×5。
  // 只要外层容器仍有宽度，就主动重新布局，保证后续输入可见。
  const ensureLayout = useCallback(() => {
    const instance = editorRef.current
    const wrapper = editorWrapperRef.current
    if (!instance || !wrapper || wrapper.clientWidth === 0) return
    const node = instance.getDomNode()
    if (node && node.clientWidth <= 10) instance.layout()
  }, [])

  const scheduleLayout = useCallback(() => {
    window.setTimeout(ensureLayout, 0)
  }, [ensureLayout])

  const attachWrapper = useCallback(
    (node: HTMLDivElement | null) => {
      wrapperObserverRef.current?.disconnect()
      editorWrapperRef.current = node
      if (!node) return
      if (node.clientWidth > 0) setHostReady(true)
      const observer = new ResizeObserver(() => {
        if (node.clientWidth > 0) setHostReady(true)
        scheduleLayout()
      })
      observer.observe(node)
      wrapperObserverRef.current = observer
    },
    [scheduleLayout],
  )

  useEffect(() => {
    const timer = window.setTimeout(ensureLayout, 0)
    return () => window.clearTimeout(timer)
  }, [code, ensureLayout])

  useEffect(() => () => nodeObserverRef.current?.disconnect(), [])

  // 标签页切换动画期间容器尺寸会短暂为 0，Monaco 可能先按 5×5 完成布局，
  // 且不再收到后续尺寸变化。挂载后的一小段时间内持续校正，等动画稳定即可。
  useEffect(() => {
    const started = Date.now()
    const timer = window.setInterval(() => {
      ensureLayout()
      if (Date.now() - started > 2000) window.clearInterval(timer)
    }, 100)
    return () => window.clearInterval(timer)
  }, [ensureLayout])

  useEffect(() => {
    if (yamlQuery.data !== undefined) {
      setCode(yamlQuery.data)
      setPreview(undefined)
      setConflict(false)
    }
  }, [yamlQuery.data])

  const validate = (value: string) => {
    try {
      parse(value)
      setParseError(undefined)
      return true
    } catch (error) {
      setParseError(error instanceof Error ? error.message : 'YAML 解析失败')
      return false
    }
  }

  const runDiff = async () => {
    if (!validate(code)) return
    setBusy('diff')
    try {
      const result = await api.diff({ cluster, gvr, namespace, name, yaml: code })
      setPreview({
        code,
        diff: result.diff || '没有差异',
        warnings: result.immutableFields ?? [],
      })
      setConflict(false)
    } catch (error) {
      if (error instanceof ApiError && error.code === 'CONFLICT') {
        setConflict(true)
      }
      message.error(error instanceof Error ? error.message : 'Diff 失败')
    } finally {
      setBusy(undefined)
    }
  }

  const submit = async (dryRun: boolean) => {
    if (!validate(code)) return
    if (!dryRun && preview?.code !== code) {
      message.warning('请先预览变更，确认 Diff 后再提交')
      return
    }
    setBusy(dryRun ? 'dryrun' : 'submit')
    try {
      await api.updateYAML({ cluster, gvr, namespace, name, yaml: code, dryRun })
      if (dryRun) {
        message.success('服务端校验通过（dryRun）')
      } else {
        message.success('提交成功')
        setPreview(undefined)
        await queryClient.invalidateQueries({
          queryKey: ['yaml', cluster, gvr.group, gvr.version, gvr.resource, namespace, name],
        })
        await queryClient.invalidateQueries({ queryKey: ['detail'] })
        onSaved()
      }
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

  if (yamlQuery.isLoading) return <Spin />
  if (yamlQuery.error) {
    return (
      <Alert type="error" showIcon message="YAML 加载失败" description={String(yamlQuery.error)} />
    )
  }

  return (
    <div>
      {!canUpdate && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="当前账号对资源只有只读权限"
        />
      )}
      {parseError && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message={`YAML 语法错误：${parseError}`}
        />
      )}
      {conflict && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message="资源版本冲突"
          description="对象已被修改。请刷新页面获取最新内容，避免覆盖他人变更。"
          action={
            <Button size="small" onClick={() => void yamlQuery.refetch()}>
              刷新
            </Button>
          }
        />
      )}

      <div ref={attachWrapper}>
        {hostReady ? (
          <Editor
            height="420px"
            language="yaml"
            value={code}
            onMount={(instance) => {
              editorRef.current = instance
              scheduleLayout()
              const node = instance.getDomNode()
              if (node) {
                nodeObserverRef.current?.disconnect()
                const observer = new ResizeObserver(() => {
                  scheduleLayout()
                })
                observer.observe(node)
                nodeObserverRef.current = observer
              }
            }}
            onChange={(value) => {
              setCode(value ?? '')
              setPreview(undefined)
              setConflict(false)
            }}
            options={{
              readOnly: !canUpdate,
              minimap: { enabled: false },
              fontSize: 13,
              scrollBeyondLastLine: false,
              automaticLayout: true,
              tabSize: 2,
            }}
          />
        ) : (
          <div
            style={{ height: 420, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
          >
            <Spin />
          </div>
        )}
      </div>

      <Space style={{ marginTop: 12 }} wrap>
        <Button loading={busy === 'diff'} onClick={() => void runDiff()} disabled={!canUpdate}>
          预览变更
        </Button>
        <Button
          loading={busy === 'dryrun'}
          onClick={() => void submit(true)}
          disabled={!canUpdate || !!parseError}
        >
          服务端校验
        </Button>
        <Button
          type="primary"
          loading={busy === 'submit'}
          onClick={() => void submit(false)}
          disabled={!canUpdate || !!parseError || preview?.code !== code}
        >
          提交
        </Button>
        <Typography.Text type="secondary">
          提交时必须携带最新的 metadata.resourceVersion
        </Typography.Text>
      </Space>

      {preview && (
        <div style={{ marginTop: 16 }}>
          {preview.warnings.length > 0 && (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 12 }}
              message="不可变字段提示"
              description={
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {preview.warnings.map((warning) => (
                    <li key={warning}>{warning}</li>
                  ))}
                </ul>
              }
            />
          )}
          <Typography.Title level={5}>变更预览</Typography.Title>
          <DiffEditor
            height="320px"
            language="yaml"
            original={yamlQuery.data}
            modified={preview.code}
            options={{ readOnly: true, minimap: { enabled: false }, fontSize: 12 }}
          />
        </div>
      )}
    </div>
  )
}
