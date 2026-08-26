export const JAV_INPUT_STATUS = {
  accepted: 'accepted',
  duplicateBatch: 'duplicate_batch',
  duplicateLibrary: 'duplicate_library',
  duplicateHistory: 'duplicate_history',
  invalid: 'invalid',
  note: 'note',
  cleared: 'cleared',
}

export const JAV_INPUT_RECEIPT_KIND = {
  none: 'none',
  partial: 'partial',
  all: 'all',
}

export function countJavInputLines(value) {
  return String(value || '')
    .split(/\r?\n/)
    .filter((line) => line.trim()).length
}

export function groupJavInputItems(batch) {
  const items = Array.isArray(batch?.items) ? batch.items : []
  return {
    items,
    batchDuplicates: items.filter((item) => item.status === JAV_INPUT_STATUS.duplicateBatch),
    firstStage: items.filter(
      (item) =>
        item.status !== JAV_INPUT_STATUS.duplicateBatch &&
        item.status !== JAV_INPUT_STATUS.invalid &&
        item.status !== JAV_INPUT_STATUS.note
    ),
    accepted: items.filter(
      (item) =>
        item.status === JAV_INPUT_STATUS.accepted || item.status === JAV_INPUT_STATUS.cleared
    ),
    globalDuplicates: items.filter(
      (item) =>
        item.status === JAV_INPUT_STATUS.duplicateLibrary ||
        item.status === JAV_INPUT_STATUS.duplicateHistory
    ),
    invalid: items.filter((item) => item.status === JAV_INPUT_STATUS.invalid),
    notes: items.filter((item) => item.status === JAV_INPUT_STATUS.note),
    globalDuplicateCount:
      Number(batch?.library_duplicate_count || 0) + Number(batch?.history_duplicate_count || 0),
  }
}

export function getJavInputReceipt(batch) {
  const uniqueCount = Math.max(0, Number(batch?.batch_unique_count) || 0)
  const addedCount = Math.min(uniqueCount, Math.max(0, Number(batch?.accepted_count) || 0))
  const existingCount = Math.max(0, uniqueCount - addedCount)
  const invalidItems = (Array.isArray(batch?.items) ? batch.items : []).filter(
    (item) => item?.status === JAV_INPUT_STATUS.invalid
  )

  let kind = JAV_INPUT_RECEIPT_KIND.none
  if (uniqueCount > 0 && addedCount === uniqueCount) {
    kind = JAV_INPUT_RECEIPT_KIND.all
  } else if (addedCount > 0) {
    kind = JAV_INPUT_RECEIPT_KIND.partial
  }

  return {
    kind,
    recognized: uniqueCount > 0,
    uniqueCount,
    addedCount,
    existingCount,
    invalidItems,
  }
}
