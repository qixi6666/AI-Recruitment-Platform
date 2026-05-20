import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, type LoginPayload } from '../api/client'
import type { Role, UserDTO } from '../types/api'

const tokenKey = 'recruitment_token'
const userKey = 'recruitment_user'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem(tokenKey) ?? '')
  const storedUser = localStorage.getItem(userKey)
  const user = ref<UserDTO | null>(storedUser ? (JSON.parse(storedUser) as UserDTO) : null)

  const isAuthed = computed(() => Boolean(token.value && user.value))
  const role = computed<Role | null>(() => user.value?.role ?? null)

  async function login(payload: LoginPayload) {
    const result = await api.login(payload)
    token.value = result.token
    user.value = result.user
    localStorage.setItem(tokenKey, result.token)
    localStorage.setItem(userKey, JSON.stringify(result.user))
  }

  async function register(payload: LoginPayload) {
    await api.register(payload)
    await login(payload)
  }

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem(tokenKey)
    localStorage.removeItem(userKey)
  }

  return { token, user, isAuthed, role, login, register, logout }
})
