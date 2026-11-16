import type { ThemeConfig } from 'antd'

/** KubeDeck 设计 token：深色导航 + 浅色内容，统一主色、状态色与表格密度。 */
export const kubedeckTheme: ThemeConfig = {
  token: {
    colorPrimary: '#2563eb',
    colorInfo: '#2563eb',
    colorSuccess: '#16a34a',
    colorWarning: '#d97706',
    colorError: '#dc2626',
    colorTextBase: '#0f172a',
    colorBgLayout: '#f4f6f9',
    colorBorder: '#e2e8f0',
    colorBorderSecondary: '#eef2f7',
    borderRadius: 8,
    borderRadiusLG: 10,
    fontSize: 13,
    fontFamily:
      "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
    boxShadowTertiary: 'none',
    controlHeight: 32,
  },
  components: {
    Layout: {
      headerBg: '#ffffff',
      headerHeight: 56,
      headerPadding: '0 20px',
      bodyBg: '#f4f6f9',
      siderBg: '#0f172a',
    },
    Menu: {
      darkItemBg: '#0f172a',
      darkSubMenuItemBg: '#0f172a',
      darkItemColor: '#94a3b8',
      darkItemHoverColor: '#e2e8f0',
      darkItemSelectedBg: '#1e293b',
      darkItemSelectedColor: '#ffffff',
      itemHeight: 36,
      itemMarginInline: 8,
      itemBorderRadius: 6,
      groupTitleColor: '#64748b',
      groupTitleFontSize: 11,
    },
    Table: {
      headerBg: '#f8fafc',
      headerColor: '#475569',
      headerSplitColor: 'transparent',
      cellPaddingBlock: 10,
      cellPaddingInline: 12,
      rowHoverBg: '#f8fafc',
      borderColor: '#eef2f7',
      fontSize: 13,
    },
    Card: {
      paddingLG: 20,
      headerFontSize: 14,
      headerHeight: 48,
      colorBorderSecondary: '#e8edf4',
    },
    Button: {
      fontWeight: 500,
      primaryShadow: 'none',
      defaultShadow: 'none',
    },
    Tabs: {
      titleFontSize: 13,
      horizontalItemPadding: '10px 0',
      horizontalItemGutter: 24,
    },
    Descriptions: {
      labelBg: '#f8fafc',
      titleMarginBottom: 8,
    },
    Drawer: {
      paddingLG: 24,
    },
  },
}