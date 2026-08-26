import { zh } from '@/utils/i18n'

export function getIdolDisplayName(item) {
  return getIdolDisplayNames(item).primaryName
}

export function getIdolDisplayNames(item) {
  // Keep the Japanese stage name as the stable card label and show the Chinese
  // name only as supporting text. Aliases remain available for search/merging,
  // but do not compete with the canonical names in the card title.
  const primaryName = firstNonEmpty(
    item?.japanese_name,
    item?.name,
    item?.chinese_name,
    zh('未知女优', 'Unknown idol')
  )
  const chineseName = String(item?.chinese_name || '').trim()
  return {
    primaryName,
    secondaryName: chineseName && chineseName !== primaryName ? chineseName : '',
  }
}

function firstNonEmpty(...values) {
  for (const value of values) {
    const trimmed = String(value || '').trim()
    if (trimmed) return trimmed
  }
  return ''
}
