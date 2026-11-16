import type { FormValues } from './createManifest'

export interface CreateDraft {
  values: FormValues
  code: string
  yamlDirty: boolean
  savedAt: number
}

const MAX_AGE_MS = 2 * 60 * 60 * 1000

export function createDraftKey(cluster: string, typeKey: string): string {
  return `kubedeck:create-draft:${cluster}:${typeKey}`
}

export function saveCreateDraft(key: string, draft: Omit<CreateDraft, 'savedAt'>): void {
  try {
    sessionStorage.setItem(key, JSON.stringify({ ...draft, savedAt: Date.now() } satisfies CreateDraft))
  } catch {
    // 隐私模式或配额不足时忽略。
  }
}

export function loadCreateDraft(key: string): CreateDraft | undefined {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return undefined
    const draft = JSON.parse(raw) as CreateDraft
    if (!draft.values || typeof draft.code !== 'string' || typeof draft.savedAt !== 'number') {
      return undefined
    }
    if (Date.now() - draft.savedAt > MAX_AGE_MS) {
      sessionStorage.removeItem(key)
      return undefined
    }
    return draft
  } catch {
    return undefined
  }
}

export function clearCreateDraft(key: string): void {
  try {
    sessionStorage.removeItem(key)
  } catch {
    // ignore
  }
}