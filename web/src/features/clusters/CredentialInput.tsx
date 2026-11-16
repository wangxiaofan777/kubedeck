import { Alert, Input, Radio, Space, Switch } from 'antd'

export interface CredentialDraft {
  mode: 'kubeconfig' | 'token'
  kubeconfig: string
  server: string
  token: string
  caData: string
  insecure: boolean
}

export function emptyCredential(): CredentialDraft {
  return { mode: 'kubeconfig', kubeconfig: '', server: '', token: '', caData: '', insecure: false }
}

export function credentialInput(draft: CredentialDraft) {
  if (draft.mode === 'kubeconfig') {
    return { kubeconfig: draft.kubeconfig }
  }
  return {
    server: draft.server,
    token: draft.token,
    caData: draft.caData || undefined,
    insecureSkipTLSVerify: draft.insecure,
  }
}

/** CredentialInput 是接入与轮换共用的凭据表单。 */
export default function CredentialInput({
  value,
  onChange,
}: {
  value: CredentialDraft
  onChange: (next: CredentialDraft) => void
}) {
  const set = (patch: Partial<CredentialDraft>) => onChange({ ...value, ...patch })
  return (
    <div>
      <Radio.Group
        value={value.mode}
        onChange={(event) => set({ mode: event.target.value })}
        optionType="button"
        buttonStyle="solid"
        style={{ marginBottom: 14 }}
        options={[
          { value: 'kubeconfig', label: 'kubeconfig' },
          { value: 'token', label: 'Server + Token' },
        ]}
      />

      {value.mode === 'kubeconfig' ? (
        <Input.TextArea
          value={value.kubeconfig}
          onChange={(event) => set({ kubeconfig: event.target.value })}
          rows={12}
          placeholder={'apiVersion: v1\nkind: Config\nclusters: ...'}
          style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }}
          aria-label="kubeconfig"
        />
      ) : (
        <Space direction="vertical" size={10} style={{ width: '100%' }}>
          <Input
            value={value.server}
            onChange={(event) => set({ server: event.target.value })}
            placeholder="https://10.0.0.1:6443"
            aria-label="apiserver 地址"
          />
          <Input.Password
            value={value.token}
            onChange={(event) => set({ token: event.target.value })}
            placeholder="ServiceAccount Token"
            aria-label="token"
          />
          <Input.TextArea
            value={value.caData}
            onChange={(event) => set({ caData: event.target.value })}
            rows={4}
            placeholder="CA 证书 PEM（可选；自签名集群需要）"
            style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }}
            aria-label="CA 证书"
          />
          <Space>
            <Switch checked={value.insecure} onChange={(checked) => set({ insecure: checked })} />
            <span>跳过 TLS 校验（仅测试环境）</span>
          </Space>
        </Space>
      )}

      <Alert
        type="info"
        showIcon
        style={{ marginTop: 12 }}
        message="凭据只用于服务端加密存储"
        description="平台不会在列表、详情或日志中回显凭据；kubeconfig 中的文件路径凭据会被拒绝。"
      />
    </div>
  )
}