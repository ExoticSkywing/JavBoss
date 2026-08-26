import assert from 'node:assert/strict'
import test from 'node:test'
import { parseJavInventoryParam, writeJavInventoryParam } from '../src/utils/javInventory.js'

test('JAV inventory survives URL parameter parsing and serialization', () => {
  for (const inventory of ['pending', 'imported']) {
    const params = new URLSearchParams(`inventory=${inventory}`)
    assert.equal(parseJavInventoryParam(params), inventory)

    const serialized = new URLSearchParams()
    writeJavInventoryParam(serialized, inventory)
    assert.equal(serialized.toString(), `inventory=${inventory}`)
  }
})

test('JAV inventory defaults invalid or absent values to the unified collection', () => {
  assert.equal(parseJavInventoryParam(new URLSearchParams()), 'all')
  assert.equal(parseJavInventoryParam(new URLSearchParams('inventory=stored')), 'all')

  const params = new URLSearchParams('inventory=pending')
  writeJavInventoryParam(params, 'stored')
  assert.equal(params.toString(), '')
})

test('inventory is omitted when the current JAV view is not the work list', () => {
  const params = new URLSearchParams()
  writeJavInventoryParam(params, 'pending', false)
  assert.equal(params.toString(), '')
})
