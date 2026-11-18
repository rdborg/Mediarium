import { useCallback, useEffect, useState } from 'react'
import { api, type SizeRow, type SizeTable } from '../api'
import { plural, t } from '../i18n'
import { applyTrashSizes, type SizeChange } from '../trashSizes'
import { useConfirm } from './ConfirmProvider'
import Switch from './Switch'
import { useToast } from './Toast'
import TipLabel from './TipLabel'
import Collapsible from './Collapsible'

// A row being edited: the boxes hold text, so a half-typed number is not lost.
interface EditRow extends SizeRow {
  minText: string
  maxText: string
}

type Kind = 'movie' | 'tv'

// The size lists the TRaSH Guides publish; the id is the name the server knows.
// Radarr's lists are for the movie table and Sonarr's for the show table.
const TRASH_LISTS: Record<Kind, { id: string; label: () => string }[]> = {
  movie: [
    { id: 'radarr-movie', label: () => t('Movies') },
    { id: 'radarr-sqp-streaming', label: () => t('Movies, streaming (SQP)') },
    { id: 'radarr-sqp-uhd', label: () => t('Movies, 4K (SQP)') },
    { id: 'radarr-anime', label: () => t('Anime movies') },
  ],
  tv: [
    { id: 'sonarr-series', label: () => t('Series') },
    { id: 'sonarr-anime', label: () => t('Anime series') },
  ],
}

const rangeText = (min: number, max: number): string => (max <= 0 ? t('{min} and up', { min }) : t('{min} to {max}', { min, max }))

const FILM_MINUTES = 100
const numText = (n: number) => (n === 0 ? '' : String(n))

const toEdit = (r: SizeRow): EditRow => ({ ...r, minText: String(r.min), maxText: numText(r.max) })

// "0.4 to 13 GB" for a film of 100 minutes; the biggest end reads "or more".
function filmText(min: number, max: number): string {
  const gb = (mbPerMin: number) => {
    const v = (mbPerMin * FILM_MINUTES) / 1024
    return v >= 10 ? String(Math.round(v)) : String(Math.round(v * 10) / 10)
  }
  if (max <= 0) return min > 0 ? t('{min} GB or more', { min: gb(min) }) : t('any size')
  return t('{min} to {max} GB', { min: gb(min), max: gb(max) })
}

function parse(text: string): number | null {
  const s = text.trim()
  if (s === '') return 0
  const n = Number(s)
  return Number.isFinite(n) && n >= 0 ? n : null
}

function rowError(r: EditRow): string | null {
  const min = parse(r.minText)
  const max = parse(r.maxText)
  if (min === null || max === null) return t('Use a number, like 4 or 2.5.')
  if (min > 3000 || max > 3000) return t('Use 3000 or less.')
  if (max !== 0 && max < min) return t("The largest can't be smaller than the smallest.")
  return null
}

// One size table, for movies or for shows: the smallest and largest release
// Mediarium takes for each quality, in megabytes per minute of what is in it (a
// film's length, or an episode's), as in the quality definitions of Radarr and Sonarr.
function SizeTableEditor({ kind, onEnabled }: { kind: Kind; onEnabled: (on: boolean) => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [saved, setSaved] = useState<SizeTable | null>(null)
  const [enabled, setEnabled] = useState(false)
  const [rows, setRows] = useState<EditRow[]>([])
  const [busy, setBusy] = useState(false)
  const [trashList, setTrashList] = useState(TRASH_LISTS[kind][0].id)
  const [trashBusy, setTrashBusy] = useState(false)
  // What the last "Apply TRaSH sizes" did, shown until the table is saved or reset.
  const [applied, setApplied] = useState<{ list: string; changes: SizeChange[]; untouched: string[]; skipped: string[] } | null>(null)

  const load = (table: SizeTable) => {
    setSaved(table)
    setEnabled(table.enabled)
    setRows(table.rows.map(toEdit))
    setApplied(null)
  }
  useEffect(() => {
    api.sizeLimits(kind).then(load).catch(() => undefined)
  }, [kind])

  useEffect(() => {
    if (saved) onEnabled(saved.enabled)
  }, [saved, onEnabled])

  const errors = rows.map(rowError)
  const hasError = errors.some(Boolean)
  const changed =
    !!saved &&
    (enabled !== saved.enabled ||
      rows.some((r, i) => {
        const was = saved.rows[i]
        return !was || parse(r.minText) !== was.min || parse(r.maxText) !== was.max
      }))
  const edit = (i: number, patch: Partial<EditRow>) => setRows((cur) => cur.map((r, j) => (j === i ? { ...r, ...patch } : r)))

  async function save() {
    if (hasError) return
    setBusy(true)
    try {
      const table = await api.putSizeLimits(
        { enabled, rows: rows.map((r) => ({ quality: r.quality, min: parse(r.minText) ?? 0, max: parse(r.maxText) ?? 0, defaultMin: r.defaultMin, defaultMax: r.defaultMax })) },
        kind,
      )
      load(table)
      toast.success(table.enabled ? t('Saved: releases outside these sizes are skipped.') : t('Saved. The size limits are off.'))
    } catch (e) {
      toast.error(t('Not saved: {error}', { error: e instanceof Error ? e.message : String(e) }))
    } finally {
      setBusy(false)
    }
  }

  // Reads the sizes from the TRaSH Guides and fills the table with them. Nothing is saved until Save.
  async function applyTrash() {
    setTrashBusy(true)
    try {
      const r = await api.trashSizes(trashList)
      const res = applyTrashSizes(rows, r.rows)
      setRows(res.rows)
      setApplied({ list: trashList, changes: res.changes, untouched: res.untouched, skipped: r.skipped })
    } catch (e) {
      setApplied(null)
      toast.error(t("Couldn't get the TRaSH sizes: {error}", { error: e instanceof Error ? e.message : String(e) }))
    } finally {
      setTrashBusy(false)
    }
  }

  async function reset() {
    if (!(await confirm({ title: t('Put every size back to its default?'), body: <p>{t('Your own numbers are replaced. The switch stays as it is.')}</p>, confirmLabel: t('Reset sizes') }))) return
    setBusy(true)
    try {
      load(await api.resetSizeLimits(kind))
      toast.success(t('The sizes are back to their defaults.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const off = !enabled
  return (
    <div className="size-kind">
      <Switch
        checked={enabled}
        onChange={setEnabled}
        disabled={!saved || busy}
        label={t('Use these size limits')}
        description={t('Off, the numbers below do nothing. Runtimes come from TMDB; when one is missing, a film counts as {film} minutes and an episode as 45.', { film: FILM_MINUTES })}
        showState
      />
      <div className="size-trash">
        <label>
          <TipLabel text={t('Fills the table with the smallest and largest sizes the TRaSH Guides recommend, read from their site. You see what changes first, and nothing is kept until you press Save. Their preferred size has no place here and is left out; a largest size at the end of their scale means no limit.')}>{t('Apply TRaSH sizes')}</TipLabel>
          <select value={trashList} onChange={(e) => setTrashList(e.target.value)} disabled={!saved || busy || trashBusy}>
            {TRASH_LISTS[kind].map((l) => (
              <option key={l.id} value={l.id}>
                {l.label()}
              </option>
            ))}
          </select>
        </label>
        <button disabled={!saved || busy || trashBusy} onClick={() => void applyTrash()}>
          {trashBusy ? t('Reading…') : t('Apply TRaSH sizes')}
        </button>
      </div>
      {applied && (
        <div className="size-trash-result" role="status">
          <strong>
            {applied.changes.length === 0
              ? t('Nothing changes: the table already has these sizes.')
              : plural(applied.changes.length, '{n} quality changed. Press Save to keep it.', '{n} qualities changed. Press Save to keep them.')}
          </strong>
          {applied.changes.length > 0 && (
            <ul>
              {applied.changes.map((c) => (
                <li key={c.quality}>
                  <b>{c.quality}</b> {rangeText(c.fromMin, c.fromMax)} → {rangeText(c.toMin, c.toMax)}
                </li>
              ))}
            </ul>
          )}
          {applied.untouched.length > 0 && <small className="field-hint">{t('Left as they are, since TRaSH gives no size for them: {names}.', { names: applied.untouched.join(', ') })}</small>}
          {applied.skipped.length > 0 && <small className="field-hint">{t('TRaSH sizes with no row here: {names}.', { names: applied.skipped.join(', ') })}</small>}
          {!enabled && <small className="field-hint">{t('The limits are still switched off. Turn on the switch above for them to count.')}</small>}
        </div>
      )}
      <div className="size-table-wrap" style={{ overflowX: 'auto', marginTop: 12 }}>
        <table className="size-table" style={{ opacity: off ? 0.6 : 1, width: '100%' }}>
          <thead>
            <tr>
              <th style={{ textAlign: 'left' }}>{t('Quality')}</th>
              <th style={{ textAlign: 'left' }}><TipLabel text={t('Releases smaller than this are skipped. A film of 2 hours at 4 MB a minute comes to about 0.5 GB.')}>{t('Smallest (MB a minute)')}</TipLabel></th>
              <th style={{ textAlign: 'left' }}><TipLabel text={t('Releases bigger than this are skipped. Leave it empty for no upper limit, which is usual for remuxes and 4K.')}>{t('Largest (MB a minute)')}</TipLabel></th>
              <th style={{ textAlign: 'left' }}>{t('For a film of {n} minutes', { n: FILM_MINUTES })}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => {
              const min = parse(r.minText)
              const max = parse(r.maxText)
              return (
                <tr key={r.quality}>
                  <td style={{ whiteSpace: 'nowrap' }}>{r.quality}</td>
                  <td>
                    <input
                      aria-label={t('{quality}: smallest size', { quality: r.quality })}
                      inputMode="decimal"
                      value={r.minText}
                      disabled={!saved || busy}
                      onChange={(e) => edit(i, { minText: e.target.value })}
                      aria-invalid={errors[i] ? true : undefined}
                      style={{ width: 90 }}
                    />
                  </td>
                  <td>
                    <input
                      aria-label={t('{quality}: largest size', { quality: r.quality })}
                      inputMode="decimal"
                      value={r.maxText}
                      placeholder={t('No limit')}
                      disabled={!saved || busy}
                      onChange={(e) => edit(i, { maxText: e.target.value })}
                      aria-invalid={errors[i] ? true : undefined}
                      style={{ width: 90 }}
                    />
                  </td>
                  <td>
                    {errors[i] ? <small style={{ color: 'var(--danger)' }}>{errors[i]}</small> : <small className="field-hint">{min !== null && max !== null ? filmText(min, max) : ''}</small>}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <p style={{ marginBottom: 0, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button className="primary" disabled={!changed || busy || hasError} onClick={() => void save()}>
          {busy ? t('Saving…') : t('Save')}
        </button>
        <button disabled={busy || !saved} onClick={() => void reset()}>
          {t('Reset to defaults')}
        </button>
      </p>
    </div>
  )
}

// Settings > Finding releases > Quality: the size limits, one table for movies and one
// for shows. TRaSH's sizes differ between Radarr and Sonarr, so each side has
// its own table and its own Apply TRaSH sizes.
export default function SizeLimitsCard() {
  const toast = useToast()
  const [minCheck, setMinCheck] = useState<boolean | null>(null)
  useEffect(() => {
    api.minSizeCheck().then((r) => setMinCheck(r.enabled)).catch(() => undefined)
  }, [])
  async function toggleMinCheck(on: boolean) {
    const was = minCheck
    setMinCheck(on)
    try {
      await api.putMinSizeCheck(on)
    } catch (e) {
      setMinCheck(was)
      toast.error(t('Not saved: {error}', { error: e instanceof Error ? e.message : String(e) }))
    }
  }
  const [kind, setKind] = useState<Kind>('movie')
  const [on, setOn] = useState<Record<Kind, boolean | null>>({ movie: null, tv: null })
  const setMovieOn = useCallback((v: boolean) => setOn((cur) => ({ ...cur, movie: v })), [])
  const setTvOn = useCallback((v: boolean) => setOn((cur) => ({ ...cur, tv: v })), [])
  const summary = on.movie === null || on.tv === null ? undefined : on.movie && on.tv ? t('On for movies and TV') : on.movie ? t('On for movies') : on.tv ? t('On for TV') : t('Off')
  const tabs: { id: Kind; label: string }[] = [
    { id: 'movie', label: t('Movies') },
    { id: 'tv', label: t('TV') },
  ]
  return (
    <section className="card size-limits span-all">
      <h2><TipLabel help="quality#size-limits-per-quality">{t('Size limits')}</TipLabel></h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t("The smallest and largest release to take for each quality, in megabytes for each minute of the film or episode. A release outside them is skipped by automatic searches and marked in the list when you search yourself; you can still pick it by hand. They come on top of the built-in minimums and a profile's largest size: the tightest one wins. A release that doesn't say its size always passes, and a season pack is only checked against the smallest.")}
      </p>
      <p style={{ color: 'var(--text-dim)' }}>{t('Movies and TV each have their own table, so the TRaSH sizes for one never change the other.')}</p>
      <Switch
        checked={minCheck ?? true}
        onChange={(on) => void toggleMinCheck(on)}
        disabled={minCheck === null}
        label={t('Built-in minimum size check')}
        description={t('Skips releases that are much smaller than a real release of that quality should be (fakes and samples). Turn it off if you have short shows or small encodes.')}
        showState
      />
      <Collapsible id="size-limits" label={t('Size limits')} summary={summary}>
      <div className="seg" role="tablist" aria-label={t('Size limits')} style={{ marginBottom: 14 }}>
        {tabs.map((tab) => (
          <button key={tab.id} id={`size-tab-${tab.id}`} role="tab" aria-selected={kind === tab.id} aria-controls={`size-panel-${tab.id}`} className={kind === tab.id ? 'active' : ''} onClick={() => setKind(tab.id)}>
            {tab.label}
          </button>
        ))}
      </div>
      {tabs.map((tab) => (
        <div key={tab.id} id={`size-panel-${tab.id}`} role="tabpanel" aria-labelledby={`size-tab-${tab.id}`} hidden={kind !== tab.id}>
          <SizeTableEditor kind={tab.id} onEnabled={tab.id === 'movie' ? setMovieOn : setTvOn} />
        </div>
      ))}
      </Collapsible>
    </section>
  )
}
