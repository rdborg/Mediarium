// Speed and time left for a download, worked out from how far it got between
// two looks at the page (the queue only reports a percentage).

import { t } from './i18n.ts'

export interface RateSample {
  at: number // milliseconds
  bytes: number // how much was done then
  rate: number // bytes a second, smoothed; 0 until there are two samples
}

// nextSample takes the previous sample and what is known now. It keeps the
// old speed when nothing moved for a short while (a refresh between two
// updates), and starts again when the download went backwards or sat still
// for a minute.
export function nextSample(prev: RateSample | null, bytes: number, now: number): RateSample {
  if (!prev || bytes < prev.bytes) return { at: now, bytes, rate: 0 }
  const seconds = (now - prev.at) / 1000
  if (seconds < 0.8) return prev
  if (bytes === prev.bytes) return seconds > 60 ? { at: now, bytes, rate: 0 } : { ...prev, at: prev.at }
  const instant = (bytes - prev.bytes) / seconds
  return { at: now, bytes, rate: prev.rate > 0 ? prev.rate * 0.6 + instant * 0.4 : instant }
}

// etaText says how long is left in plain words, or '' when it can't be said.
export function etaText(secondsLeft: number): string {
  if (!Number.isFinite(secondsLeft) || secondsLeft <= 0) return ''
  if (secondsLeft < 90) return t('{n} s', { n: Math.max(1, Math.round(secondsLeft)) })
  const minutes = Math.round(secondsLeft / 60)
  if (minutes < 90) return t('{n} min', { n: minutes })
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest ? t('{h} h {m} min', { h: hours, m: rest }) : t('{h} h', { h: hours })
}
