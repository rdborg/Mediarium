import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type LifecycleShelfItem } from '../api'
import { plural, st as serverText, t } from '../i18n'
import { useModules } from '../ModulesContext'
import Icon from './Icon'
import InfoTip from './InfoTip'
import { useToast } from './Toast'

// The "Leaving soon" shelf on Home (library lifecycle rules, off until an
// administrator switches them on and "Act for real" is on): what the rules
// will remove, how many days are left, and a Keep button that exempts the
// title. Nothing shows while the shelf is empty.
export default function LeavingSoon() {
  const { on } = useModules()
  const toast = useToast()
  const enabled = on('lifecycle')
  const [items, setItems] = useState<LifecycleShelfItem[]>([])
  const [busy, setBusy] = useState('')

  const load = useCallback(() => {
    if (!enabled) return
    api
      .lifecycleShelf()
      .then((r) => setItems(r.items))
      .catch(() => setItems([]))
  }, [enabled])
  useEffect(load, [load])

  async function keep(it: LifecycleShelfItem) {
    const key = `${it.kind}-${it.id}`
    setBusy(key)
    try {
      await api.lifecycleKeep(it.kind, it.id)
      toast.success(t('Kept. It will not be removed.'))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  if (!enabled || items.length === 0) return null
  return (
    <section className="tile tone-library wide-tile leaving-tile">
      <div className="tile-head">
        <span className="tile-ico">
          <Icon name="clock" size={18} />
        </span>
        <h2>{t('Leaving soon')}</h2>
        <InfoTip text={t('Titles your rules will remove soon. Press Keep and a title stays.')} />
      </div>
      <ul className="leaving-list">
        {items.map((it) => {
          const key = `${it.kind}-${it.id}`
          const to = it.kind === 'movie' ? `/title/${it.tmdbId}` : `/series/${it.seriesId}`
          return (
            <li key={key}>
              {it.posterUrl ? <img src={it.posterUrl} alt="" loading="lazy" /> : <span className="leaving-poster" aria-hidden="true" />}
              <div className="leaving-main">
                <Link to={to}>{it.title}</Link>
                <small>{serverText(it.reason)}</small>
              </div>
              <span className={`badge leaving-days${it.daysLeft <= 3 ? ' soon' : ''}`}>{it.daysLeft === 0 ? t('Goes today') : plural(it.daysLeft, '1 day left', '{n} days left')}</span>
              <button className="btn-sm" disabled={busy === key} onClick={() => void keep(it)}>
                {t('Keep')}
              </button>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
