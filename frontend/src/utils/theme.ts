import { ref } from 'vue'

export type ThemeMode = 'dark'

export const THEME_STORAGE_KEY = 'ai-bot-theme'
export const theme = ref<ThemeMode>('dark')

export function applyTheme(_mode: string = 'dark'): ThemeMode {
  theme.value = 'dark'
  if (typeof document !== 'undefined') {
    const root = document.documentElement
    root.dataset.theme = 'dark'
    root.classList.add('dark')
    root.style.colorScheme = 'dark'
  }
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      window.localStorage.setItem(THEME_STORAGE_KEY, 'dark')
    }
  } catch {
    // 忽略存储写入异常
  }
  return 'dark'
}

export function initTheme(): ThemeMode {
  return applyTheme('dark')
}

export function toggleTheme(): ThemeMode {
  return applyTheme('dark')
}
