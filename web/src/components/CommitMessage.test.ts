import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import CommitMessage from './CommitMessage.vue'

describe('CommitMessage', () => {
  it.each([49, 50, 51])('supports the 50-character history limit (%i)', async length => {
    const message = '😀'.repeat(length)
    const wrapper = mount(CommitMessage, { props: { message, limit: 50 } })
    expect(wrapper.find('button').exists()).toBe(length > 50)
    expect(Array.from(wrapper.get('span').text())).toHaveLength(Math.min(50, length))
    if (length > 50) {
      await wrapper.get('button').trigger('click')
      expect(wrapper.get('span').text()).toBe(message)
      await wrapper.get('button').trigger('click')
      expect(Array.from(wrapper.get('span').text())).toHaveLength(50)
    }
    wrapper.unmount()
  })

  it.each([99, 100, 101])('folds only messages longer than 100 characters (%i)', async (length) => {
    const message = '中'.repeat(length)
    const wrapper = mount(CommitMessage, { props: { message } })
    expect(wrapper.find('button').exists()).toBe(length > 100)
    if (length <= 100) {
      expect(wrapper.text()).toBe(message)
    } else {
      expect(wrapper.find('span').text()).toBe('中'.repeat(100))
      expect(wrapper.text()).toContain('…更多')
      await wrapper.get('button').trigger('click')
      expect(wrapper.get('span').text()).toBe(message)
      expect(wrapper.get('button').text()).toBe('收起')
      await wrapper.get('button').trigger('click')
      expect(wrapper.get('span').text()).toBe('中'.repeat(100))
    }
    wrapper.unmount()
  })

  it('counts emoji as Unicode characters and supports empty messages', async () => {
    const wrapper = mount(CommitMessage, { props: { message: '😀'.repeat(101) } })
    expect(Array.from(wrapper.get('span').text())).toHaveLength(100)
    await wrapper.setProps({ message: '' })
    expect(wrapper.text()).toBe('-')
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
