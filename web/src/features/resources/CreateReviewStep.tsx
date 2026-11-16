import { Alert, Button, Card, Space, Typography } from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useCreateResource } from './CreateResourceLayout'

export default function CreateReviewStep() {
  const { base, namespace, preview, busy, submit, createCan, cancel, yamlDirty } = useCreateResource()
  const navigate = useNavigate()
  const nsQuery = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''

  return (
    <Space direction="vertical" size={14} style={{ width: '100%', maxWidth: 980 }}>
      {!createCan.loading && !createCan.allowed && (
        <Alert
          type="warning"
          showIcon
          message="没有创建权限"
          description="当前账号在目标集群/命名空间没有 create 权限，最终提交会被服务端拒绝。"
        />
      )}
      <Alert
        type="info"
        showIcon
        message="提交前先执行服务端 dryRun"
        description="dryRun 会经过目标集群的准入与校验，但不会持久化对象；通过后才会创建。"
      />

      <Card
        size="small"
        title="清单预览"
        extra={
          <span className="kd-muted" style={{ fontSize: 11 }}>
            {yamlDirty ? '来自 YAML 编辑' : '来自表单'}
          </span>
        }
      >
        {preview ? (
          <pre
            className="kd-mono"
            style={{
              margin: 0,
              maxHeight: 560,
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
          <Typography.Text type="secondary">没有可提交的清单。</Typography.Text>
        )}
      </Card>

      <Space>
        <Button onClick={() => navigate(`${base}/yaml${nsQuery}`)}>上一步：YAML</Button>
        <Button onClick={cancel}>取消</Button>
        <Button
          loading={busy === 'validate'}
          disabled={!preview.trim() || !createCan.allowed}
          onClick={() => void submit(true)}
        >
          服务端校验
        </Button>
        <Button
          type="primary"
          icon={<SaveOutlined />}
          loading={busy === 'create'}
          disabled={!preview.trim() || !createCan.allowed}
          onClick={() => void submit(false)}
        >
          创建
        </Button>
      </Space>
    </Space>
  )
}