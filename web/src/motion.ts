// Decorative motion is on unless this browser asked for less of it
// (Settings > General > Motion). The choice is kept per browser, like the
// theme, and applied to <html> as data-motion="off".
const STORAGE_KEY = 'mediarium-reduce-effects'
const OFF = 'off'

// effectsOff reads the saved setting: only "1" means the person switched it off.
export function effectsOff(saved: string | null): boolean {
  return saved === '1'
}

// applyEffects sets (or clears) the attribute the stylesheet looks for.
export function applyEffects(off: boolean) {
  if (off) document.documentElement.dataset.motion = OFF
  else delete document.documentElement.dataset.motion
}

export function savedEffectsOff(): boolean {
  try {
    return effectsOff(localStorage.getItem(STORAGE_KEY))
  } catch {
    // storage can be blocked (private windows): keep the motion on
    return false
  }
}

// Called as the app starts, before it paints, so a reload never shows one
// frame of the motion the person switched off.
export function applySavedEffects() {
  applyEffects(savedEffectsOff())
}

export function setEffectsOff(off: boolean) {
  applyEffects(off)
  try {
    localStorage.setItem(STORAGE_KEY, off ? '1' : '0')
  } catch {
    // a blocked localStorage just means the choice will not survive a reload
  }
}
