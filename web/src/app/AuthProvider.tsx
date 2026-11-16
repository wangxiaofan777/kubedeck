import { createContext, useCallback, useContext, useMemo, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type MeResponse } from '../lib/api'

interface AuthContextValue {
  me?: MeResponse
  loading: boolean
  error?: unknown
  isDev: boolean
  logout: () => Promise<void>
  refetch: () => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

/** AuthProvider 只负责读取当前身份；登录跳转由 RequireAuth 处理。 */
export default function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const meQuery = useQuery({
    queryKey: ['me'],
    queryFn: ({ signal }) => api.me(signal),
    staleTime: 30_000,
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.code === 'UNAUTHENTICATED') return false
      return failureCount < 1
    },
  })

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } catch {
      // 登出接口失败也要清理本地状态，避免用户卡在无法退出的页面。
    }
    queryClient.clear()
    window.location.assign('/')
  }, [queryClient])

  const me = meQuery.data
  const meLoading = meQuery.isLoading
  const meError = meQuery.error

  const value = useMemo<AuthContextValue>(
    () => ({
      me,
      loading: meLoading,
      error: meError,
      isDev: me?.mode === 'dev',
      logout,
      refetch: () => {
        void meQuery.refetch()
      },
    }),
    // meQuery 是稳定对象，但 refetch 依赖它；显式列出数据状态避免无谓重渲染。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [me, meLoading, meError, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider')
  return ctx
}
