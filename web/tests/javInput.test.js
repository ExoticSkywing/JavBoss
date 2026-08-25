import assert from 'node:assert/strict'
import test from 'node:test'
import { countJavInputLines, groupJavInputItems, JAV_INPUT_STATUS } from '../src/utils/javInput.js'

test('countJavInputLines ignores blank lines without changing source text', () => {
  assert.equal(countJavInputLines('  ABC-001 中文备注  \n\nDEF-002\r\n   '), 2)
})

test('groupJavInputItems exposes both de-duplication stages separately', () => {
  const items = [
    { id: 1, status: JAV_INPUT_STATUS.accepted },
    { id: 2, status: JAV_INPUT_STATUS.duplicateBatch },
    { id: 3, status: JAV_INPUT_STATUS.duplicateLibrary },
    { id: 4, status: JAV_INPUT_STATUS.duplicateHistory },
    { id: 5, status: JAV_INPUT_STATUS.invalid },
    { id: 6, status: JAV_INPUT_STATUS.note },
  ]
  const grouped = groupJavInputItems({
    items,
    library_duplicate_count: 1,
    history_duplicate_count: 1,
  })

  assert.deepEqual(
    grouped.firstStage.map((item) => item.id),
    [1, 3, 4]
  )
  assert.deepEqual(
    grouped.batchDuplicates.map((item) => item.id),
    [2]
  )
  assert.deepEqual(
    grouped.accepted.map((item) => item.id),
    [1]
  )
  assert.deepEqual(
    grouped.globalDuplicates.map((item) => item.id),
    [3, 4]
  )
  assert.deepEqual(
    grouped.invalid.map((item) => item.id),
    [5]
  )
  assert.deepEqual(
    grouped.notes.map((item) => item.id),
    [6]
  )
  assert.equal(grouped.globalDuplicateCount, 2)
})
