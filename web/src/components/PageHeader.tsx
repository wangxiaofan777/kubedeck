import type { ReactNode } from 'react'
import { Breadcrumb, Space, Typography } from 'antd'
import { Link } from 'react-router-dom'

export interface Crumb {
  label: string
  to?: string
}

interface Props {
  crumbs?: Crumb[]
  title: ReactNode
  subtitle?: ReactNode
  tags?: ReactNode
  actions?: ReactNode
}

/** PageHeader 统一页面层级：面包屑 + 标题 + 副信息 + 操作区。 */
export default function PageHeader({ crumbs, title, subtitle, tags, actions }: Props) {
  return (
    <div className="kd-page-head">
      {crumbs && crumbs.length > 0 && (
        <Breadcrumb
          className="kd-breadcrumb"
          items={crumbs.map((crumb) => ({
            title: crumb.to ? <Link to={crumb.to}>{crumb.label}</Link> : crumb.label,
          }))}
        />
      )}
      <div className="kd-page-title-row">
        <div>
          <h1 className="kd-page-title">{title}</h1>
          {(subtitle || tags) && (
            <Space size={8} wrap className="kd-page-subtitle">
              {subtitle}
              {tags}
            </Space>
          )}
        </div>
        {actions && <div className="kd-page-actions">{actions}</div>}
      </div>
    </div>
  )
}

/** 页面内小节标题，避免每个页面重复写 Typography。 */
export function SectionTitle({ children, extra }: { children: ReactNode; extra?: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
      <h2 className="kd-section-title">{children}</h2>
      {extra}
    </div>
  )
}

export function Muted({ children }: { children: ReactNode }) {
  return <Typography.Text className="kd-muted">{children}</Typography.Text>
}