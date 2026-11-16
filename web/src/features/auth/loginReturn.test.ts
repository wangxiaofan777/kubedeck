import { describe, expect, it } from 'vitest'
import { safeReturnTo } from './LoginPage'

describe('safeReturnTo', () => {
  it('没有 returnTo 时回到首页', () => {
    expect(safeReturnTo(null)).toBe('/')
    expect(safeReturnTo('')).toBe('/')
  })

  it('保留站内相对路径与查询参数', () => {
    expect(safeReturnTo('/clusters/local/deployments?namespace=kubedeck-e2e')).toBe(
      '/clusters/local/deployments?namespace=kubedeck-e2e',
    )
  })

  it('拒绝协议相对地址与外部地址', () => {
    expect(safeReturnTo('//evil.example/login')).toBe('/')
    expect(safeReturnTo('https://evil.example/')).toBe('/')
  })

  it('拒绝反斜杠混淆的路径', () => {
    expect(safeReturnTo('/\\evil.example')).toBe('/')
  })
})