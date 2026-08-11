export const THEME_STORAGE_KEY = 'javboss-color-theme'
export const LIGHT_THEME = 'light'
export const DARK_THEME = 'dark'

const normalizeTheme = (theme) => (theme === DARK_THEME ? DARK_THEME : LIGHT_THEME)

export function getStoredTheme() {
  try {
    return normalizeTheme(window.localStorage.getItem(THEME_STORAGE_KEY))
  } catch {
    return LIGHT_THEME
  }
}

export function applyTheme(theme) {
  const nextTheme = normalizeTheme(theme)
  const dark = nextTheme === DARK_THEME
  const root = document.documentElement

  root.classList.toggle('dark', dark)
  root.dataset.theme = nextTheme
  root.style.colorScheme = nextTheme

  const themeColor = document.querySelector('meta[name="theme-color"]')
  themeColor?.setAttribute('content', dark ? '#09090b' : '#ffffff')

  return nextTheme
}

export function saveTheme(theme) {
  const nextTheme = applyTheme(theme)
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, nextTheme)
  } catch {
    // The theme can still be applied when storage is unavailable.
  }
  return nextTheme
}

export function initializeTheme() {
  return applyTheme(getStoredTheme())
}
