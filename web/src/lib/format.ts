/** 把 Kubernetes 时间戳格式化为紧凑的相对时间（s/m/h/d）。 */
export function ageOf(timestamp?: string): string {
  if (!timestamp) return '-'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(timestamp).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

/** 按状态文本选择 AntD Tag 颜色。 */
export function statusColor(value: string): string {
  if (/^(Running|Ready|Succeeded|Active|Healthy)/.test(value)) return 'green'
  if (/^(Pending|ContainerCreating|Terminating)/.test(value)) return 'gold'
  if (/^(Failed|Error|CrashLoopBackOff|NotReady|Unknown)/.test(value)) return 'red'
  return 'default'
}

/** 统一时间格式：YYYY-MM-DD HH:mm。 */
export function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '-'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** Kubernetes CPU 数量（"4" / "500m"）转毫核。 */
export function parseCPU(value: unknown): number {
  if (typeof value !== 'string' && typeof value !== 'number') return 0
  const text = String(value)
  if (text.endsWith('m')) return Number(text.slice(0, -1)) || 0
  return Math.round((Number(text) || 0) * 1000)
}

/** Kubernetes 内存数量（Ki/Mi/Gi/Ti）转字节。 */
export function parseMemory(value: unknown): number {
  if (typeof value !== 'string' && typeof value !== 'number') return 0
  const text = String(value)
  const units: Record<string, number> = {
    Ki: 1024,
    Mi: 1024 ** 2,
    Gi: 1024 ** 3,
    Ti: 1024 ** 4,
    K: 1000,
    M: 1000 ** 2,
    G: 1000 ** 3,
  }
  for (const [suffix, factor] of Object.entries(units)) {
    if (text.endsWith(suffix)) return (Number(text.slice(0, -suffix.length)) || 0) * factor
  }
  return Number(text) || 0
}

export function formatCPU(milli: number): string {
  if (milli <= 0) return '-'
  if (milli % 1000 === 0) return `${milli / 1000} 核`
  return `${(milli / 1000).toFixed(2)} 核`
}

export function formatMemory(bytes: number): string {
  if (bytes <= 0) return '-'
  const gib = bytes / 1024 ** 3
  if (gib >= 1) return `${gib.toFixed(gib >= 10 ? 0 : 1)} GiB`
  return `${(bytes / 1024 ** 2).toFixed(0)} MiB`
}
