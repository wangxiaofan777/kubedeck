import { Button, Col, Divider, Input, InputNumber, Row, Select, Space, Switch, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import type { FormKind, FormValues, KeyValue } from './createManifest'

export function Field({
  label,
  children,
  hint,
}: {
  label: string
  children: React.ReactNode
  hint?: string
}) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div style={{ fontSize: 12, color: '#475569', marginBottom: 5 }}>{label}</div>
      {children}
      {hint && (
        <div className="kd-muted" style={{ fontSize: 11, marginTop: 4 }}>
          {hint}
        </div>
      )}
    </div>
  )
}

export function KvList({
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

export default function FormFields({
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
              <Input
                value={values.schedule}
                onChange={(event) => set({ schedule: event.target.value })}
              />
            </Field>
          )}
          <Field label="启动命令（可选）" hint="空格分隔，留空使用镜像默认入口">
            <Input
              value={values.command}
              onChange={(event) => set({ command: event.target.value })}
              placeholder="nginx -g 'daemon off;'"
            />
          </Field>
          <Divider style={{ margin: '10px 0 14px' }} />
          <Field label="环境变量">
            <KvList value={values.env} onChange={(env) => set({ env })} addLabel="添加环境变量" />
          </Field>
          {kind !== 'cronjobs' && (
            <Row gutter={12}>
              <Col span={6}>
                <Field label="CPU request">
                  <Input
                    value={values.cpuRequest}
                    onChange={(event) => set({ cpuRequest: event.target.value })}
                  />
                </Field>
              </Col>
              <Col span={6}>
                <Field label="内存 request">
                  <Input
                    value={values.memoryRequest}
                    onChange={(event) => set({ memoryRequest: event.target.value })}
                  />
                </Field>
              </Col>
              <Col span={6}>
                <Field label="CPU limit">
                  <Input
                    value={values.cpuLimit}
                    onChange={(event) => set({ cpuLimit: event.target.value })}
                  />
                </Field>
              </Col>
              <Col span={6}>
                <Field label="内存 limit">
                  <Input
                    value={values.memoryLimit}
                    onChange={(event) => set({ memoryLimit: event.target.value })}
                  />
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
                <Input
                  value={values.host}
                  onChange={(event) => set({ host: event.target.value })}
                  placeholder="app.example.com"
                />
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
                <Input
                  value={values.serviceName}
                  onChange={(event) => set({ serviceName: event.target.value })}
                />
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
          <Input
            value={values.serviceName}
            onChange={(event) => set({ serviceName: event.target.value })}
          />
        </Field>
      )}
      <Typography.Text className="kd-muted" style={{ fontSize: 11 }}>
        下一步会在 YAML 页生成清单，并在确认页执行服务端 dryRun。
      </Typography.Text>
    </div>
  )
}