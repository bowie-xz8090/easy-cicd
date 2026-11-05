import axios from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'

const previousAdapter = axios.defaults.adapter
afterEach(() => { axios.defaults.adapter = previousAdapter })

describe('access response handling', () => {
  it.each([
    { code: 40101, rejected: true, loginRequired: true },
    { code: 40101, rejected: false, loginRequired: true },
    { code: -1, rejected: true, loginRequired: false },
  ])('distinguishes access expiry from internal password errors ($code, $rejected)', async ({ code, rejected, loginRequired }) => {
    vi.resetModules()
    let requestHeader: unknown
    axios.defaults.adapter = async (config) => {
      requestHeader = config.headers.get('X-CICD-Request')
      const data = { code, message: 'validation failed' }
      if (rejected) throw { response: { status: 401, data } }
      return { data, status: 200, statusText: 'OK', headers: {}, config }
    }
    const listener = vi.fn()
    window.addEventListener('cicd-login-required', listener)
    try {
      const api = await import('./index')
      await expect(api.fetchProjects()).rejects.toMatchObject({ code })
      expect(requestHeader).toBe('1')
      expect(listener).toHaveBeenCalledTimes(loginRequired ? 1 : 0)
    } finally {
      window.removeEventListener('cicd-login-required', listener)
    }
  })
})
