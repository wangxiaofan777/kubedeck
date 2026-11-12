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
