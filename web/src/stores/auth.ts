import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import axios from 'axios'
import { authApi, tokenStore, type LoginResult, type User } from '@/api'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(tokenStore.get())
  const user = ref<User | null>(null)

  const isLoggedIn = computed(() => !!token.value && !!user.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  function setSession(res: LoginResult) {
    token.value = res.token
    user.value = res.user
    tokenStore.set(res.token)
  }

  function reset() {
    token.value = ''
    user.value = null
    tokenStore.clear()
  }

  async function login(username: string, password: string) {
    setSession(await authApi.login(username, password))
  }

  /**
   * 返回当前用户；token 失效时清理登录态并返回 null。
   * 网络错误会抛出，由调用方提示，避免因为服务暂时不可用就丢掉登录态。
   */
  async function ensureUser(): Promise<User | null> {
    if (!token.value) return null
    if (user.value) return user.value
    try {
      user.value = await authApi.me()
      return user.value
    } catch (err) {
      if (axios.isAxiosError(err) && err.response?.status === 401) {
        reset()
        return null
      }
      throw err
    }
  }

  async function changePassword(oldPassword: string, newPassword: string) {
    setSession(await authApi.changePassword(oldPassword, newPassword))
  }

  return { token, user, isLoggedIn, isAdmin, login, reset, ensureUser, changePassword }
})
