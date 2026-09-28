import axios, { type AxiosError } from 'axios'
import { ElMessage } from 'element-plus'

declare module 'axios' {
  interface AxiosRequestConfig {
    /** 为 true 时出错不弹全局提示，由调用方自行展示。 */
    silent?: boolean
  }
}

const TOKEN_KEY = 'yunshu.token'

// 浏览器可能禁用存储（隐私模式等），读写都要兜底。
export const tokenStore = {
  get(): string {
    try {
      return localStorage.getItem(TOKEN_KEY) ?? ''
    } catch {
      return ''
    }
  },
  set(token: string) {
    try {
      localStorage.setItem(TOKEN_KEY, token)
    } catch {
      /* 忽略：本次会话内仍可用内存中的 token */
    }
  },
  clear() {
    try {
      localStorage.removeItem(TOKEN_KEY)
    } catch {
      /* 忽略 */
    }
  },
}

export const http = axios.create({ baseURL: '/api/v1', timeout: 90_000 })

let unauthorizedHandler: (() => void) | null = null

/** 注册 401 处理（清理登录态并跳转登录页）。 */
export function onUnauthorized(fn: () => void) {
  unauthorizedHandler = fn
}

http.interceptors.request.use((config) => {
  const token = tokenStore.get()
  if (token) config.headers.set('Authorization', `Bearer ${token}`)
  return config
})

http.interceptors.response.use(
  (res) => res,
  (err: AxiosError) => {
    if (axios.isCancel(err)) return Promise.reject(err)
    const status = err.response?.status
    const url = err.config?.url ?? ''
    if (status === 401 && !url.startsWith('/auth/login') && !url.startsWith('/auth/password')) {
      tokenStore.clear()
      unauthorizedHandler?.()
      return Promise.reject(err)
    }
    if (!err.config?.silent) {
      ElMessage.error({ message: errorMessage(err), grouping: true })
    }
    return Promise.reject(err)
  },
)

/** 把请求错误转成给用户看的中文提示。 */
export function errorMessage(err: unknown): string {
  if (axios.isAxiosError(err)) {
    const data = err.response?.data as { error?: string } | undefined
    if (data && typeof data.error === 'string' && data.error) return data.error
    if (err.code === 'ECONNABORTED') return '请求超时，请稍后重试'
    if (!err.response) return '无法连接服务器，请检查网络'
    return `请求失败（HTTP ${err.response.status}）`
  }
  return err instanceof Error ? err.message : String(err)
}
