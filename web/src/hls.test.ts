import assert from 'node:assert/strict'
import { test } from 'node:test'
import { hlsMode } from './hls.ts'

test('hlsMode prefers hls.js when MediaSource works, even if canPlayType says maybe', () => {
  assert.equal(hlsMode(true, 'maybe'), 'hlsjs')
  assert.equal(hlsMode(true, ''), 'hlsjs')
})

test('hlsMode uses native HLS only when MediaSource is missing', () => {
  assert.equal(hlsMode(false, 'maybe'), 'native')
  assert.equal(hlsMode(false, 'probably'), 'native')
})

test('hlsMode gives up when neither path exists', () => {
  assert.equal(hlsMode(false, ''), 'none')
})
