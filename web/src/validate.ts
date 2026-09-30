// Plain-function checks for form fields. Every function returns null when the
// value is fine, or a short, friendly message to show under the field.
//
// Rules that apply to blank input: only `required` complains about a blank
// value. Everything else lets a blank pass, so optional fields can use the
// same check as required ones, and required fields just add `required` in
// front (see `firstError`).
//
// This file is deliberately free of React and browser-only code so it can be
// unit-tested with plain node (see validate.test.ts). The matching server-side
// rules live in internal/api/validate.go, and the two are kept in step.

export type Message = string | null

/** The first problem out of several checks, or null when they all pass. */
export function firstError(...checks: (Message | false | undefined)[]): Message {
  for (const c of checks) if (c) return c
  return null
}

const str = (v: unknown) => (v === null || v === undefined ? '' : String(v))
const blank = (v: unknown) => str(v).trim() === ''

/** A value that has to be filled in. Pass the whole message for the field. */
export function required(value: unknown, message = "This can't be left empty."): Message {
  return blank(value) ? message : null
}

// ---- Accounts -------------------------------------------------------------

export const USERNAME_MIN = 3
export const USERNAME_MAX = 32
export const PASSWORD_MIN = 8
// bcrypt only looks at the first 72 bytes, so longer passwords are refused.
export const PASSWORD_MAX_BYTES = 72

export function email(value: string): Message {
  const v = str(value).trim()
  if (v === '') return null
  const bad = "That email address doesn't look right. It should look like name@example.com."
  if (/[\s,;<>]/.test(v) || v.length > 254) return bad
  const at = v.indexOf('@')
  if (at < 1 || at !== v.lastIndexOf('@')) return bad
  const domain = v.slice(at + 1)
  const dot = domain.lastIndexOf('.')
  if (dot < 1 || dot > domain.length - 2) return bad
  return null
}

export function username(value: string): Message {
  const v = str(value).trim()
  if (v === '') return null
  if (v.length < USERNAME_MIN || v.length > USERNAME_MAX) return `A username needs to be ${USERNAME_MIN} to ${USERNAME_MAX} characters long.`
  if (!/^[A-Za-z0-9._-]+$/.test(v)) return 'Usernames can only use letters, numbers, dots, dashes and underscores.'
  return null
}

const utf8Length = (s: string) => new TextEncoder().encode(s).length

export function password(value: string): Message {
  const v = str(value)
  if (v === '') return null
  if (v.length < PASSWORD_MIN) return `Passwords need at least ${PASSWORD_MIN} characters.`
  if (utf8Length(v) > PASSWORD_MAX_BYTES) return `Passwords can be at most ${PASSWORD_MAX_BYTES} characters long.`
  return null
}

export function passwordsMatch(pw: string, again: string): Message {
  if (again === '') return null
  return pw === again ? null : "The two passwords don't match."
}

export function maxLength(value: string, max: number, label: string): Message {
  return str(value).trim().length > max ? `${label} can be at most ${max} characters long.` : null
}

// ---- Addresses ------------------------------------------------------------

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/

function isIPv4(s: string): boolean {
  const m = IPV4.exec(s)
  return !!m && m.slice(1).every((n) => Number(n) <= 255)
}

function isIPv6(s: string): boolean {
  if (!/^[0-9a-fA-F:.]+$/.test(s) || !s.includes(':')) return false
  if (s.split('::').length > 2) return false
  let parts = s.split(':')
  const last = parts[parts.length - 1]
  let groups = 0
  if (last.includes('.')) {
    if (!isIPv4(last)) return false
    parts = parts.slice(0, -1)
    groups = 2
  }
  const compressed = s.includes('::')
  const hex = parts.filter((p) => p !== '')
  if (hex.some((p) => p.length > 4)) return false
  groups += hex.length
  return compressed ? groups <= 7 : groups === 8
}

const HOST_LABEL = /^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$/

function isHostName(s: string): boolean {
  const h = s.endsWith('.') ? s.slice(0, -1) : s
  if (h === '' || h.length > 253) return false
  return h.split('.').every((l) => HOST_LABEL.test(l))
}

/** True for a host name, an IPv4 address or an IPv6 address (no port, no scheme). */
export function isHostOrIP(s: string): boolean {
  if (/^[0-9.]+$/.test(s) && s.includes('.')) return isIPv4(s)
  return isHostName(s) || isIPv6(s.replace(/^\[|\]$/g, ''))
}

/** A bare host name or IP address, for fields where the port is a separate box. */
export function hostOrIP(value: string, example = 'news.example.com'): Message {
  const v = str(value).trim()
  if (v === '') return null
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(v) || v.includes('/')) return `Enter just the server name, without http:// or a path. For example ${example}.`
  if (/\s/.test(v)) return "The server name can't contain spaces."
  // A single colon means a port was tacked on; IPv6 addresses have several.
  if (v.split(':').length === 2 && !v.startsWith('[')) return `Put the port in the Port box, not in the address. The address should look like ${example}.`
  if (!isHostOrIP(v)) return `That doesn't look like a server name or IP address. It should look like ${example} or 192.168.1.10.`
  return null
}

export interface UrlOptions {
  /** Refuse an address that has no http:// or https:// in front. Default false. */
  requireScheme?: boolean
  /** Shown to the user as the example, e.g. "http://192.168.1.10:32400". */
  example?: string
}

/** A web address. Only http and https, and it has to name a server. */
export function url(value: string, opts: UrlOptions = {}): Message {
  const v = str(value).trim()
  if (v === '') return null
  const example = opts.example ?? 'http://192.168.1.10:8080'
  if (/\s/.test(v)) return "The address can't contain spaces."
  if (v.startsWith('/')) return `That doesn't look like a web address. It should look like ${example}.`
  const hasScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(v)
  if (!hasScheme && opts.requireScheme) return `Start the address with http:// or https://, for example ${example}.`
  if (hasScheme && !/^https?:\/\//i.test(v)) return `The address has to start with http:// or https://, for example ${example}.`
  let u: URL
  try {
    u = new URL(hasScheme ? v : `http://${v}`)
  } catch {
    return `That doesn't look like a web address. It should look like ${example}.`
  }
  const host = u.hostname.replace(/^\[|\]$/g, '')
  if (host === '') return `That address is missing a server name. It should look like ${example}.`
  if (!isHostOrIP(host)) return `That doesn't look like a web address. It should look like ${example}.`
  return null
}

/** True when the string is a well-formed port number. */
export function isPort(value: unknown): boolean {
  const s = str(value).trim()
  if (!/^\d{1,5}$/.test(s)) return false
  const n = Number(s)
  return n >= 1 && n <= 65535
}

export function port(value: unknown, label = 'Port'): Message {
  if (blank(value)) return null
  return isPort(value) ? null : `${label} must be a number between 1 and 65535.`
}

// ---- Numbers --------------------------------------------------------------

export interface RangeOptions {
  min?: number
  max?: number
  /** Decimals allowed. Default false: whole numbers only. */
  decimal?: boolean
  /** Let a blank value through (an optional number). Default true. */
  allowBlank?: boolean
}

const fmt = (n: number) => n.toLocaleString('en-US')

/** A whole number (or a decimal, if asked) within limits. `label` starts the sentence, e.g. "Connections". */
export function numberRange(value: unknown, label: string, opts: RangeOptions = {}): Message {
  const { min, max, decimal = false, allowBlank = true } = opts
  const s = str(value).trim()
  const noun = decimal ? 'a number' : 'a whole number'
  if (s === '') return allowBlank ? null : `${label} must be ${noun}${limits(min, max)}.`
  const n = Number(s)
  if (!Number.isFinite(n) || (!decimal && !Number.isInteger(n))) return `${label} must be ${noun}${limits(min, max)}.`
  if ((min !== undefined && n < min) || (max !== undefined && n > max)) return `${label} must be ${noun}${limits(min, max)}.`
  return null
}

function limits(min?: number, max?: number): string {
  if (min !== undefined && max !== undefined) return ` between ${fmt(min)} and ${fmt(max)}`
  if (min !== undefined) return min === 0 ? ' of 0 or more' : min === 1 ? ' of 1 or more' : ` of ${fmt(min)} or more`
  if (max !== undefined) return ` of ${fmt(max)} or less`
  return ''
}

/** A whole number of at least 1. */
export function positiveInt(value: unknown, label: string, max?: number): Message {
  return numberRange(value, label, { min: 1, max })
}

/** min must not be larger than max. Blank or non-numeric values are left to the field checks. */
export function minMax(min: unknown, max: unknown, message: string): Message {
  if (blank(min) || blank(max)) return null
  const a = Number(min)
  const b = Number(max)
  if (!Number.isFinite(a) || !Number.isFinite(b)) return null
  return a <= b ? null : message
}

// ---- Paths, keys, text ----------------------------------------------------

/** An absolute folder path: starts with /, a drive letter such as D:\ , or a \\server\share. */
export function folderPath(value: string, example = '/media/movies'): Message {
  const v = str(value).trim()
  if (v === '') return null
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f]/.test(v)) return "The folder path can't contain hidden or control characters."
  if (!(v.startsWith('/') || /^[A-Za-z]:[\\/]/.test(v) || v.startsWith('\\\\'))) {
    return `That folder path isn't complete. It should start with / or a drive letter, for example ${example}.`
  }
  return null
}

/** An API key or token: one unbroken string, no spaces or line breaks. */
export function apiKey(value: string): Message {
  const v = str(value).trim()
  if (v === '') return null
  if (/\s/.test(v)) return "That key has a space or line break in it. Copy just the key itself, with nothing around it."
  if (v.length > 512) return 'That key is much longer than a real one. Check that you copied only the key.'
  return null
}

/** A folder or file name that must not contain path separators or control characters. */
export function plainName(value: string, label: string): Message {
  const v = str(value).trim()
  if (v === '') return null
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f]/.test(v)) return `${label} can't contain control characters.`
  return null
}

// ---- VPN / network details --------------------------------------------------

/** A server address with its port in one box, such as vpn.example.com:51820 or [2001:db8::1]:51820. */
export function hostPort(value: string, example = 'vpn.example.com:51820'): Message {
  const v = str(value).trim()
  if (v === '') return null
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(v) || v.includes('/') || /\s/.test(v)) return `Enter the address and port only, like ${example}.`
  const m = /^(?:\[([^\]]+)\]|([^:]+)):(\d+)$/.exec(v)
  if (!m) return `Add the port after the address, like ${example}.`
  if (!isPort(m[3])) return 'The port must be a number between 1 and 65535.'
  if (!isHostOrIP(m[1] ?? m[2])) return `That doesn't look like a server name or IP address. It should look like ${example}.`
  return null
}

/** A WireGuard key: 32 bytes written in base64, which is always 44 characters ending in =. */
export function wireguardKey(value: string, label = 'The key'): Message {
  const v = str(value).trim()
  if (v === '') return null
  if (!/^[A-Za-z0-9+/]{43}=$/.test(v)) return `${label} should be 44 characters ending in =, exactly as it appears in your provider's config. Copy just the key, with nothing around it.`
  return null
}

/** An IPv4 or IPv6 address, optionally with a /prefix length such as 10.2.0.2/32. */
export function ipWithPrefix(value: string, example = '10.2.0.2/32'): Message {
  const v = str(value).trim()
  if (v === '') return null
  const bad = `That doesn't look like an IP address. It should look like ${example}.`
  const [addr, prefix, ...rest] = v.split('/')
  if (rest.length > 0) return bad
  const v6 = addr.includes(':')
  if (!(v6 ? isIPv6(addr) : isIPv4(addr))) return bad
  if (prefix !== undefined) {
    if (!/^\d{1,3}$/.test(prefix) || Number(prefix) > (v6 ? 128 : 32)) return `The number after the / should be between 0 and ${v6 ? 128 : 32}, like ${example}.`
  }
  return null
}
