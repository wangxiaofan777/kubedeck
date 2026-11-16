import { describe, expect, it } from 'vitest'
import { buildSubscribeFrame, resyncAction, resyncWindowMS } from './useRealtime'

describe('buildSubscribeFrame', () => {
  it('携带标签选择器与资源版本', () => {
    expect(
      buildSubscribeFrame(
        {
          id: 'apps/v1/deployments/prod',
          gvr: { group: 'apps', version: 'v1', resource: 'deployments' },
          namespace: 'prod',
          labelSelector: 'app=web',
        },
        '42',
      ),
    ).toEqual({
      type: 'subscribe',
      id: 'apps/v1/deployments/prod',
      group: 'apps',
      version: 'v1',
      resource: 'deployments',
      namespace: 'prod',
      labelSelector: 'app=web',
      resourceVersion: '42',
    })
  })

  it('重新订阅时使用空资源版本', () => {
    const frame = buildSubscribeFrame(
      {
        id: 'v1/pods/default',
        gvr: { group: '', version: 'v1', resource: 'pods' },
        namespace: 'default',
      },
      '',
    )
    expect(frame.resourceVersion).toBe('')
  })
})

describe('resyncAction', () => {
  it('首次 resync 重新订阅', () => {
    expect(resyncAction(0, 1_000)).toBe('resubscribe')
  })

  it('短时间连续 resync 重建连接', () => {
    expect(resyncAction(1_000, 1_000 + resyncWindowMS - 1)).toBe('reconnect')
    expect(resyncAction(1_000, 1_000 + resyncWindowMS)).toBe('resubscribe')
  })
})