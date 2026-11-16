/** 平台动作到 Kubernetes RBAC 动词/子资源的映射，避免把动作名直接当 SSAR 动词。 */
export type WorkloadAction = 'scale' | 'restart' | 'rollback' | 'pause' | 'resume' | 'evict'

export interface WorkloadCanCheck {
  verb: 'update' | 'patch' | 'create'
  subresource?: string
}

export function workloadCanCheck(action: WorkloadAction): WorkloadCanCheck {
  switch (action) {
    case 'scale':
      return { verb: 'update', subresource: 'scale' }
    case 'evict':
      return { verb: 'create', subresource: 'eviction' }
    case 'restart':
    case 'rollback':
    case 'pause':
    case 'resume':
      return { verb: 'patch' }
  }
}