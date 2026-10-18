// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { applyEffects, applySavedEffects, effectsOff, savedEffectsOff, setEffectsOff } from './motion.ts'

type FakeDocument = { documentElement: { dataset: Record<string, string> } }

// The browser globals the motion helpers use. Node has neither, so each test
// gets a fresh pair and puts the real globals back afterwards.
function withBrowserEnv(run: (store: Map<string, string>) => void) {
  const store = new Map<string, string>()
  const doc: FakeDocument = { documentElement: { dataset: {} } }
  const beforeDocument = Object.getOwnPropertyDescriptor(globalThis, 'document')
  const beforeStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'document', { value: doc, configurable: true })
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => store.set(key, value),
    },
    configurable: true,
  })
  try {
    run(store)
  } finally {
    if (beforeDocument) Object.defineProperty(globalThis, 'document', beforeDocument)
    else Reflect.deleteProperty(globalThis, 'document')
    if (beforeStorage) Object.defineProperty(globalThis, 'localStorage', beforeStorage)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
}

test('reduced effects are off only when this browser saved that choice', () => {
  assert.equal(effectsOff('1'), true)
  assert.equal(effectsOff('0'), false)
  assert.equal(effectsOff(null), false)
  assert.equal(effectsOff('true'), false)
})

test('applying the choice sets and clears the motion attribute', () => {
  withBrowserEnv(() => {
    applyEffects(true)
    assert.equal(document.documentElement.dataset.motion, 'off')
    applyEffects(false)
    assert.equal(document.documentElement.dataset.motion, undefined)
  })
})

test('the choice is kept in this browser and can be read back', () => {
  withBrowserEnv((store) => {
    assert.equal(savedEffectsOff(), false)
    setEffectsOff(true)
    assert.equal(store.get('mediarium-reduce-effects'), '1')
    assert.equal(savedEffectsOff(), true)
    setEffectsOff(false)
    assert.equal(store.get('mediarium-reduce-effects'), '0')
    assert.equal(savedEffectsOff(), false)
  })
})

test('starting the app applies the saved choice before the first paint', () => {
  withBrowserEnv((store) => {
    store.set('mediarium-reduce-effects', '1')
    applySavedEffects()
    assert.equal(document.documentElement.dataset.motion, 'off')
  })
})

test('a browser that blocks storage keeps the motion on', () => {
  withBrowserEnv(() => {
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: () => {
          throw new Error('blocked')
        },
        setItem: () => {
          throw new Error('blocked')
        },
      },
      configurable: true,
    })
    assert.equal(savedEffectsOff(), false)
    assert.doesNotThrow(() => setEffectsOff(true))
    assert.equal(document.documentElement.dataset.motion, 'off')
  })
})
