import { ref } from 'vue'
import { fetchSession, type AccessSession } from './api'

export const accessSession = ref<AccessSession | null>(null)
export const accessLoading = ref(true)
export const accessError = ref('')

export async function refreshSession() {
  accessLoading.value = true
  accessError.value = ''
  try {
    accessSession.value = await fetchSession()
  } catch {
    accessSession.value = null
    accessError.value = '暂时无法读取登录状态，请重试。'
  } finally {
    accessLoading.value = false
  }
}

export function requireLogin() {
  accessSession.value = { authenticated: false, authorized: false, mode: 'anonymous', display_name: '', expires_at: null }
  accessError.value = ''
}

export function denyAccess() {
  if (accessSession.value) accessSession.value = { ...accessSession.value, authorized: false }
}
