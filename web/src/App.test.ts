import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { fetchEditableConfig, fetchSession, fetchSiteInfo } from './api'
import { accessSession, accessLoading, accessError } from './auth'

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return { ...actual, fetchSession: vi.fn(), fetchSiteInfo: vi.fn(), fetchEditableConfig: vi.fn() }
})

const stubs = {
  DeployPage: { template: '<div data-test="deploy">部署内容</div>' },
  HistoryPage: { template: '<div>历史内容</div>' },
  EnvironmentLinks: true,
  ConfigPage: true,
}
let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  vi.resetAllMocks()
  accessSession.value = null
  accessLoading.value = true
  accessError.value = ''
  window.history.replaceState(null, '', '/')
  vi.mocked(fetchSiteInfo).mockResolvedValue({ title: 'CICD' })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers() })

describe('access gate', () => {
  it('shows a centered login card and does not mount business views for anonymous clients', async () => {
    vi.mocked(fetchSession).mockResolvedValue({ authenticated: false, mode: 'anonymous', display_name: '', expires_at: null })
    wrapper = mount(App, { global: { stubs } })
    expect(wrapper.find('[data-test="deploy"]').exists()).toBe(false)
    await flushPromises()
    expect(wrapper.find('.login-card').text()).toContain('登录后才可操作')
    expect(wrapper.get('a.login-button').attributes('href')).toContain('/api/auth/oidc/login')
    expect(wrapper.find('nav').exists()).toBe(false)
    expect(wrapper.find('[data-test="deploy"]').exists()).toBe(false)
  })

  it.each([
    { mode: 'ip' as const, display_name: '192.0.2.1' },
    { mode: 'oidc' as const, display_name: '182****2780' },
  ])('shows the server-provided $mode identity', async (identity) => {
    vi.mocked(fetchSession).mockResolvedValue({ authenticated: true, ...identity, expires_at: null })
    wrapper = mount(App, { global: { stubs } }); await flushPromises()
    expect(wrapper.get('.current-user').text()).toBe(identity.display_name)
    expect(wrapper.find('[data-test="deploy"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('退出')
    expect(wrapper.text()).not.toContain('18223452780')
    window.dispatchEvent(new Event('cicd-login-required')); await flushPromises()
    expect(wrapper.find('.login-card').exists()).toBe(true)
    expect(wrapper.find('[data-test="deploy"]').exists()).toBe(false)
  })

  it('keeps internal admin password failures inside their existing dialog', async () => {
    vi.mocked(fetchSession).mockResolvedValue({ authenticated: true, mode: 'ip', display_name: '192.0.2.1', expires_at: null })
    vi.mocked(fetchEditableConfig).mockRejectedValue(new Error('admin_token 无效'))
    wrapper = mount(App, { global: { stubs } }); await flushPromises()
    await wrapper.get('nav button:last-child').trigger('click')
    await wrapper.get('#admin-token-input').setValue('wrong')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('.dialog-error').text()).toContain('admin_token 无效')
    expect(wrapper.find('.login-card').exists()).toBe(false)
  })

  it('expires a local session on time without requiring a page reload', async () => {
    vi.useFakeTimers()
    vi.mocked(fetchSession).mockResolvedValue({ authenticated: true, mode: 'oidc', display_name: '182****2780', expires_at: new Date(Date.now() + 2000).toISOString() })
    wrapper = mount(App, { global: { stubs } }); await flushPromises()
    expect(wrapper.find('.current-user').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.find('.login-card').exists()).toBe(true)
  })
})
