import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Divider,
  Input,
  InputNumber,
  Row,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from 'antd'
import { DeleteOutlined, PlusOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { parse, stringify } from 'yaml'
import { api, type GVR, type ResourceItem } from '../../lib/api'
import PageHeader from '../../components/PageHeader'
import { useCan } from '../auth/useCan'
import {
  buildManifest,
  emptyValues,
  FORM_KINDS,
  templatesFor,
  valuesFromResource,
  type FormKind,
  type FormValues,
  type KeyValue,
} from './createManifest'
import { resolveType, type ResourceType } from './resourceTypes'
import { useApiResources } from './useApiResources'

const YamlCodeEditor = lazy(() => import('./YamlCodeEditor'))

const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/

/** skeletonFor 为尚未提供表单的资源类型生成可提交的最小 YAML，避免空白编辑器。 */
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
    spec = { ...(type.kind === 'DaemonSet' ? { selector: { matchLabels: labels } } : {}), template: podTemplate }
  }
  return stringify({ apiVersion, kind: type.kind, metadata, spec })
}

function Field({ label, children, hint }: { label: string; children: React.ReactNode; hint?: string }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div style={{ fontSize: 12, color: '#475569', marginBottom: 5 }}>{label}</div>
      {children}
      {hint && <div className="kd-muted" style={{ fontSize: 11, marginTop: 4 }}>{hint}</div>}
    </div>
  )
}

function KvList({
  value,
  onChange,
  addLabel,
  secret,
}: {
  value: KeyValue[]
  onChange: (next: KeyValue[]) => void
  addLabel: string
  secret?: boolean
}) {
  const update = (index: number, patch: Partial<KeyValue>) => {
    onChange(value.map((item, i) => (i === index ? { ...item, ...patch } : item)))
  }
  return (
    <div>
      {value.map((item, index) => (
        <Space key={index} style={{ display: 'flex', marginBottom: 6 }}>
          <Input
            placeholder="键"
            value={item.key}
            onChange={(event) => update(index, { key: event.target.value })}
            style={{ width: 180 }}
          />
          {secret ? (
            <Input.Password
              placeholder="值（服务端自动 base64）"
              value={item.value}
              onChange={(event) => update(index, { value: event.target.value })}
              style={{ width: 240 }}
            />
          ) : (
            <Input
              placeholder="值"
              value={item.value}
              onChange={(event) => update(index, { value: event.target.value })}
              style={{ width: 240 }}
            />
          )}
          <Button
            type="text"
            danger
            icon={<DeleteOutlined />}
            onClick={() => onChange(value.filter((_, i) => i !== index))}
          />
        </Space>
      ))}
      <Button
        type="dashed"
        size="small"
        icon={<PlusOutlined />}
        onClick={() => onChange([...value, { key: '', value: '' }])}
      >
        {addLabel}
      </Button>
    </div>
  )
}

/** CreateResourcePage 是引导式创建：左侧表单、右侧实时 YAML 预览。 */
export default function CreateResourcePage() {
  const { cluster, typeKey } = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const namespace = searchParams.get('namespace') ?? ''
  const copyFrom = searchParams.get('copy') ?? ''
  const type = resolveType(typeKey)
  const kind = (type?.key ?? '') as FormKind
  const formSupported = FORM_KINDS.includes(kind)
  const [mode, setMode] = useState<'form' | 'yaml'>(formSupported ? 'form' : 'yaml')
  const [values, setValues] = useState<FormValues>(() => emptyValues(namespace || 'default'))
  const [code, setCode] = useState(() =>
    formSupported || !type ? '' : skeletonFor(type, namespace),
  )
  const [yamlDirty, setYamlDirty] = useState(false)
  const [busy, setBusy] = useState<'validate' | 'create'>()
  const [error, setError] = useState<unknown>()
  const prefilled = useRef(false)

  const discovery = useApiResources(cluster)
  const createCan = useCan({ cluster, gvr: type?.gvr ?? { group: '', version: 'v1', resource: 'pods' }, namespace: namespace || undefined, verb: 'create', enabled: !!type })

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
    if (!sourceQuery.data || prefilled.current) return
    prefilled.current = true
    setValues((current) => ({
      ...current,
      ...valuesFromResource(kind, sourceQuery.data.object, namespace || 'default'),
    }))
  }, [sourceQuery.data, kind, namespace])

  const manifest = useMemo(() => {
    if (!formSupported) return undefined
    return buildManifest(kind, values)
  }, [formSupported, kind, values])

  const preview = useMemo(() => {
    if (mode === 'yaml') return code
    if (!manifest) return ''
    return stringify(manifest)
  }, [mode, code, manifest])

  useEffect(() => {
    if (mode === 'form' && manifest && !yamlDirty) setCode(stringify(manifest))
  }, [mode, manifest, yamlDirty])

  if (!cluster || !type) {
    return <Alert type="error" showIcon message="资源类型不受支持" />
  }

  const set = (patch: Partial<FormValues>) => setValues((current) => ({ ...current, ...patch }))

  const validate = (): string | undefined => {
    if (mode === 'yaml') {
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

  const parseYaml = (): { gvr: GVR; namespace?: string; text: string } | undefined => {
    const parsed = parse(code) as {
      apiVersion?: string
      kind?: string
      metadata?: { namespace?: string }
    }
    if (!parsed?.apiVersion || !parsed.kind) return undefined
    const resolution = discovery.data
      ? resolveTarget(discovery.data.resources, parsed.apiVersion, parsed.kind)
      : undefined
    if (!resolution) {
      setError(new Error(`集群中未发现 ${parsed.apiVersion} / ${parsed.kind}`))
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
    if (mode === 'yaml') {
      const parsed = parseYaml()
      if (!parsed) return
      target = parsed
    } else {
      if (!manifest) {
        message.error('无法生成清单')
        return
      }
      target = {
        gvr: type.gvr,
        namespace: kind === 'namespaces' ? undefined : values.namespace,
        text: stringify(manifest),
      }
    }
    setBusy(dryRun ? 'validate' : 'create')
    try {
      if (dryRun) {
        await api.create({ cluster, gvr: target.gvr, namespace: target.namespace, yaml: target.text, dryRun: true })
        message.success('服务端校验通过')
      } else {
        await api.create({ cluster, gvr: target.gvr, namespace: target.namespace, yaml: target.text, dryRun: true })
        const created = await api.create({
          cluster,
          gvr: target.gvr,
          namespace: target.namespace,
          yaml: target.text,
        })
        await queryClient.invalidateQueries({ queryKey: ['resources'] })
        message.success(`已创建 ${target.gvr.resource}/${created.metadata.name}`)
        const ns = created.metadata.namespace ?? '_'
        const suffix = created.metadata.namespace
          ? `?namespace=${encodeURIComponent(created.metadata.namespace)}`
          : ''
        const page = resolveType(target.gvr.resource)?.key ?? type.key
        navigate(
          `/clusters/${encodeURIComponent(cluster)}/${page}/${encodeURIComponent(ns)}/${encodeURIComponent(created.metadata.name)}${suffix}`,
        )
      }
    } catch (err) {
      setError(err)
    } finally {
      setBusy(undefined)
    }
  }

  const templates = formSupported ? templatesFor(kind) : []

  return (
    <div>
      <PageHeader
        crumbs={[
          { label: '集群', to: `/clusters/${encodeURIComponent(cluster)}/overview` },
          { label: namespace || '全部命名空间', to: `/clusters/${encodeURIComponent(cluster)}/${type.key}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''}` },
          { label: `新建 ${type.label}` },
        ]}
        title={`新建 ${type.label}`}
        subtitle={
          <span className="kd-muted">
            表单会实时生成清单；需要完全控制时可以切换到 YAML 高级模式。
          </span>
        }
        tags={namespace ? <Tag>{namespace}</Tag> : undefined}
        actions={
          <Space>
            <Button onClick={() => navigate(-1)}>取消</Button>
            <Button
              loading={busy === 'validate'}
              onClick={() => void submit(true)}
              disabled={!createCan.allowed && !createCan.loading}
            >
              服务端校验
            </Button>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={busy === 'create'}
              onClick={() => void submit(false)}
              disabled={!createCan.loading && !createCan.allowed}
            >
              创建
            </Button>
          </Space>
        }
      />

      {!createCan.loading && !createCan.allowed && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
          message="没有创建权限"
          description="当前账号在目标集群/命名空间没有 create 权限，提交会被服务端拒绝。"
        />
      )}
      {error ? (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 14 }}
          message="创建失败"
          description={error instanceof Error ? error.message : '请稍后重试'}
          action={<Button size="small" onClick={() => setError(undefined)}>关闭</Button>}
        />
      ) : null}

      <Row gutter={18}>
        <Col xs={24} xl={11}>
          <Card
            title={formSupported ? '配置' : 'YAML'}
            extra={
              formSupported ? (
                <SegmentedMode
                  mode={mode}
                  onChange={(next) => {
                    if (next === 'form' && yamlDirty) {
                      try {
                        const parsed = parse(code) as ResourceItem
                        const mapped = valuesFromResource(kind, parsed, values.namespace)
                        setValues((current) => ({
                          ...current,
                          ...mapped,
                          name: parsed.metadata?.name ?? current.name,
                        }))
                        setYamlDirty(false)
                      } catch {
                        message.warning('YAML 无法解析，保留 YAML 模式')
                        return
                      }
                    }
                    setMode(next)
                  }}
                />
              ) : undefined
            }
          >
            {kind === 'secrets' && (
              <Alert
                type="info"
                showIcon
                style={{ marginBottom: 14 }}
                message="值会以 stringData 提交，由服务端完成 base64 编码"
              />
            )}

            {templates.length > 0 && mode === 'form' && (
              <div style={{ marginBottom: 16 }}>
                <div className="kd-muted" style={{ fontSize: 11, marginBottom: 6 }}>模板</div>
                <Space wrap>
                  {templates.map((template) => (
                    <Button
                      key={template.id}
                      size="small"
                      onClick={() => set({ ...template.patch, ...(template.patch.image ? { image: template.patch.image } : {}) })}
                      title={template.description}
                    >
                      {template.label}
                    </Button>
                  ))}
                  <Button size="small" icon={<ReloadOutlined />} onClick={() => setValues(emptyValues(namespace || 'default'))}>
                    重置
                  </Button>
                </Space>
              </div>
            )}

            {mode === 'form' && formSupported ? (
              <FormFields kind={kind} values={values} set={set} namespace={namespace} />
            ) : (
              <Suspense fallback={<div className="kd-muted">加载编辑器…</div>}>
                <YamlCodeEditor
                  value={code}
                  height={460}
                  onChange={(value) => {
                    setCode(value)
                    setYamlDirty(true)
                  }}
                />
              </Suspense>
            )}
          </Card>
        </Col>
        <Col xs={24} xl={13}>
          <Card
            title="清单预览"
            extra={<span className="kd-muted" style={{ fontSize: 11 }}>只读 · 提交前可核对</span>}
          >
            {preview ? (
              <pre
                className="kd-mono"
                style={{
                  margin: 0,
                  maxHeight: 620,
                  overflow: 'auto',
                  background: '#0b1020',
                  color: '#d7e0ff',
                  padding: 16,
                  borderRadius: 8,
                  lineHeight: 1.6,
                }}
              >
                {preview}
              </pre>
            ) : (
              <div className="kd-muted">填写表单后显示清单</div>
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}

function SegmentedMode({
  mode,
  onChange,
}: {
  mode: 'form' | 'yaml'
  onChange: (mode: 'form' | 'yaml') => void
}) {
  return (
    <Space size={4}>
      <Button size="small" type={mode === 'form' ? 'primary' : 'text'} onClick={() => onChange('form')}>
        表单
      </Button>
      <Button size="small" type={mode === 'yaml' ? 'primary' : 'text'} onClick={() => onChange('yaml')}>
        YAML
      </Button>
    </Space>
  )
}

function FormFields({
  kind,
  values,
  set,
  namespace,
}: {
  kind: FormKind
  values: FormValues
  set: (patch: Partial<FormValues>) => void
  namespace: string
}) {
  const workload = ['deployments', 'statefulsets', 'cronjobs'].includes(kind)
  return (
    <div>
      <Row gutter={12}>
        <Col span={12}>
          <Field label="名称" hint="小写字母、数字与连字符">
            <Input
              value={values.name}
              onChange={(event) => set({ name: event.target.value })}
              placeholder="web"
            />
          </Field>
        </Col>
        {kind !== 'namespaces' && (
          <Col span={12}>
            <Field label="命名空间">
              <Input
                value={values.namespace}
                onChange={(event) => set({ namespace: event.target.value })}
                placeholder={namespace || 'default'}
              />
            </Field>
          </Col>
        )}
      </Row>

      {workload && (
        <>
          <Field label="镜像">
            <Input
              value={values.image}
              onChange={(event) => set({ image: event.target.value })}
              placeholder="nginx:1.27"
            />
          </Field>
          <Row gutter={12}>
            {kind !== 'cronjobs' && (
              <Col span={12}>
                <Field label="副本数">
                  <InputNumber
                    min={0}
                    max={1000}
                    value={values.replicas}
                    onChange={(value) => set({ replicas: value ?? 1 })}
                    style={{ width: '100%' }}
                  />
                </Field>
              </Col>
            )}
            <Col span={12}>
              <Field label="容器端口">
                <InputNumber
                  min={1}
                  max={65535}
                  value={values.containerPort}
                  onChange={(value) => set({ containerPort: value ?? undefined })}
                  style={{ width: '100%' }}
                />
              </Field>
            </Col>
          </Row>
          {kind === 'cronjobs' && (
            <Field label="调度表达式（Cron）" hint="五段式，例如 0 * * * * 表示每小时">
              <Input value={values.schedule} onChange={(event) => set({ schedule: event.target.value })} />
            </Field>
          )}
          <Field label="启动命令（可选）" hint="空格分隔，留空使用镜像默认入口">
            <Input value={values.command} onChange={(event) => set({ command: event.target.value })} placeholder="nginx -g 'daemon off;'" />
          </Field>
          <Divider style={{ margin: '10px 0 14px' }} />
          <Field label="环境变量">
            <KvList value={values.env} onChange={(env) => set({ env })} addLabel="添加环境变量" />
          </Field>
          {kind !== 'cronjobs' && (
            <Row gutter={12}>
              <Col span={6}>
                <Field label="CPU request">
                  <Input value={values.cpuRequest} onChange={(event) => set({ cpuRequest: event.target.value })} />
                </Field>
              </Col>
              <Col span={6}>
                <Field label="内存 request">
                  <Input value={values.memoryRequest} onChange={(event) => set({ memoryRequest: event.target.value })} />
                </Field>
              </Col>
              <Col span={6}>
                <Field label="CPU limit">
                  <Input value={values.cpuLimit} onChange={(event) => set({ cpuLimit: event.target.value })} />
                </Field>
              </Col>
              <Col span={6}>
                <Field label="内存 limit">
                  <Input value={values.memoryLimit} onChange={(event) => set({ memoryLimit: event.target.value })} />
                </Field>
              </Col>
            </Row>
          )}
        </>
      )}

      {kind === 'services' && (
        <>
          <Row gutter={12}>
            <Col span={12}>
              <Field label="类型">
                <Select
                  value={values.serviceType}
                  onChange={(value) => set({ serviceType: value })}
                  options={[
                    { value: 'ClusterIP', label: 'ClusterIP' },
                    { value: 'NodePort', label: 'NodePort' },
                    { value: 'LoadBalancer', label: 'LoadBalancer' },
                  ]}
                />
              </Field>
            </Col>
            <Col span={12}>
              <Field label="服务端口">
                <InputNumber
                  min={1}
                  max={65535}
                  value={values.servicePort}
                  onChange={(value) => set({ servicePort: value ?? undefined })}
                  style={{ width: '100%' }}
                />
              </Field>
            </Col>
          </Row>
          <Field label="目标端口" hint="转发到容器的端口">
            <InputNumber
              min={1}
              max={65535}
              value={values.containerPort}
              onChange={(value) => set({ containerPort: value ?? undefined })}
              style={{ width: '100%' }}
            />
          </Field>
          <Field label="选择器标签" hint="默认 app=<名称>">
            <Input value={`app=${values.name}`} disabled />
          </Field>
        </>
      )}

      {(kind === 'configmaps' || kind === 'secrets') && (
        <Field label={kind === 'secrets' ? 'Secret 键值' : 'ConfigMap 键值'}>
          <KvList
            value={values.env}
            onChange={(env) => set({ env })}
            addLabel="添加键值"
            secret={kind === 'secrets'}
          />
        </Field>
      )}

      {kind === 'ingresses' && (
        <>
          <Row gutter={12}>
            <Col span={12}>
              <Field label="Host" hint="留空匹配所有 Host">
                <Input value={values.host} onChange={(event) => set({ host: event.target.value })} placeholder="app.example.com" />
              </Field>
            </Col>
            <Col span={12}>
              <Field label="Path">
                <Input value={values.path} onChange={(event) => set({ path: event.target.value })} />
              </Field>
            </Col>
          </Row>
          <Row gutter={12}>
            <Col span={8}>
              <Field label="Path 类型">
                <Select
                  value={values.pathType}
                  onChange={(value) => set({ pathType: value })}
                  options={[
                    { value: 'Prefix', label: 'Prefix' },
                    { value: 'Exact', label: 'Exact' },
                  ]}
                />
              </Field>
            </Col>
            <Col span={8}>
              <Field label="后端 Service">
                <Input value={values.serviceName} onChange={(event) => set({ serviceName: event.target.value })} />
              </Field>
            </Col>
            <Col span={8}>
              <Field label="后端端口">
                <InputNumber
                  min={1}
                  max={65535}
                  value={values.servicePort}
                  onChange={(value) => set({ servicePort: value ?? undefined })}
                  style={{ width: '100%' }}
                />
              </Field>
            </Col>
          </Row>
          <Field label="TLS">
            <Switch checked={values.tls} onChange={(value) => set({ tls: value })} />{' '}
            <span className="kd-muted">启用后自动生成 {values.name || '<名称>'}-tls</span>
          </Field>
        </>
      )}

      <Divider style={{ margin: '10px 0 14px' }} />
      <Field label="标签">
        <KvList value={values.labels} onChange={(labels) => set({ labels })} addLabel="添加标签" />
      </Field>
      {kind === 'statefulsets' && (
        <Field label="Service Name" hint="StatefulSet 关联的 headless Service">
          <Input value={values.serviceName} onChange={(event) => set({ serviceName: event.target.value })} />
        </Field>
      )}
      <Typography.Text className="kd-muted" style={{ fontSize: 11 }}>
        提交前会先执行一次服务端 dryRun，通过后再创建。
      </Typography.Text>
    </div>
  )
}

/** 在资源发现结果中解析 apiVersion/kind 对应的 GVR。 */
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