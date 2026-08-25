export const JAV_INPUT_STATUS = {
  accepted: 'accepted',
  duplicateBatch: 'duplicate_batch',
  duplicateLibrary: 'duplicate_library',
  duplicateHistory: 'duplicate_history',
  invalid: 'invalid',
  note: 'note',
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
    accepted: items.filter((item) => item.status === JAV_INPUT_STATUS.accepted),
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
