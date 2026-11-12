import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { ApiError } from '../lib/api'
import AuthProvider from './AuthProvider'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 4xx 不重试；5xx/429 允许一次重试。
      retry: (failureCount, error) => {
        if (error instanceof ApiError) return error.retryable && failureCount < 1
        return failureCount < 1
      },
      staleTime: 5_000,
      refetchOnWindowFocus: false,
    },
  },
})

export default function AppProviders({ children }: { children: ReactNode }) {
  return (
    <ConfigProvider locale={zhCN}>
      <AntApp>
        <QueryClientProvider client={queryClient}>
          <AuthProvider>{children}</AuthProvider>
        </QueryClientProvider>
      </AntApp>
    </ConfigProvider>
  )
}
