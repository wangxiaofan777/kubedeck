import type { GVR } from '../../lib/api'

export interface YamlDraft {
  code: string
  savedAt: number
}

const MAX_AGE_MS = 2 * 60 * 60 * 1000

export function yamlDraftKey(
  cluster: string,
  gvr: GVR,
  namespace: string | undefined,
  name: string,
): string {
  return ['kubedeck', 'yaml-draft', cluster, gvr.group, gvr.version, gvr.resource, namespace ?? '', name].join(':')
}

export function saveYamlDraft(key: string, code: string): void {
  try {
    sessionStorage.setItem(key, JSON.stringify({ code, savedAt: Date.now() } satisfies YamlDraft))
  } catch {
    // 隐私模式或配额不足时忽略；比较页会提示没有草稿。
  }
}

export function loadYamlDraft(key: string): YamlDraft | undefined {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return undefined
    const draft = JSON.parse(raw) as YamlDraft
    if (typeof draft.code !== 'string' || typeof draft.savedAt !== 'number') return undefined
    if (Date.now() - draft.savedAt > MAX_AGE_MS) {
      sessionStorage.removeItem(key)
      return undefined
    }
    return draft
  } catch {
    return undefined
  }
}

export function clearYamlDraft(key: string): void {
  try {
    sessionStorage.removeItem(key)
  } catch {
    // ignore
  }
}