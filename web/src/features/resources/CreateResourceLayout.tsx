import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, App, Button, Result, Steps, Typography } from 'antd'
import {
  Outlet,
  useLocation,
  useNavigate,
  useOutletContext,
  useParams,
  useSearchParams,
} from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { parse, stringify } from 'yaml'
import PageHeader from '../../components/PageHeader'
import { api, type GVR, type ResourceItem } from '../../lib/api'
import { useCan } from '../auth/useCan'
import {
  buildManifest,
  emptyValues,
  FORM_KINDS,
  valuesFromResource,
  type FormKind,
  type FormValues,
} from './createManifest'
import { clearCreateDraft, createDraftKey, loadCreateDraft, saveCreateDraft } from './createDraft'
import { resolveType, type ResourceType } from './resourceTypes'
import { useApiResources } from './useApiResources'

const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/

/** skeletonFor 为尚未提供表单的资源类型生成可提交的最小 YAML。 */
function skeletonFor(type: ResourceType, namespace: string): string {
  const apiVersion = type.gvr.group ? `${type.gvr.group}/${type.gvr.version}` : type.gvr.version
  const metadata = {
    name: `example-${type.key.replace(/s$/, '')}`,
    ...(type.namespaced ? { namespace: namespace || 'default' } : {}),
  }
  const labels = { app: metadata.name }
  const podTemplate = {
    metadata: { labels },
    spec: {
      ...(type.kind === 'Job' ? { restartPolicy: 'Never' } : {}),
      containers: [
        {
          name: 'main',
          image: type.kind === 'Job' ? 'busybox:1.36' : 'nginx:1.27',
          ...(type.kind === 'Job' ? { command: ['sh', '-c', 'echo hello'] } : {}),
        },
      ],
    },
  }
  let spec: Record<string, unknown> = {}
  if (type.kind === 'DaemonSet' || type.kind === 'Job') {
    spec = {
      ...(type.kind === 'DaemonSet' ? { selector: { matchLabels: labels } } : {}),
      template: podTemplate,
    }
  }
  return stringify({ apiVersion, kind: type.kind, metadata, spec })
}

function resolveTarget(
  resources: { group: string; version: string; resource: string; kind: string; verbs: string[] }[],
  apiVersion: string,
  kind: string,
): GVR | undefined {
  const [group, version] = apiVersion.includes('/') ? apiVersion.split('/') : ['', apiVersion]
  const match = resources.find(
    (item) => item.group === group && item.version === version && item.kind === kind,
  )
  if (!match || !match.verbs.includes('create')) return undefined
  return { group: match.group, version: match.version, resource: match.resource }
}

export interface CreateResourceContextValue {
  cluster: string
  type: ResourceType
  kind: FormKind
  formSupported: boolean
  namespace: string
  copyFrom: string
  base: string
  values: FormValues
  set: (patch: Partial<FormValues>) => void
  setValues: React.Dispatch<React.SetStateAction<FormValues>>
  code: string
  setCode: (code: string) => void
  yamlDirty: boolean
  setYamlDirty: (dirty: boolean) => void
  manifest?: Record<string, unknown>
  preview: string
  createCan: ReturnType<typeof useCan>
  busy?: 'validate' | 'create'
  error?: unknown
  setError: (error: unknown) => void
  submit: (dryRun: boolean) => Promise<void>
  cancel: () => void
}

export function useCreateResource(): CreateResourceContextValue {
  return useOutletContext<CreateResourceContextValue>()
}

/** CreateResourceLayout 持有创建流程的共享状态，三个步骤页只负责各自的界面。 */
export default function CreateResourceLayout() {
  const { cluster, typeKey } = useParams()
  const [searchParams] = useSearchParams()
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const namespaceFromQuery = searchParams.get('namespace') ?? ''
  const copyFrom = searchParams.get('copy') ?? ''
  const type = resolveType(typeKey)
  const kind = (type?.key ?? '') as FormKind
  const formSupported = FORM_KINDS.includes(kind)
  const step = location.pathname.endsWith('/yaml')
    ? 1
    : location.pathname.endsWith('/review')
      ? 2
      : 0
  const base = `/clusters/${encodeURIComponent(cluster ?? '')}/new/${typeKey ?? ''}`

  const draftKey = createDraftKey(cluster ?? '', type?.key ?? '')
  // 只在首次挂载时读取草稿；步骤切换保留布局层状态。
  const initialDraft = useMemo(() => loadCreateDraft(draftKey), [draftKey])
  const [values, setValues] = useState<FormValues>(
    () => initialDraft?.values ?? emptyValues(namespaceFromQuery || 'default'),
  )
  const [code, setCode] = useState(() =>
    initialDraft?.code ?? (type && !formSupported ? skeletonFor(type, namespaceFromQuery) : ''),
  )
  const [yamlDirty, setYamlDirty] = useState(initialDraft?.yamlDirty ?? false)
  const [busy, setBusy] = useState<'validate' | 'create'>()
  const [error, setError] = useState<unknown>()
  // 步骤导航可能丢失 query（例如直接打开 /yaml）；以表单中的命名空间兜底。
  const namespace = namespaceFromQuery || values.namespace || ''
  const prefilled = useRef(false)

  const discovery = useApiResources(cluster)
  const createCan = useCan({
    cluster,
    gvr: type?.gvr ?? { group: '', version: 'v1', resource: 'pods' },
    namespace: namespace || undefined,
    verb: 'create',
    enabled: !!type,
  })

  const sourceQuery = useQuery({
    queryKey: ['copy-source', cluster, type?.key, copyFrom],
    queryFn: ({ signal }) => {
      const [ns, name] = copyFrom.split('/')
      return api.detail({
        cluster: cluster as string,
        gvr: type!.gvr,
        namespace: ns && ns !== '_' ? ns : undefined,
        name,
        signal,
      })
    },
    enabled: !!cluster && !!type && !!copyFrom,
  })

  useEffect(() => {
    if (!sourceQuery.data || prefilled.current || initialDraft) return
    prefilled.current = true
    setValues((current) => ({
      ...current,
      ...valuesFromResource(kind, sourceQuery.data.object, namespace || 'default'),
    }))
  }, [sourceQuery.data, kind, namespace, initialDraft])

  const manifest = useMemo(() => {
    if (!formSupported) return undefined
    return buildManifest(kind, values)
  }, [formSupported, kind, values])

  useEffect(() => {
    if (formSupported && manifest && !yamlDirty) setCode(stringify(manifest))
  }, [formSupported, manifest, yamlDirty])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      saveCreateDraft(draftKey, { values, code, yamlDirty })
    }, 200)
    return () => window.clearTimeout(timer)
  }, [draftKey, values, code, yamlDirty])

  const set = (patch: Partial<FormValues>) => setValues((current) => ({ ...current, ...patch }))

  const preview = code || (manifest ? stringify(manifest) : '')

  const validate = (): string | undefined => {
    if (yamlDirty || !formSupported) {
      try {
        const parsed = parse(code) as Record<string, unknown>
        if (!parsed || typeof parsed !== 'object') return 'YAML 必须是对象'
        if (!parsed.apiVersion || !parsed.kind) return 'YAML 缺少 apiVersion 或 kind'
        return undefined
      } catch (err) {
        return err instanceof Error ? `YAML 解析失败：${err.message}` : 'YAML 解析失败'
      }
    }
    if (!NAME_RE.test(values.name)) return '名称必须是小写字母、数字或连字符，且以字母数字开头结尾'
    if (kind !== 'namespaces' && !values.namespace) return '请填写命名空间'
    if (['deployments', 'statefulsets', 'cronjobs'].includes(kind) && !values.image.trim())
      return '镜像不能为空'
    if (kind === 'services' && !values.servicePort) return '服务端口不能为空'
    if (kind === 'ingresses' && !values.serviceName.trim()) return '后端 Service 名称不能为空'
    return undefined
  }

  const parseYamlTarget = (): { gvr: GVR; namespace?: string; text: string } | undefined => {
    const parsed = parse(code) as {
      apiVersion?: string
      kind?: string
      metadata?: { namespace?: string }
    }
    if (!parsed?.apiVersion || !parsed.kind) {
      setError(new Error('YAML 缺少 apiVersion 或 kind'))
      return undefined
    }
    const resolution = discovery.data
      ? resolveTarget(discovery.data.resources, parsed.apiVersion, parsed.kind)
      : undefined
    if (!resolution) {
      setError(new Error(`集群中未发现可创建的 ${parsed.apiVersion} / ${parsed.kind}`))
      return undefined
    }
    return {
      gvr: resolution,
      namespace: parsed.metadata?.namespace,
      text: code,
    }
  }

  const submit = async (dryRun: boolean) => {
    setError(undefined)
    const validation = validate()
    if (validation) {
      message.error(validation)
      return
    }
    let target: { gvr: GVR; namespace?: string; text: string }
    if (yamlDirty || !formSupported) {
      const parsed = parseYamlTarget()
      if (!parsed) return
      target = parsed
    } else {
      if (!manifest) {
        message.error('无法生成清单')
        return
      }
      target = {
        gvr: type!.gvr,
        namespace: kind === 'namespaces' ? undefined : values.namespace,
        text: stringify(manifest),
      }
    }
    setBusy(dryRun ? 'validate' : 'create')
    try {
      if (dryRun) {
        await api.create({
          cluster: cluster!,
          gvr: target.gvr,
          namespace: target.namespace,
          yaml: target.text,
          dryRun: true,
        })
        message.success('服务端校验通过')
      } else {
        await api.create({
          cluster: cluster!,
          gvr: target.gvr,
          namespace: target.namespace,
          yaml: target.text,
          dryRun: true,
        })
        const created: ResourceItem = await api.create({
          cluster: cluster!,
          gvr: target.gvr,
          namespace: target.namespace,
          yaml: target.text,
        })
        clearCreateDraft(draftKey)
        await queryClient.invalidateQueries({ queryKey: ['resources'] })
        message.success(`已创建 ${target.gvr.resource}/${created.metadata.name}`)
        const ns = created.metadata.namespace ?? '_'
        const suffix = created.metadata.namespace
          ? `?namespace=${encodeURIComponent(created.metadata.namespace)}`
          : ''
        const page = resolveType(target.gvr.resource)?.key ?? type!.key
        navigate(
          `/clusters/${encodeURIComponent(cluster!)}/${page}/${encodeURIComponent(ns)}/${encodeURIComponent(created.metadata.name)}${suffix}`,
        )
      }
    } catch (err) {
      setError(err)
    } finally {
      setBusy(undefined)
    }
  }

  const cancel = () => {
    clearCreateDraft(draftKey)
    navigate(
      `/clusters/${encodeURIComponent(cluster ?? '')}/${type?.key ?? ''}${
        namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
      }`,
    )
  }

  if (!cluster || !type) {
    return <Result status="404" title="资源类型不受支持" subTitle={`类型 "${typeKey}" 不在导航中。`} />
  }

  const context: CreateResourceContextValue = {
    cluster,
    type,
    kind,
    formSupported,
    namespace,
    copyFrom,
    base,
    values,
    set,
    setValues,
    code,
    setCode,
    yamlDirty,
    setYamlDirty,
    manifest,
    preview,
    createCan,
    busy,
    error,
    setError,
    submit,
    cancel,
  }

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `/clusters/${encodeURIComponent(cluster)}/overview` },
          {
            label: namespace || '全部命名空间',
            to: `/clusters/${encodeURIComponent(cluster)}/${type.key}${
              namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
            }`,
          },
          { label: `新建 ${type.label}` },
        ]}
        title={`新建 ${type.label}`}
        subtitle={<span className="kd-muted">配置与 YAML 分步编辑，确认页统一执行 dryRun 与创建。</span>}
        actions={<Button onClick={cancel}>取消</Button>}
      />

      <Steps
        current={step}
        size="small"
        style={{ margin: '4px 0 18px', maxWidth: 720 }}
        items={[
          { title: '配置', description: formSupported ? '表单填写' : '该类型仅支持 YAML' },
          { title: 'YAML', description: '高级编辑' },
          { title: '确认', description: 'dryRun 与创建' },
        ]}
      />

      {error ? (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 14 }}
          message="创建失败"
          description={error instanceof Error ? error.message : '请稍后重试'}
          action={
            <Button size="small" onClick={() => setError(undefined)}>
              关闭
            </Button>
          }
        />
      ) : null}

      <Outlet context={context} />

      <div className="kd-muted" style={{ marginTop: 14, fontSize: 11 }}>
        <Typography.Text type="secondary">
          草稿会自动保存在当前浏览器会话中，刷新页面后可继续编辑。
        </Typography.Text>
      </div>
    </div>
  )
}