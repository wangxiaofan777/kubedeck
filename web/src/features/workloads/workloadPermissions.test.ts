import { describe, expect, it } from 'vitest'
import { workloadCanCheck } from './workloadPermissions'

describe('workloadCanCheck', () => {
  it('伸缩使用 update + scale 子资源', () => {
    expect(workloadCanCheck('scale')).toEqual({ verb: 'update', subresource: 'scale' })
  })

  it('重启、回滚、暂停与恢复使用 patch', () => {
    for (const action of ['restart', 'rollback', 'pause', 'resume'] as const) {
      expect(workloadCanCheck(action)).toEqual({ verb: 'patch' })
    }
  })

  it('驱逐使用 create + eviction 子资源', () => {
    expect(workloadCanCheck('evict')).toEqual({ verb: 'create', subresource: 'eviction' })
  })
})