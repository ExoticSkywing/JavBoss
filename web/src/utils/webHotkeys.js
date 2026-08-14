export const WEB_HOTKEY_ACTIONS = [
  { action: 'content_page_up', defaultKey: 'w' },
  { action: 'content_page_down', defaultKey: 's' },
  { action: 'previous_page', defaultKey: 'a' },
  { action: 'next_page', defaultKey: 'd' },
  { action: 'browser_back', defaultKey: '1' },
  { action: 'browser_forward', defaultKey: '2' },
]

const RESERVED_KEYS = new Set(['Alt', 'Control', 'Meta', 'Shift', 'Escape', 'Tab'])

export function normalizeWebHotkeyKey(value) {
  const rawKey = String(value ?? '')
  if (rawKey === ' ' || rawKey === 'Spacebar') return 'Space'
  const key = rawKey.trim()
  if (!key) return ''
  return key.length === 1 ? key.toLowerCase() : key
}

export function webHotkeyKeyId(value) {
  return normalizeWebHotkeyKey(value).toLocaleLowerCase()
}

export function isAllowedWebHotkeyKey(value) {
  const key = normalizeWebHotkeyKey(value)
  return Boolean(key) && key.length <= 32 && !RESERVED_KEYS.has(key)
}

export function defaultWebHotkeys() {
  return WEB_HOTKEY_ACTIONS.map(({ action, defaultKey }) => ({ action, key: defaultKey }))
}

export function parseWebHotkeys(value) {
  const defaults = defaultWebHotkeys()
  let parsed = value
  if (typeof value === 'string') {
    try {
      parsed = JSON.parse(value)
    } catch {
      parsed = null
    }
  }

  if (!Array.isArray(parsed) || parsed.length !== WEB_HOTKEY_ACTIONS.length) return defaults

  const configured = new Map()
  const usedKeys = new Set()
  for (const item of parsed) {
    const action = String(item?.action || '')
    const key = normalizeWebHotkeyKey(item?.key)
    const keyId = webHotkeyKeyId(key)
    if (
      !WEB_HOTKEY_ACTIONS.some((entry) => entry.action === action) ||
      configured.has(action) ||
      !isAllowedWebHotkeyKey(key) ||
      usedKeys.has(keyId)
    ) {
      return defaults
    }
    configured.set(action, key)
    usedKeys.add(keyId)
  }

  return WEB_HOTKEY_ACTIONS.map(({ action }) => ({ action, key: configured.get(action) }))
}

export function webHotkeysEqual(left, right) {
  const leftItems = parseWebHotkeys(left)
  const rightItems = parseWebHotkeys(right)
  return leftItems.every(
    (item, index) =>
      item.action === rightItems[index]?.action && item.key === rightItems[index]?.key
  )
}

export function formatWebHotkeyKey(value) {
  const key = normalizeWebHotkeyKey(value)
  return key.length === 1 ? key.toUpperCase() : key
}

export function isWebHotkeyEditingTarget(target) {
  if (!(target instanceof Element)) return false
  if (target.closest('textarea, select, [contenteditable]:not([contenteditable="false"])')) {
    return true
  }

  const input = target.closest('input')
  if (!input) return false
  return !['button', 'checkbox', 'color', 'file', 'radio', 'range', 'reset', 'submit'].includes(
    String(input.type || 'text').toLowerCase()
  )
}
