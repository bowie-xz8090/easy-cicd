import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import HistoryPage from './HistoryPage.vue'
import { fetchEnvironments, fetchRecords, fetchTaskStatus, type DeployRecord } from '../api'

vi.mock('../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api')>()
  return { ...actual, fetchEnvironments: vi.fn(), fetchRecords: vi.fn(), fetchTaskStatus: vi.fn() }
})

let wrapper: ReturnType<typeof mount> | undefined
const first: DeployRecord = {
  id: 'one', project_owner: 'org', project_name: 'app', project_label: '项目',
  component: 'web', component_label: '', sub_project: '', sub_project_label: '',
  target: 'dev-server', target_label: '', server: '', branch: 'main',
  commit_message: '😀'.repeat(101), environment: 'dev', status: 'building',
  operator: '182****2780', operator_type: 'oidc', created_at: '2026-11-05T00:00:00Z', finished_at: '',
}

beforeEach(() => {
  vi.resetAllMocks(); vi.useFakeTimers()
  vi.mocked(fetchEnvironments).mockResolvedValue([{ key: 'dev', label: '开发环境', disabled: false, need_password: false, targets: [], user_url: '', admin_url: '', extra: [] }])
  vi.mocked(fetchRecords).mockResolvedValue({ records: [{ ...first }, { ...first, id: 'two', operator: '192.0.2.10', operator_type: 'ip' }], total: 2 })
  vi.mocked(fetchTaskStatus).mockResolvedValue({ task_id: 'one', status: 'success', project_name: 'app', branch: 'main', environment: 'dev', created_at: '', updated_at: '', finished_at: '2026-11-05T00:03:21Z' })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers() })

describe('deployment history', () => {
  it('uses the requested column order and filter labels and shows masked actors', async () => {
    wrapper = mount(HistoryPage, { global: { stubs: { LogViewer: { template: '<div>部署日志</div>' } } } })
    await flushPromises()
    expect(wrapper.findAll('thead th').map(node => node.text())).toEqual(['部署环境', '项目名称', '子项目名称', '分支/Tag', '最后 commit', '部署目标', '状态', '操作人', '触发时间'])
    expect(wrapper.get('label[for="filter-sub-project"]').text()).toBe('子项目关键字')
    expect(wrapper.get('label[for="filter-env"]').text()).toBe('部署环境')
    expect(wrapper.text()).toContain('182****2780')
    expect(wrapper.text()).toContain('192.0.2.10')
    expect(wrapper.text()).not.toContain('18223452780')
    expect(wrapper.findAll('button').some(button => button.text().includes('清空'))).toBe(false)
    await wrapper.findAll('.record-row')[0].trigger('click')
    expect(wrapper.get('.log-row td').attributes('colspan')).toBe('9')
  })

  it('expands each commit independently without opening logs or resetting during polling', async () => {
    wrapper = mount(HistoryPage, { global: { stubs: { LogViewer: { template: '<div>部署日志</div>' } } } })
    await flushPromises()
    await wrapper.findAll('.commit-toggle')[0].trigger('click')
    expect(wrapper.findAll('.commit-toggle').map(node => node.text())).toEqual(['收起', '更多'])
    expect(wrapper.find('.log-row').exists()).toBe(false)
    expect(Array.from(wrapper.findAll('.commit-message span')[0].text())).toHaveLength(101)
    await vi.advanceTimersByTimeAsync(5000)
    expect(fetchTaskStatus).toHaveBeenCalled()
    expect(wrapper.findAll('.commit-toggle').map(node => node.text())).toEqual(['收起', '更多'])
    await wrapper.findAll('.commit-toggle')[0].trigger('click')
    expect(Array.from(wrapper.findAll('.commit-message span')[0].text())).toHaveLength(50)
  })

  it('leaves running end times blank and fills them from polling without remounting rows', async () => {
    wrapper = mount(HistoryPage)
    await flushPromises()
    const firstRow = wrapper.findAll('.record-row')[0].element
    expect(wrapper.findAll('.finished-time').map(node => node.text())).toEqual(['', ''])
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.findAll('.record-row')[0].element).toBe(firstRow)
    expect(wrapper.findAll('.finished-time')[0].text()).toBe(new Date('2026-11-05T00:03:21Z').toLocaleString('zh-CN'))
  })

  it.each(['success', 'failed'])('shows persisted end times for terminal tasks (%s)', async status => {
    const end = '2026-11-05T00:30:00Z'
    vi.mocked(fetchRecords).mockResolvedValue({ records: [{ ...first, status, finished_at: end }], total: 1 })
    wrapper = mount(HistoryPage)
    await flushPromises()
    expect(wrapper.get('.deployment-time').text()).toContain(new Date(first.created_at).toLocaleString('zh-CN'))
    expect(wrapper.get('.finished-time').text()).toBe(new Date(end).toLocaleString('zh-CN'))
  })
})
