import { beforeEach, describe, expect, it } from 'vitest'
import { THEME_STORAGE_KEY, applyTheme, initTheme, theme, toggleTheme } from './theme'

describe('theme engine', () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.className = ''
    delete document.documentElement.dataset.theme
  })

  it('locks to dark sci-fi theme when localStorage is empty', () => {
    const mode = initTheme()
    expect(mode).toBe('dark')
    expect(theme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')
  })

  it('overrides legacy light storage and enforces dark sci-fi mode', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'light')
    expect(initTheme()).toBe('dark')
    expect(theme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')

    expect(toggleTheme()).toBe('dark')
    expect(applyTheme('dark')).toBe('dark')
  })
})
