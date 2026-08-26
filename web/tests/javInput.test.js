import assert from 'node:assert/strict'
import test from 'node:test'
import {
  countJavInputLines,
  getJavInputReceipt,
  groupJavInputItems,
  JAV_INPUT_RECEIPT_KIND,
  JAV_INPUT_STATUS,
} from '../src/utils/javInput.js'

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
    { id: 7, status: JAV_INPUT_STATUS.cleared },
  ]
  const grouped = groupJavInputItems({
    items,
    library_duplicate_count: 1,
    history_duplicate_count: 1,
  })

  assert.deepEqual(
    grouped.firstStage.map((item) => item.id),
    [1, 3, 4, 7]
  )
  assert.deepEqual(
    grouped.batchDuplicates.map((item) => item.id),
    [2]
  )
  assert.deepEqual(
    grouped.accepted.map((item) => item.id),
    [1, 7]
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

test('getJavInputReceipt reports the three user-facing incremental outcomes', () => {
  const cases = [
    {
      batch: { batch_unique_count: 3, accepted_count: 0 },
      expected: {
        kind: JAV_INPUT_RECEIPT_KIND.none,
        recognized: true,
        uniqueCount: 3,
        addedCount: 0,
        existingCount: 3,
        invalidItems: [],
      },
    },
    {
      batch: { batch_unique_count: 3, accepted_count: 1 },
      expected: {
        kind: JAV_INPUT_RECEIPT_KIND.partial,
        recognized: true,
        uniqueCount: 3,
        addedCount: 1,
        existingCount: 2,
        invalidItems: [],
      },
    },
    {
      batch: { batch_unique_count: 3, accepted_count: 3 },
      expected: {
        kind: JAV_INPUT_RECEIPT_KIND.all,
        recognized: true,
        uniqueCount: 3,
        addedCount: 3,
        existingCount: 0,
        invalidItems: [],
      },
    },
  ]

  for (const { batch, expected } of cases) {
    assert.deepEqual(getJavInputReceipt(batch), expected)
  }
})

test('getJavInputReceipt only exposes suspicious unrecognized input as warnings', () => {
  const invalid = { id: 1, status: JAV_INPUT_STATUS.invalid, raw_line: '番号 123?' }
  const receipt = getJavInputReceipt({
    batch_unique_count: 1,
    accepted_count: 1,
    items: [
      invalid,
      { id: 2, status: JAV_INPUT_STATUS.note, raw_line: '收藏清单' },
      { id: 3, status: JAV_INPUT_STATUS.duplicateBatch, code: 'ABC-001' },
    ],
  })

  assert.deepEqual(receipt.invalidItems, [invalid])
})
