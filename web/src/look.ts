// How Mediarium looks in this browser, beyond light or dark (V4 B7): a pure
// black background for OLED screens (dark only) and a bigger text size. Kept
// in this browser, set before anything paints (main.tsx). No React here.

export type TextSize = 'normal' | 'large' | 'larger'

const BLACK_KEY = 'mediarium-black'
const TEXT_KEY = 'mediarium-text-size'

function read(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function write(key: string, value: string) {
  try {
    if (value) localStorage.setItem(key, value)
    else localStorage.removeItem(key)
  } catch {
    // a blocked storage only means the choice isn't kept
  }
}

export function savedBlack(): boolean {
  return read(BLACK_KEY) === '1'
}

export function savedTextSize(): TextSize {
  const v = read(TEXT_KEY)
  return v === 'large' || v === 'larger' ? v : 'normal'
}

// The colour of the browser bar and the phone's status strip. Pure black only
// counts in the dark look; the light look keeps the dark strip it always had.
export function themeColor(theme: string | undefined, black: boolean): string {
  return black && theme !== 'light' ? '#000000' : '#0b0d18'
}

// applyLook puts the saved choices on the page.
export function applyLook(black = savedBlack(), size = savedTextSize()) {
  const root = document.documentElement
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', themeColor(root.dataset.theme, black))
  if (black) root.dataset.black = '1'
  else delete root.dataset.black
  if (size === 'normal') delete root.dataset.text
  else root.dataset.text = size
}

export function setBlack(on: boolean) {
  write(BLACK_KEY, on ? '1' : '')
  applyLook()
}

export function setTextSize(size: TextSize) {
  write(TEXT_KEY, size === 'normal' ? '' : size)
  applyLook()
}
