type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral'

const RULES: Array<{ pattern: RegExp; tone: Tone }> = [
  // 先判危险状态：NotReady 也包含 "ready"，不能被成功规则抢先命中。
  {
    pattern:
      /(failed|error|crashloop|notready|unreachable|imagepull|errimagepull|evicted|disabled|oomkilled)/i,
    tone: 'danger',
  },
  {
    pattern: /(pending|progressing|containercreating|terminating|updating|degraded|unknown)/i,
    tone: 'warning',
  },
  {
    pattern: /(running|ready|succeeded|active|healthy|available|normal|complete)/i,
    tone: 'success',
  },
  { pattern: /(external|loadbalancer|nodeport|created|info)/i, tone: 'info' },
]

export function statusTone(value: string): Tone {
  for (const rule of RULES) {
    if (rule.pattern.test(value)) return rule.tone
  }
  return 'neutral'
}

/** StatusPill 统一列表与详情的状态语义与颜色。 */
export default function StatusPill({ value, title }: { value: string; title?: string }) {
  if (!value || value === '-') return <span className="kd-muted">-</span>
  return (
    <span className={`kd-pill ${statusTone(value)}`} title={title ?? value}>
      {value}
    </span>
  )
}