import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type BulkTitle, type WantedReviewItem } from '../api'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import Loading from './Loading'
import { useToast } from './Toast'
import { locale, plural, t } from '../i18n'

// Wanted > Long wanted (#168): the films and shows still missing long after
// they came out, with where each came from, to tidy up a big wanted list.
// Keep (it leaves this list for a year), stop monitoring, or remove.

const PERIODS = [
  { days: 182, label: t('6 months') },
  { days: 365, label: t('1 year') },
  { days: 730, label: t('2 years') },
  { days: 1825, label: t('5 years') },
]

type Origin = 'all' | 'list' | 'request' | 'account' | 'none'

function originText(o: WantedReviewItem['origin']): string {
  switch (o.kind) {
    case 'list':
      return t('List: {name}', { name: o.name ?? '' })
    case 'request':
      return o.name ? t('Requested by {name}', { name: o.name }) : t('A request')
    case 'account':
      return t('Added by {name}', { name: o.name ?? '' })
  }
  return t('Not known')
}

const keyOf = (i: WantedReviewItem) => `${i.kind}:${i.id}`

// A date as "12 Mar 2012" in the reader's language (the server sends YYYY-MM-DD or a full time).
function formatDate(d: string): string {
  const at = new Date(d.length === 10 ? `${d}T12:00:00` : d)
  return Number.isNaN(at.getTime()) ? d : at.toLocaleDateString(locale(), { day: 'numeric', month: 'short', year: 'numeric' })
}

export default function WantedReview() {
  const toast = useToast()
  const confirm = useConfirm()
  const [days, setDays] = useState(() => {
    try {
      return Number(localStorage.getItem('mediarium-long-wanted-days')) || 365
    } catch {
      return 365
    }
  })
  const [origin, setOrigin] = useState<Origin>('all')
  const [items, setItems] = useState<WantedReviewItem[] | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    setError('')
    api
      .wantedReview(days)
      .then((r) => {
        setItems(r)
        setPicked(new Set())
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [days])
  useEffect(load, [load])

  const shown = useMemo(() => (items ?? []).filter((i) => origin === 'all' || (origin === 'none' ? i.origin.kind === '' : i.origin.kind === origin)), [items, origin])
  const chosen = shown.filter((i) => picked.has(keyOf(i)))
  const titles = (): BulkTitle[] => chosen.map((i) => ({ kind: i.kind, id: i.id }))

  async function run(work: () => Promise<unknown>, done: string) {
    setBusy(true)
    try {
      await work()
      toast.success(done)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    const ok = await confirm({
      title: plural(chosen.length, 'Remove {n} title?', 'Remove {n} titles?'),
      body: <p>{t('Mediarium stops tracking them and cancels anything still downloading for them. Files already on the disk stay where they are. You can add them back any time.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
    })
    if (!ok) return
    await run(() => api.bulkRemove(titles(), false), plural(chosen.length, 'Removed {n} title.', 'Removed {n} titles.'))
  }

  const period = PERIODS.find((p) => p.days === days)?.label ?? ''
  return (
    <div className="long-wanted">
      <p className="field-hint" style={{ marginTop: 0 }}>
        {t('Titles still missing long after they came out, with where each one came from. Keep the ones you still want (they leave this list for a year), stop monitoring the rest, or remove them.')}
      </p>
      <div className="toolbar">
        <label className="inline-field">
          {t('Out for more than')}
          <select
            value={days}
            onChange={(e) => {
              const v = Number(e.target.value)
              setDays(v)
              try {
                localStorage.setItem('mediarium-long-wanted-days', String(v))
              } catch {
                /* only a convenience */
              }
            }}
          >
            {PERIODS.map((p) => (
              <option key={p.days} value={p.days}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
        <label className="inline-field">
          {t('Came from')}
          <select value={origin} onChange={(e) => setOrigin(e.target.value as Origin)}>
            <option value="all">{t('Anywhere')}</option>
            <option value="list">{t('An import list')}</option>
            <option value="request">{t('A request')}</option>
            <option value="account">{t('Added by someone')}</option>
            <option value="none">{t('Not known')}</option>
          </select>
        </label>
        <span className="spacer" />
        <button className="btn-sm" disabled={busy || chosen.length === 0} onClick={() => void run(() => api.keepWanted(titles()), plural(chosen.length, 'Kept {n} title. It leaves this list for a year.', 'Kept {n} titles. They leave this list for a year.'))}>
          <Icon name="check" size={14} /> {t('Keep')}
        </button>
        <button className="btn-sm" disabled={busy || chosen.length === 0} onClick={() => void run(() => api.bulkMonitored(titles(), false), plural(chosen.length, 'Stopped monitoring {n} title.', 'Stopped monitoring {n} titles.'))}>
          <Icon name="eye-off" size={14} /> {t('Stop monitoring')}
        </button>
        <button className="btn-sm danger-ghost" disabled={busy || chosen.length === 0} onClick={() => void remove()}>
          <Icon name="trash" size={14} /> {t('Remove')}
        </button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {items === null ? (
        <Loading height={120} />
      ) : shown.length === 0 ? (
        <p className="empty-note">{t('Nothing here: everything missing came out more recently, or you kept it.')}</p>
      ) : (
        <>
          <label className="long-wanted-all">
            <input
              type="checkbox"
              checked={chosen.length === shown.length}
              onChange={(e) => setPicked(e.target.checked ? new Set(shown.map(keyOf)) : new Set())}
            />{' '}
            {plural(shown.length, '{n} title missing for more than {period}', '{n} titles missing for more than {period}', { period })}
          </label>
          <ul className="long-wanted-list">
            {shown.map((i) => {
              const k = keyOf(i)
              return (
                <li key={k} className={picked.has(k) ? 'picked' : ''}>
                  <input
                    type="checkbox"
                    aria-label={i.title}
                    checked={picked.has(k)}
                    onChange={(e) =>
                      setPicked((p) => {
                        const next = new Set(p)
                        if (e.target.checked) next.add(k)
                        else next.delete(k)
                        return next
                      })
                    }
                  />
                  {i.posterUrl ? <img src={i.posterUrl} alt="" loading="lazy" /> : <span className="lw-noposter" />}
                  <span className="lw-main">
                    <Link to={i.kind === 'movie' ? `/title/${i.tmdbId}` : `/series/${i.id}`} className="lw-title">
                      {i.title} {i.year ? <small>({i.year})</small> : null}
                    </Link>
                    <small className="lw-meta">
                      {i.kind === 'tv'
                        ? plural(i.missing ?? 0, '{n} episode missing since {date}', '{n} episodes missing since {date}', { date: formatDate(i.outSince) })
                        : t('Out since {date}', { date: formatDate(i.outSince) })}
                      {' · '}
                      {originText(i.origin)}
                      {i.addedAt ? ` · ${t('Added {date}', { date: formatDate(i.addedAt) })}` : ''}
                    </small>
                  </span>
                </li>
              )
            })}
          </ul>
        </>
      )}
    </div>
  )
}
