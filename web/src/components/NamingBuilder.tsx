import { useEffect, useRef, useState } from 'react'
import { api, type NamingTokens } from '../api'
import NamingPreview from './NamingPreview'
import { FieldError, type Validation } from '../useValidation'

type Kind = 'movie' | 'tv'

// The custom naming builder: start from a ready-made format (or paste the one
// you use in Radarr or Sonarr), then edit it and click tokens to add them.
// The previews come from the real naming engine, so they match what imports
// produce.
export default function NamingBuilder({
  movieFormat,
  setMovieFormat,
  episodeFormat,
  setEpisodeFormat,
  v,
}: {
  movieFormat: string
  setMovieFormat: (s: string) => void
  episodeFormat: string
  setEpisodeFormat: (s: string) => void
  v: Validation
}) {
  const [data, setData] = useState<NamingTokens | null>(null)
  const [target, setTarget] = useState<Kind>('movie')
  const movieRef = useRef<HTMLInputElement>(null)
  const episodeRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api
      .namingTokens()
      .then(setData)
      .catch(() => setData(null))
  }, [])

  const current = data?.schemes.find((s) => s.movie === movieFormat && s.episode === episodeFormat)?.id ?? ''

  function pickScheme(id: string) {
    const s = data?.schemes.find((x) => x.id === id)
    if (!s) return
    setMovieFormat(s.movie)
    setEpisodeFormat(s.episode)
  }

  // Put the token where the cursor is in the box that was used last.
  function insert(token: string) {
    const el = target === 'movie' ? movieRef.current : episodeRef.current
    const value = target === 'movie' ? movieFormat : episodeFormat
    const set = target === 'movie' ? setMovieFormat : setEpisodeFormat
    const start = el?.selectionStart ?? value.length
    const end = el?.selectionEnd ?? value.length
    const before = value.slice(0, start)
    const gap = before !== '' && !/[\s([{.\-_]$/.test(before) ? ' ' : ''
    const next = before + gap + token + value.slice(end)
    set(next)
    const caret = start + gap.length + token.length
    requestAnimationFrame(() => {
      el?.focus()
      el?.setSelectionRange(caret, caret)
    })
  }

  const tokens = (data?.tokens ?? []).filter((t) => t.kind === 'both' || t.kind === target)
  const groups = [...new Set(tokens.map((t) => t.group))]

  return (
    <div className="naming-builder">
      <div className="naming-builder-forms">
        <label>
          Start from
          <select value={current} onChange={(e) => pickScheme(e.target.value)}>
            <option value="" disabled>
              Your own format
            </option>
            {data?.schemes.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        <small style={{ color: 'var(--text-dim)' }}>
          Already have a format in Radarr or Sonarr? Copy it from Settings &gt; Media Management there and paste it below. The same tokens work here.
        </small>
        <label>
          Movie file name
          <input
            ref={movieRef}
            value={movieFormat}
            onChange={(e) => setMovieFormat(e.target.value)}
            onFocus={() => setTarget('movie')}
            placeholder="{Movie CleanTitle} ({Release Year}){ - Edition Tags}"
            spellCheck={false}
            {...v.bind('customFormat', movieFormat, setMovieFormat)}
          />
          <FieldError v={v} name="customFormat" />
        </label>
        <NamingPreview preset="custom" format={movieFormat || undefined} />
        <label>
          Episode file name
          <input
            ref={episodeRef}
            value={episodeFormat}
            onChange={(e) => setEpisodeFormat(e.target.value)}
            onFocus={() => setTarget('tv')}
            placeholder="{Series TitleYear} - S{Season:00}E{Episode:00} - {Episode CleanTitle}"
            spellCheck={false}
            {...v.bind('episodeFormat', episodeFormat, setEpisodeFormat)}
          />
          <FieldError v={v} name="episodeFormat" />
        </label>
        <NamingPreview preset={episodeFormat ? 'custom' : 'plex'} format={episodeFormat || undefined} kind="tv" />
      </div>
      {data && (
        <div className="naming-tokens">
          <small style={{ color: 'var(--text-dim)' }}>
            Click a token to add it to the {target === 'movie' ? 'movie' : 'episode'} file name. Text inside the braces, like {'{ - Edition Tags}'} or {'{[Quality Full]}'}, only shows up when there's a value.
          </small>
          {groups.map((g) => (
            <div key={g} className="naming-token-group">
              <span>{g}</span>
              <div className="chip-row">
                {tokens
                  .filter((t) => t.group === g)
                  .map((t) => (
                    <button key={t.token} type="button" className="chip naming-token" onMouseDown={(e) => e.preventDefault()} onClick={() => insert(t.token)}>
                      {t.token}
                    </button>
                  ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
