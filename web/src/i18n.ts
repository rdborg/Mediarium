// Translations. The English text is the key: t('Save') looks the phrase up in
// the chosen language and falls back to the English when it isn't translated
// yet, so nothing ever shows blank. The language is picked per browser
// (Profile > Language, or the sign-in page) and starts from the browser's own
// language when it is one we have.
//
// Rules that keep the catalogues complete (tools/i18n-check.mjs finds every
// phrase by reading the code):
// - pass t(), tc(), tx() and plural() a plain string literal, never a variable;
// - put changing parts in {braces}: t('{name} was added', { name });
// - plural(n, '{n} movie', '{n} movies') picks the right form for the language.
import { Fragment, createElement, type ReactNode } from 'react'

export type Lang = 'en' | 'es' | 'de' | 'fr' | 'it' | 'nl' | 'pl' | 'pt' | 'pt-PT' | 'ru' | 'uk' | 'zh-CN'

export const LANGUAGES: { code: Lang; name: string }[] = [
  { code: 'en', name: 'English' },
  { code: 'es', name: 'Español' },
  { code: 'de', name: 'Deutsch' },
  { code: 'fr', name: 'Français' },
  { code: 'it', name: 'Italiano' },
  { code: 'nl', name: 'Nederlands' },
  { code: 'pl', name: 'Polski' },
  { code: 'pt', name: 'Português (Brasil)' },
  { code: 'pt-PT', name: 'Português (Portugal)' },
  { code: 'ru', name: 'Русский' },
  { code: 'uk', name: 'Українська' },
  { code: 'zh-CN', name: '简体中文' },
]

// Each language's catalogue is its own file, fetched only when it is chosen.
const CATALOGUES: Record<Exclude<Lang, 'en'>, () => Promise<{ default: unknown }>> = {
  es: () => import('./locales/es.json'),
  de: () => import('./locales/de.json'),
  fr: () => import('./locales/fr.json'),
  it: () => import('./locales/it.json'),
  nl: () => import('./locales/nl.json'),
  pl: () => import('./locales/pl.json'),
  pt: () => import('./locales/pt.json'),
  'pt-PT': () => import('./locales/pt-PT.json'),
  ru: () => import('./locales/ru.json'),
  uk: () => import('./locales/uk.json'),
  'zh-CN': () => import('./locales/zh-CN.json'),
}

const STORAGE_KEY = 'mediarium-lang'

let lang: Lang = 'en'
let table: Record<string, string> = {}

const isLang = (v: unknown): v is Lang => LANGUAGES.some((l) => l.code === v)

// The language this browser asked for: a stored choice first, then the first
// browser language we have a translation for, then English.
export function preferredLanguage(stored: string | null, browser: readonly string[]): Lang {
  if (isLang(stored)) return stored
  for (const b of browser) {
    // A regional language we have on its own (pt-PT) wins over its family.
    const exact = LANGUAGES.find((l) => l.code.toLowerCase() === b.toLowerCase())
    if (exact) return exact.code
    const code = b.toLowerCase().split('-')[0]
    // Every Chinese (zh, zh-Hans, zh-SG ...) gets the Simplified translation until Traditional is added.
    if (code === 'zh') return 'zh-CN'
    if (isLang(code)) return code
  }
  return 'en'
}

function storedChoice(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

// Loads the chosen language before the app starts, so text set up when a
// file loads is translated too. English needs nothing loaded.
export async function loadLanguage(): Promise<void> {
  const want = preferredLanguage(storedChoice(), typeof navigator === 'undefined' ? [] : navigator.languages ?? [navigator.language])
  if (want !== 'en') {
    try {
      const mod = await CATALOGUES[want]()
      setCatalogue(want, mod.default as Record<string, string>)
    } catch {
      // A failed download leaves the app in English rather than broken.
      setCatalogue('en', {})
    }
  }
  if (typeof document !== 'undefined') document.documentElement.lang = lang
}

export function setCatalogue(code: Lang, entries: Record<string, string>) {
  lang = code
  table = entries
  serverPatterns = null
}

export function currentLanguage(): Lang {
  return lang
}

// Saves the choice and reloads, so every page comes back in the new language.
export function chooseLanguage(code: Lang) {
  try {
    localStorage.setItem(STORAGE_KEY, code)
  } catch {
    // private window: the choice lasts for this visit only
  }
  location.reload()
}

// The locale for dates and numbers: the browser's own for English (so a
// British or American reader keeps their date order), the language otherwise.
export function locale(): string | undefined {
  if (lang === 'en') return undefined
  return lang === 'pt' ? 'pt-BR' : lang
}

function fill(text: string, vars?: Record<string, string | number>): string {
  if (!vars) return text
  return text.replace(/\{(\w+)\}/g, (whole, name: string) => {
    if (!(name in vars)) return whole
    const v = vars[name]
    return typeof v === 'number' ? v.toLocaleString(locale()) : v
  })
}

export function t(text: string, vars?: Record<string, string | number>): string {
  return fill(table[text] || text, vars)
}

// Like t(), for a short word that means different things in different places
// ("Search" as a page title or as a button). The catalogue key is
// "context|English"; without its own entry it falls back to the plain phrase.
export function tc(context: string, text: string, vars?: Record<string, string | number>): string {
  return fill(table[`${context}|${text}`] || table[text] || text, vars)
}

// Like t(), for a sentence with links or bold words in it:
// tx('Press {save} when done', { save: <b>{t('Save')}</b> }).
export function tx(text: string, parts: Record<string, ReactNode>): ReactNode {
  const out: ReactNode[] = []
  const src = table[text] || text
  const re = /\{(\w+)\}/g
  let last = 0
  for (let m = re.exec(src); m; m = re.exec(src)) {
    if (!(m[1] in parts)) continue
    if (m.index > last) out.push(src.slice(last, m.index))
    const p = parts[m[1]]
    out.push(typeof p === 'number' ? p.toLocaleString(locale()) : p)
    last = m.index + m[0].length
  }
  if (last < src.length) out.push(src.slice(last))
  return createElement(Fragment, null, ...out)
}

// The singular or plural form for n. French counts 0 and 1 as singular, the
// other languages here only 1. Russian, Ukrainian and Polish have a third
// form for 2 to 4 (and 22 to 24 ...): it is the entry "<plural phrase>|few",
// and the plural phrase itself is used for the rest (5 to 21 ...).
//
// The language's "one" form also covers 21, 31, 101 ... in Russian and
// Ukrainian and 0 in French. The singular sentence only fits those when it
// shows the count ("{n} movie"); a sentence written for exactly one ("Remove
// this download?") is used for 1 only, and the others get the entry
// "<plural phrase>|one" when there is one, or else the plural phrase.
export function plural(n: number, one: string, other: string, vars?: Record<string, string | number>): string {
  const category = new Intl.PluralRules(lang).select(n)
  if (category === 'one' && (n === 1 || one.includes('{n}'))) return t(one, { n, ...vars })
  const special = category === 'other' ? '' : table[`${other}|${category}`]
  return fill(special || table[other] || other, { n, ...vars })
}

// Sentences the server sends (error messages, notes in the activity feed...)
// are in English. Each one the server can send is a phrase in the catalogue,
// with {0}, {1} ... where the server puts a name or a number (tools/msgextract
// lists them in serverText.json). st() finds the translation of a sentence the
// server sent, filling the pieces in again; a sentence it does not know stays
// as it came.
interface ServerPattern {
  re: RegExp
  key: string
  head: string // the fixed text the sentence starts with
  tail: string // and ends with
  weight: number // how much of the sentence is fixed text: the most specific match wins
}
let serverPatterns: ServerPattern[] | null = null

// Longer text is shown as it came. The patterns backtrack, and a long message
// that starts like a known sentence could otherwise keep the tab busy for
// seconds; no sentence the server writes itself is this long.
const MAX_SERVER_TEXT = 500

function compileServerPatterns(): ServerPattern[] {
  const out: ServerPattern[] = []
  for (const key of Object.keys(table)) {
    if (!/\{\d+\}/.test(key) || !table[key]) continue
    const parts = key.split(/\{\d+\}/)
    const re = new RegExp('^' + parts.map((p) => p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('(.+?)') + '$', 's')
    out.push({ re, key, head: parts[0], tail: parts[parts.length - 1], weight: parts.reduce((n, p) => n + p.length, 0) })
  }
  return out.sort((a, b) => b.weight - a.weight)
}

export function st(text: string | undefined | null): string {
  if (!text) return text ?? ''
  if (lang === 'en') return text
  const exact = table[text]
  if (exact) return exact
  // A note of two sentences, each on its own line, is translated sentence by sentence.
  if (text.includes('\n')) return text.split('\n').map(st).join(' ')
  if (text.length > MAX_SERVER_TEXT) return text
  if (!serverPatterns) serverPatterns = compileServerPatterns()
  for (const p of serverPatterns) {
    if (!text.startsWith(p.head) || !text.endsWith(p.tail)) continue
    const m = p.re.exec(text)
    if (!m) continue
    // The pieces come in the order of the English sentence: {0}, {1} ... as numbered there.
    const order = [...p.key.matchAll(/\{(\d+)\}/g)].map((x) => Number(x[1]))
    const args: string[] = []
    order.forEach((idx, i) => {
      args[idx] = m[i + 1]
    })
    return table[p.key].replace(/\{(\d+)\}/g, (whole, n: string) => args[Number(n)] ?? whole)
  }
  return text
}
