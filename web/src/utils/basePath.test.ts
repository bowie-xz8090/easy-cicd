import { afterEach, describe, expect, it } from 'vitest'
import { getRuntimeBasePath } from './basePath'

afterEach(() => { document.head.innerHTML = ''; window.history.replaceState(null, '', '/') })
describe('runtime base path', () => {
  it('uses root assets when opening an arbitrary SPA page', () => {
    window.history.replaceState(null, '', '/history')
    const script = document.createElement('script'); script.src = '/assets/app.js'; document.head.append(script)
    expect(getRuntimeBasePath()).toBe('')
  })
  it('keeps a configured application prefix', () => {
    const script = document.createElement('script'); script.src = '/cicd/assets/app.js'; document.head.append(script)
    expect(getRuntimeBasePath()).toBe('/cicd')
  })
})
