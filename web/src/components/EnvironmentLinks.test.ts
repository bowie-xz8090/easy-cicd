import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import EnvironmentLinks from './EnvironmentLinks.vue'
import { fetchEnvironments, type Environment } from '../api'
import { groupEnvironmentLinks } from '../utils/environmentLinks'

vi.mock('../api', () => ({ fetchEnvironments: vi.fn() }))

const environment: Environment = {
  key: 'dev', label: '开发环境', disabled: false, need_password: false, targets: [],
  user_url: 'https://example.com/user/', admin_url: '',
  extra: [
    { label: '渠道管理后台', url: 'https://example.com/channel/' },
    { label: '词元交易', url: 'https://example.com/old-trade/' },
    { label: 'OIDC登录', url: 'https://example.com/login/' },
    { label: 'OIDC后台', url: 'https://example.com/admin/' },
    { label: 'Nacos', url: 'https://example.com/nacos/' },
  ],
}
const testingEnvironment: Environment = {
  ...environment, key: 'sit', label: '测试环境',
  user_url: 'https://test.example.com/user/',
  extra: [{ label: 'Nacos', url: 'http://192.0.2.10:8848/nacos/', proxy_url: '/cicd/proxy/sit/nacos/' }],
}
const productionEnvironment: Environment = {
  ...environment, key: 'prod', label: '生产环境',
  user_url: 'https://production.example.com/user/', extra: null,
}
let wrapper: ReturnType<typeof mount> | undefined
beforeEach(() => vi.mocked(fetchEnvironments).mockResolvedValue([environment]))
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('environment links', () => {
  it('uses server-registered proxy launchers while leaving direct links unchanged', () => {
    const groups = groupEnvironmentLinks({ ...environment, extra: [{ label: 'Nacos', url: 'http://192.0.2.10:8848/nacos/', proxy_url: '/cicd/proxy/sit/nacos/' }] })
    expect(groups[0].links[0].url).toBe(environment.user_url)
    expect(groups[3].links.find(link => link.label === 'Nacos')?.url).toBe('/cicd/proxy/sit/nacos/')
  })
  it('groups every requested entry and normalizes old labels without changing configured URLs', () => {
    const groups = groupEnvironmentLinks(environment)
    expect(groups.map(group => [group.label, group.links.map(link => link.label)])).toEqual([
      ['大赛项目', ['用户端', '管理端', '广州征集', '数据要素X', '数超官网']],
      ['词元项目', ['词元官网', '词元管理端', '产业平台', '渠道管理端', '查询中心', '词元交易旧版']],
      ['统一登录', ['OIDC登录页', 'OIDC管理端']],
      ['运维项目', ['Grafana', 'Prometheus', 'Nacos']],
    ])
    expect(groups[1].links.find(link => link.label === '渠道管理端')?.url).toBe('https://example.com/channel/')
    expect(groups[2].links[0].url).toBe('https://example.com/login/')
    expect(groups[3].links[1].url).toBe('')
    expect(groups[3].links[2].url).toBe('https://example.com/nacos/')
  })

  it('retains custom links and handles absent extra links', () => {
    expect(groupEnvironmentLinks({ ...environment, extra: null })).toHaveLength(4)
    const groups = groupEnvironmentLinks({ ...environment, extra: [{ label: '文档', url: 'https://example.com/docs/' }] })
    expect(groups[4]).toEqual({ label: '其他', links: [{ label: '文档', url: 'https://example.com/docs/' }] })
  })

  it('starts in development and shows only links for the selected environment', async () => {
    vi.mocked(fetchEnvironments).mockResolvedValue([testingEnvironment, productionEnvironment, environment])
    wrapper = mount(EnvironmentLinks)
    await flushPromises()

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs).toHaveLength(3)
    expect(tabs[2].attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[role="status"]').text()).toBe('当前：开发环境')
    expect(wrapper.get('[role="tabpanel"]').attributes('aria-labelledby')).toBe(tabs[2].attributes('id'))
    expect(wrapper.findAll('fieldset legend').map(legend => legend.text())).toEqual(['大赛项目', '词元项目', '统一登录', '运维项目'])
    expect(wrapper.findAll('.env-link-btn')).toHaveLength(16)

    await tabs[0].trigger('click')
    const userLink = wrapper.findAll('a').find(link => link.text() === '用户端')!
    expect(userLink.attributes('href')).toBe(testingEnvironment.user_url)
    expect(userLink.attributes('target')).toBe('_blank')
    expect(userLink.attributes('rel')).toBe('noopener noreferrer')
    expect(userLink.attributes('aria-label')).toContain('测试环境')
    expect(wrapper.get('[role="status"]').text()).toBe('当前：测试环境')
    expect(wrapper.findAll('a').some(link => link.attributes('href') === environment.user_url)).toBe(false)

    await tabs[1].trigger('click')
    expect(wrapper.get('[role="status"]').text()).toBe('当前：生产环境')
    expect(wrapper.findAll('a').find(link => link.text() === '用户端')?.attributes('href')).toBe(productionEnvironment.user_url)
  })

  it('supports keyboard switching with a single focusable environment tab', async () => {
    vi.mocked(fetchEnvironments).mockResolvedValue([environment, testingEnvironment, productionEnvironment])
    wrapper = mount(EnvironmentLinks, { attachTo: document.body })
    await flushPromises()
    const tabs = wrapper.findAll('[role="tab"]')

    await tabs[0].trigger('keydown', { key: 'ArrowRight' })
    expect(tabs[1].attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(tabs[1].element)
    expect(tabs.map(tab => tab.attributes('tabindex'))).toEqual(['-1', '0', '-1'])
    await tabs[1].trigger('keydown', { key: 'End' })
    expect(tabs[2].attributes('aria-selected')).toBe('true')
    await tabs[2].trigger('keydown', { key: 'ArrowRight' })
    expect(tabs[0].attributes('aria-selected')).toBe('true')
    await tabs[0].trigger('keydown', { key: 'ArrowLeft' })
    expect(tabs[2].attributes('aria-selected')).toBe('true')
    await tabs[2].trigger('keydown', { key: 'Home' })
    expect(tabs[0].attributes('aria-selected')).toBe('true')
  })

  it('keeps the search when switching environments and uses the matching proxy link', async () => {
    vi.mocked(fetchEnvironments).mockResolvedValue([environment, testingEnvironment])
    wrapper = mount(EnvironmentLinks)
    await flushPromises()
    const search = wrapper.get('input[type="search"]')
    await search.setValue('  nAcOs  ')
    expect(wrapper.findAll('.env-link-btn')).toHaveLength(1)
    expect(wrapper.get('legend').text()).toBe('运维项目')
    expect(wrapper.get('.env-links-context .env-link-count').text()).toBe('1 个入口')

    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.get('a.env-link-btn').attributes('href')).toBe('/cicd/proxy/sit/nacos/')
    await search.setValue('不存在的入口')
    expect(wrapper.findAll('.env-link-btn')).toHaveLength(0)
    expect(wrapper.get('.env-links-empty').text()).toBe('没有找到匹配的入口')
    await search.setValue('词元项目')
    expect(wrapper.findAll('.env-link-btn')).toHaveLength(6)
    expect(wrapper.get('legend').text()).toBe('词元项目')
    await search.setValue('')
    expect(wrapper.findAll('.env-link-btn')).toHaveLength(16)
    expect(wrapper.find('.env-links-empty').exists()).toBe(false)
  })

  it('uses the first configured environment when development is absent', async () => {
    vi.mocked(fetchEnvironments).mockResolvedValue([testingEnvironment])
    wrapper = mount(EnvironmentLinks)
    await flushPromises()
    expect(wrapper.get('[role="tab"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[role="status"]').text()).toBe('当前：测试环境')
    expect(wrapper.findAll('a').find(link => link.text() === '用户端')?.attributes('href')).toBe(testingEnvironment.user_url)
  })

  it('opens configured URLs and shows a dismissible dialog for missing entries', async () => {
    wrapper = mount(EnvironmentLinks, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.findAll('.env-link-group')).toHaveLength(4)
    expect(wrapper.findAll('a').find(link => link.text() === '用户端')?.attributes('href')).toBe(environment.user_url)
    const unavailable = wrapper.findAll('button.unavailable').find(button => button.text() === 'Prometheus')!
    await unavailable.trigger('click')
    expect(wrapper.get('[role="alertdialog"]').text()).toContain('暂未部署')
    expect(wrapper.get('#unavailable-message').text()).toContain('开发环境的「Prometheus」')
    expect(document.activeElement).toBe(wrapper.get('.dialog-close').element)
    await wrapper.get('.dialog-close').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="alertdialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(unavailable.element)
  })
})
