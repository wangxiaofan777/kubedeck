/** Sparkline 用内联 SVG 绘制面积图，避免为单条趋势引入图表库。 */
export function AreaSpark({
  values,
  color = '#2563eb',
  height = 56,
  label,
}: {
  values: number[]
  color?: string
  height?: number
  label?: string
}) {
  if (values.length < 2) {
    return <div className="kd-chart-label" style={{ height, display: 'grid', placeItems: 'center' }}>采样中，稍后显示趋势</div>
  }
  const width = 240
  const max = Math.max(...values)
  const min = Math.min(...values)
  const span = max - min || 1
  const points = values.map((value, index) => {
    const x = (index / (values.length - 1)) * width
    const y = height - 6 - ((value - min) / span) * (height - 12)
    return [x, y] as const
  })
  const line = points.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = `${points[0][0]},${height} ${line} ${points[points.length - 1][0]},${height}`
  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      width="100%"
      height={height}
      role="img"
      aria-label={label ?? '趋势'}
      preserveAspectRatio="none"
    >
      <polygon points={area} fill={color} opacity={0.08} />
      <polyline
        points={line}
        fill="none"
        stroke={color}
        strokeWidth={2}
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

export interface Segment {
  key: string
  label: string
  value: number
  color: string
}

/** DistributionBar 用水平堆叠条展示状态分布。 */
export function DistributionBar({ segments }: { segments: Segment[] }) {
  const total = segments.reduce((sum, segment) => sum + segment.value, 0)
  if (total === 0) {
    return <div className="kd-chart-label">暂无数据</div>
  }
  return (
    <div>
      <div
        style={{
          display: 'flex',
          height: 12,
          borderRadius: 999,
          overflow: 'hidden',
          background: '#f1f5f9',
        }}
      >
        {segments
          .filter((segment) => segment.value > 0)
          .map((segment) => (
            <div
              key={segment.key}
              title={`${segment.label}: ${segment.value}`}
              style={{ width: `${(segment.value / total) * 100}%`, background: segment.color }}
            />
          ))}
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 14px', marginTop: 10 }}>
        {segments.map((segment) => (
          <span key={segment.key} style={{ fontSize: 12, color: '#475569' }}>
            <span
              style={{
                display: 'inline-block',
                width: 7,
                height: 7,
                borderRadius: 2,
                background: segment.color,
                marginRight: 6,
              }}
            />
            {segment.label}
            <span className="kd-muted" style={{ marginLeft: 4 }}>
              {segment.value}
            </span>
          </span>
        ))}
      </div>
    </div>
  )
}

/** MiniBar 用于工作负载健康度等单值比例展示。 */
export function MiniBar({ value, total, danger }: { value: number; total: number; danger?: number }) {
  const percent = total > 0 ? Math.round((value / total) * 100) : 0
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12 }}>
        <span className="kd-muted">可用</span>
        <span>{percent}%</span>
      </div>
      <div style={{ height: 6, borderRadius: 999, background: '#f1f5f9', marginTop: 5 }}>
        <div
          style={{
            width: `${percent}%`,
            height: '100%',
            borderRadius: 999,
            background: danger && danger > 0 ? '#f59e0b' : '#16a34a',
          }}
        />
      </div>
    </div>
  )
}