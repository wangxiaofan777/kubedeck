import { describe, expect, it } from 'vitest'
import { statusTone } from './StatusPill'

describe('statusTone', () => {
  it('NotReady 必须判为危险，不能因为包含 Ready 而变绿', () => {
    expect(statusTone('NotReady')).toBe('danger')
  })

  it('常见失败状态判为危险', () => {
    for (const value of ['CrashLoopBackOff', 'ImagePullBackOff', 'Evicted', 'Failed', 'OOMKilled']) {
      expect(statusTone(value)).toBe('danger')
    }
  })

  it('正常运行状态判为成功', () => {
    for (const value of ['Running', 'Ready', 'Succeeded', 'Healthy']) {
      expect(statusTone(value)).toBe('success')
    }
  })

  it('未知状态使用中性色', () => {
    expect(statusTone('SomethingNew')).toBe('neutral')
  })
})