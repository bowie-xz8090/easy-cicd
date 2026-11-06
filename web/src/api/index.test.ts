import axios from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'

const previousAdapter = axios.defaults.adapter
afterEach(() => { axios.defaults.adapter = previousAdapter })

describe('access response handling', () => {
  it.each([
    { code: 40101, rejected: true, loginRequired: true },
    { code: 40101, rejected: false, loginRequired: true },
    { code: 40301, rejected: true, loginRequired: false, accessDenied: true },
    { code: 40301, rejected: false, loginRequired: false, accessDenied: true },
    { code: -1, rejected: true, loginRequired: false },
  ])('distinguishes access expiry from internal password errors ($code, $rejected)', async ({ code, rejected, loginRequired, accessDenied }) => {
    vi.resetModules()
    let requestHeader: unknown
    axios.defaults.adapter = async (config) => {
      requestHeader = config.headers.get('X-CICD-Request')
      const data = { code, message: 'validation failed' }
      if (rejected) throw { response: { status: code === 40301 ? 403 : 401, data } }
      return { data, status: 200, statusText: 'OK', headers: {}, config }
    }
    const listener = vi.fn()
    const deniedListener = vi.fn()
    window.addEventListener('cicd-login-required', listener)
    window.addEventListener('cicd-access-denied', deniedListener)
    try {
      const api = await import('./index')
      await expect(api.fetchProjects()).rejects.toMatchObject({ code })
      expect(requestHeader).toBe('1')
      expect(listener).toHaveBeenCalledTimes(loginRequired ? 1 : 0)
      expect(deniedListener).toHaveBeenCalledTimes(accessDenied ? 1 : 0)
    } finally {
      window.removeEventListener('cicd-login-required', listener)
      window.removeEventListener('cicd-access-denied', deniedListener)
    }
  })
})
