export const JAV_INVENTORY_ALL = 'all'
export const JAV_INVENTORY_PENDING = 'pending'
export const JAV_INVENTORY_IMPORTED = 'imported'

const javInventoryValues = new Set([
  JAV_INVENTORY_ALL,
  JAV_INVENTORY_PENDING,
  JAV_INVENTORY_IMPORTED,
])

export function normalizeJavInventory(value, fallback = JAV_INVENTORY_ALL) {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
  if (javInventoryValues.has(normalized)) return normalized
  return javInventoryValues.has(fallback) ? fallback : JAV_INVENTORY_ALL
}

export function parseJavInventoryParam(searchParams) {
  return normalizeJavInventory(searchParams?.get?.('inventory'))
}

export function writeJavInventoryParam(searchParams, value, enabled = true) {
  const normalized = normalizeJavInventory(value)
  searchParams?.delete?.('inventory')
  if (enabled && normalized !== JAV_INVENTORY_ALL) {
    searchParams?.set?.('inventory', normalized)
  }
  return normalized
}
