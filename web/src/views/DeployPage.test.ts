import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DeployPage from './DeployPage.vue'
import { fetchBranches, fetchEnvironments, fetchProjects, fetchTags, type Branch, type Project } from '../api'

vi.mock('../api', () => ({
  fetchBranches: vi.fn(), fetchEnvironments: vi.fn(), fetchProjects: vi.fn(),
  fetchTags: vi.fn(), triggerDeploy: vi.fn(),
}))

const projects: Project[] = ['app-one', 'app-two'].map(name => ({
  owner: 'org', name, full_name: `org/${name}`, label: name, mode: 'single',
  selectable: true, components: [], clone_url: `https://example.com/${name}.git`,
}))
function refs(count: number, message = '提交说明'): Branch[] {
  return Array.from({ length: count }, (_, index) => ({ name: `ref-${index}`, commit_id: `commit-${index}`, commit_message: message }))
}
let wrapper: ReturnType<typeof mount> | undefined
async function selectProject(name = 'app-one') {
  await wrapper!.get('#project-cascader').trigger('click')
  const option = wrapper!.findAll('.cascader-option').find(node => node.text().includes(name))!
  await option.trigger('click')
  await flushPromises()
}
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(fetchProjects).mockResolvedValue(projects)
  vi.mocked(fetchEnvironments).mockResolvedValue([])
  vi.mocked(fetchBranches).mockResolvedValue([])
  vi.mocked(fetchTags).mockResolvedValue([])
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('deployment selection', () => {
  it.each([0, 1, 2])('selects a branch automatically only when exactly one is available (%i)', async count => {
    vi.mocked(fetchBranches).mockResolvedValue(refs(count))
    wrapper = mount(DeployPage)
    await flushPromises()
    await selectProject()
    expect((wrapper.get('#ref-select').element as HTMLSelectElement).value).toBe(count === 1 ? 'ref-0' : '')
    expect(wrapper.find('.commit-card').exists()).toBe(count === 1)
    expect(wrapper.get('.page-heading button').text()).toBe('刷新')
  })

  it.each([0, 1, 2])('applies the same rule after switching to tags (%i)', async count => {
    vi.mocked(fetchBranches).mockResolvedValue(refs(1))
    vi.mocked(fetchTags).mockResolvedValue(refs(count))
    wrapper = mount(DeployPage)
    await flushPromises()
    await selectProject()
    await wrapper.get('input[value="tag"]').setValue(true)
    await flushPromises()
    expect(fetchTags).toHaveBeenCalledWith('org', 'app-one')
    expect((wrapper.get('#ref-select').element as HTMLSelectElement).value).toBe(count === 1 ? 'ref-0' : '')
  })

  it.each([99, 100, 101])('folds the selected commit only beyond 100 Unicode characters (%i)', async length => {
    vi.mocked(fetchBranches).mockResolvedValue(refs(1, '😀'.repeat(length)))
    wrapper = mount(DeployPage)
    await flushPromises()
    await selectProject()
    expect(wrapper.find('.commit-toggle').exists()).toBe(length > 100)
    expect(Array.from(wrapper.get('.commit-message span').text())).toHaveLength(Math.min(length, 100))
    if (length > 100) {
      await wrapper.get('.commit-toggle').trigger('click')
      expect(Array.from(wrapper.get('.commit-message span').text())).toHaveLength(101)
      await wrapper.get('.commit-toggle').trigger('click')
      expect(Array.from(wrapper.get('.commit-message span').text())).toHaveLength(100)
    }
  })

  it('ignores an old project response after the user selects another project', async () => {
    let resolveOld!: (value: Branch[]) => void
    vi.mocked(fetchBranches)
      .mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce([{ name: 'current', commit_id: 'new', commit_message: '当前项目' }])
    wrapper = mount(DeployPage)
    await flushPromises()
    await selectProject()
    await selectProject('app-two')
    expect((wrapper.get('#ref-select').element as HTMLSelectElement).value).toBe('current')
    resolveOld([{ name: 'outdated', commit_id: 'old', commit_message: '旧项目' }])
    await flushPromises()
    expect((wrapper.get('#ref-select').element as HTMLSelectElement).value).toBe('current')
    expect(wrapper.get('.commit-card').text()).toContain('当前项目')
  })
})
