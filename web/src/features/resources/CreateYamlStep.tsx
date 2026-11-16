import { lazy, Suspense } from 'react'
import { Alert, Button, Card, Space, Spin } from 'antd'
import { useNavigate } from 'react-router-dom'
import { stringify } from 'yaml'
import { useCreateResource } from './CreateResourceLayout'

const YamlCodeEditor = lazy(() => import('./YamlCodeEditor'))

export default function CreateYamlStep() {
  const {
    base,
    namespace,
    formSupported,
    code,
    setCode,
    yamlDirty,
    setYamlDirty,
    manifest,
    cancel,
  } = useCreateResource()
  const navigate = useNavigate()
  const nsQuery = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''

  return (
    <Space direction="vertical" size={14} style={{ width: '100%', maxWidth: 980 }}>
      {formSupported && !yamlDirty && (
        <Alert
          type="info"
          showIcon
          message="当前清单由表单生成"
          description="可以直接修改 YAML；修改后表单与 YAML 不再自动同步。"
        />
      )}
      {namespace && <Alert type="info" showIcon message={`目标命名空间：${namespace}`} />}

      <Card
        size="small"
        title="YAML"
        extra={<span className="kd-muted" style={{ fontSize: 11 }}>高级模式</span>}
      >
        <Suspense
          fallback={
            <div style={{ display: 'grid', placeItems: 'center', minHeight: 420 }}>
              <Spin />
            </div>
          }
        >
          <YamlCodeEditor
            value={code}
            height={520}
            onChange={(value) => {
              setCode(value)
              setYamlDirty(true)
            }}
          />
        </Suspense>
      </Card>

      <Space wrap>
        {formSupported ? (
          <Button onClick={() => navigate(`${base}${nsQuery}`)}>上一步：配置</Button>
        ) : (
          <Button onClick={cancel}>取消</Button>
        )}
        {formSupported && yamlDirty && manifest && (
          <Button
            onClick={() => {
              setYamlDirty(false)
              setCode(stringify(manifest))
            }}
          >
            用表单重新生成
          </Button>
        )}
        <Button type="primary" onClick={() => navigate(`${base}/review${nsQuery}`)} disabled={!code.trim()}>
          下一步：确认
        </Button>
      </Space>
    </Space>
  )
}