import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ConfigPage from './ConfigPage.vue'
import { fetchEditableConfig, updateWhitelist, clearDeployHistory } from '../api'
import { refreshSession } from '../auth'

vi.mock('../api', () => ({ fetchEditableConfig: vi.fn(), updateWhitelist: vi.fn(), clearDeployHistory: vi.fn(), updateEnvironmentAccess: vi.fn(), updateProjectBuildConfig: vi.fn() }))
vi.mock('../auth', () => ({ refreshSession: vi.fn() }))

let wrapper: ReturnType<typeof mount> | undefined
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(fetchEditableConfig).mockResolvedValue({ whitelist: ['192.0.2.1', '18223452780'], environments: {}, projects: {}, servers: [] })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks() })

describe('whitelist configuration', () => {
  it('clears history only from configuration and sends the existing admin credential', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.mocked(clearDeployHistory).mockResolvedValue({ deleted: 3 })
    wrapper = mount(ConfigPage, { props: { adminToken: 'test-admin' } })
    await flushPromises()
    await wrapper.get('.clear-history-btn').trigger('click')
    await flushPromises()
    expect(clearDeployHistory).toHaveBeenCalledWith('test-admin')
    expect(wrapper.get('[role="status"]').text()).toContain('已清空 3 条部署记录')
  })

  it('does not clear history when cancelled or when the admin credential is absent', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    wrapper = mount(ConfigPage, { props: { adminToken: 'test-admin' } })
    await flushPromises()
    await wrapper.get('.clear-history-btn').trigger('click')
    expect(clearDeployHistory).not.toHaveBeenCalled()
    await wrapper.setProps({ adminToken: '' })
    expect((wrapper.get('.clear-history-btn').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows an active-task rejection without reporting success', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.mocked(clearDeployHistory).mockRejectedValue(new Error('存在进行中的部署任务'))
    wrapper = mount(ConfigPage, { props: { adminToken: 'test-admin' } })
    await flushPromises()
    await wrapper.get('.clear-history-btn').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('进行中的部署任务')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('shows one entry per line and saves a trimmed list with the admin credential', async () => {
    vi.mocked(updateWhitelist).mockResolvedValue({ whitelist: ['192.0.2.2', '18223452780'] })
    wrapper = mount(ConfigPage, { props: { adminToken: 'test-admin' } })
    await flushPromises()
    expect((wrapper.get('#access-whitelist').element as HTMLTextAreaElement).value).toBe('192.0.2.1\n18223452780')
    await wrapper.get('#access-whitelist').setValue(' 192.0.2.2 \n\n18223452780\n192.0.2.2\n')
    await wrapper.get('.whitelist-form').trigger('submit')
    await flushPromises()
    expect(updateWhitelist).toHaveBeenCalledWith('test-admin', ['192.0.2.2', '18223452780'])
    expect(wrapper.get('[role="status"]').text()).toContain('已写入 config.yaml 并生效')
    expect(refreshSession).toHaveBeenCalledOnce()
  })

  it('preserves the input and shows server-side validation errors without refreshing access', async () => {
    vi.mocked(updateWhitelist).mockRejectedValue(new Error('第 1 项必须为有效 IP 或 11 位手机号'))
    wrapper = mount(ConfigPage, { props: { adminToken: 'test-admin' } })
    await flushPromises()
    await wrapper.get('#access-whitelist').setValue('invalid')
    await wrapper.get('.whitelist-form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('11 位手机号')
    expect((wrapper.get('#access-whitelist').element as HTMLTextAreaElement).value).toBe('invalid')
    expect(refreshSession).not.toHaveBeenCalled()
  })

  it('can explicitly save an empty list', async () => {
    vi.mocked(updateWhitelist).mockResolvedValue({ whitelist: [] })
    wrapper = mount(ConfigPage, { props: { adminToken: 'test-admin' } })
    await flushPromises()
    await wrapper.get('#access-whitelist').setValue(' \n')
    await wrapper.get('.whitelist-form').trigger('submit')
    await flushPromises()
    expect(updateWhitelist).toHaveBeenCalledWith('test-admin', [])
  })
})
