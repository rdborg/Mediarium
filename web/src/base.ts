// Mediarium can be served under a sub-path behind a reverse proxy, such as
// /mediarium (the URL_BASE setting of the container). The server puts that
// prefix in a <meta name="mediarium-base"> tag of the page, so the same build
// works under any prefix. Without the tag the prefix is empty and every
// address is what it always was.

/** Cleans up what the page's tag holds: "" or a path like "/mediarium" with no slash at the end. */
export function cleanBase(raw: string | null | undefined): string {
  const trimmed = (raw ?? '').trim().replace(/\/+$/, '')
  return trimmed.startsWith('/') ? trimmed : ''
}

/** Puts base in front of an address on this server that starts with a slash. Anything else comes back unchanged. */
export function prefixed(base: string, path: string): string {
  if (base === '' || !path.startsWith('/') || path.startsWith('//')) return path
  return base + path
}

/** The reverse of prefixed: the address without base. */
export function unprefixed(base: string, path: string): string {
  if (base !== '' && (path === base || path.startsWith(base + '/'))) return path.slice(base.length) || '/'
  return path
}

// The fields in which the server sends a picture's address. Pictures it serves
// itself (book and music covers) come as "/api/...", from the domain root.
const PICTURE_FIELDS = new Set(['coverUrl', 'posterUrl', 'imageUrl', 'backdropUrl', 'logoUrl', 'profileUrl'])

// Posters, backdrops and pictures of people come from TMDB's picture host. By
// default the server fetches them, resizes them and keeps a copy
// (GET /api/img/tmdb/<size>/<file>), so the browser never contacts TMDB and a
// phone gets a small picture. An administrator can switch that off in
// Settings; the choice is kept here so the very first answers of a page load
// already use it.
const ARTWORK_KEY = 'mediarium-artwork-via-server'
const TMDB_PICTURE = /^https:\/\/image\.tmdb\.org\/t\/p\/(w92|w154|w185|w342|w500|w780|w1280|original)\/([A-Za-z0-9_-]{4,64}\.(?:jpg|jpeg|png))$/

function readArtworkChoice(): boolean {
  try {
    return typeof localStorage === 'undefined' || localStorage.getItem(ARTWORK_KEY) !== '0'
  } catch {
    return true
  }
}

let artworkViaServer = readArtworkChoice()

/** Whether artwork is loaded through this server (the default) or straight from the picture host. */
export function artworkThroughServer(): boolean {
  return artworkViaServer
}

/** Remembers the administrator's choice (from the server's answers) for the next page load. */
export function setArtworkThroughServer(on: boolean): void {
  artworkViaServer = on
  try {
    localStorage.setItem(ARTWORK_KEY, on ? '1' : '0')
  } catch {
    // A private window may refuse; the choice then lasts until the page is closed.
  }
}

/** The address of a TMDB picture on this server, or the address unchanged when it is not one (or the choice is off). */
export function viaServer(base: string, url: string): string {
  if (!artworkViaServer) return url
  const m = TMDB_PICTURE.exec(url)
  return m ? `${base}/api/img/tmdb/${m[1]}/${m[2]}` : url
}

const OWN_PICTURE = /^(.*\/api\/img\/tmdb\/)(?:w\d+|original)\/([A-Za-z0-9_-]+\.(?:jpg|jpeg|png))$/

/**
 * A srcset for a picture this server keeps at several sizes (a size name that
 * exists: 92, 154, 185, 342, 500, 780 or 1280), so a phone takes a small one.
 * Undefined for any other address, which then simply loads as it is.
 */
export function pictureSrcSet(url: string, widths: number[]): string | undefined {
  const m = OWN_PICTURE.exec(url)
  return m ? widths.map((w) => `${m[1]}w${w}/${m[2]} ${w}w`).join(', ') : undefined
}

/**
 * Puts base in front of every picture address on this server ("/api/...") in
 * an answer from the server, however deep it sits, and sends TMDB pictures
 * through this server (see viaServer). The answer is changed in place and
 * returned. Addresses on other sites are left alone.
 */
export function rebasePictures<T>(base: string, data: T): T {
  if (data === null || typeof data !== 'object') return data
  const seen: unknown[] = [data]
  while (seen.length > 0) {
    const node = seen.pop() as Record<string, unknown> | unknown[]
    if (Array.isArray(node)) {
      for (let i = 0; i < node.length; i++) {
        const v = node[i]
        if (typeof v === 'string') {
          if (v.startsWith('https://image.tmdb.org/')) node[i] = viaServer(base, v)
        } else if (v !== null && typeof v === 'object') seen.push(v)
      }
      continue
    }
    for (const [k, v] of Object.entries(node)) {
      if (typeof v === 'string') {
        if (base !== '' && PICTURE_FIELDS.has(k) && v.startsWith('/api/')) node[k] = base + v
        else if (v.startsWith('https://image.tmdb.org/')) node[k] = viaServer(base, v)
      } else if (v !== null && typeof v === 'object') seen.push(v)
    }
  }
  return data
}

function readBase(): string {
  if (typeof document === 'undefined') return ''
  return cleanBase(document.querySelector('meta[name="mediarium-base"]')?.getAttribute('content'))
}

/** The prefix Mediarium is served under: "" or something like "/mediarium". */
export const BASE_PATH = readBase()

/** '/api/version' becomes '/mediarium/api/version' under a prefix. */
export const withBase = (path: string): string => prefixed(BASE_PATH, path)

/** The address without the prefix. */
export const withoutBase = (path: string): string => unprefixed(BASE_PATH, path)
