import { Alert, Button, Card, Space } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { Navigate, useNavigate } from 'react-router-dom'
import FormFields from './CreateFields'
import { emptyValues, templatesFor } from './createManifest'
import { useCreateResource } from './CreateResourceLayout'

export default function CreateFormStep() {
  const {
    base,
    kind,
    formSupported,
    namespace,
    values,
    set,
    setValues,
    createCan,
    cancel,
    copyFrom,
  } = useCreateResource()
  const navigate = useNavigate()
  const nsQuery = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''

  if (!formSupported) {
    return <Navigate to={`${base}/yaml${nsQuery}`} replace />
  }

  const templates = templatesFor(kind)

  return (
    <Space direction="vertical" size={14} style={{ width: '100%', maxWidth: 920 }}>
      {copyFrom ? (
        <Alert
          type="info"
          showIcon
          message="已从现有对象复制"
          description="可映射字段已预填；名称需要重新填写，避免与源对象冲突。"
        />
      ) : null}
      {!createCan.loading && !createCan.allowed && (
        <Alert
          type="warning"
          showIcon
          message="没有创建权限"
          description="当前账号在目标集群/命名空间没有 create 权限，最终提交会被服务端拒绝。"
        />
      )}

      <Card title="配置" size="small">
        {templates.length > 0 && (
          <div style={{ marginBottom: 16 }}>
            <div className="kd-muted" style={{ fontSize: 11, marginBottom: 6 }}>
              模板
            </div>
            <Space wrap>
              {templates.map((template) => (
                <Button
                  key={template.id}
                  size="small"
                  onClick={() => set(template.patch)}
                  title={template.description}
                >
                  {template.label}
                </Button>
              ))}
              <Button
                size="small"
                icon={<ReloadOutlined />}
                onClick={() => setValues(emptyValues(namespace || 'default'))}
              >
                重置
              </Button>
            </Space>
          </div>
        )}
        <FormFields kind={kind} values={values} set={set} namespace={namespace} />
      </Card>

      <Space>
        <Button onClick={cancel}>取消</Button>
        <Button type="primary" onClick={() => navigate(`${base}/yaml${nsQuery}`)}>
          下一步：YAML
        </Button>
      </Space>
    </Space>
  )
}