import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type AppLink } from '../../api'
import Icon, { type IconName } from '../../components/Icon'
import { useToast } from '../../components/Toast'
import { DOCS_URL } from '../../docs'
import { t } from '../../i18n'
import TipLabel from '../../components/TipLabel'

const ICONS: IconName[] = ['grid', 'tv', 'film', 'music', 'book', 'headphones', 'globe', 'server', 'monitor', 'star', 'shield', 'calendar']

// Settings > Media servers and apps > Add-ons: other programs that run next to Mediarium,
// each with a link in the players menu. Mediarium doesn't run them or load
// their code: it only keeps the name and the address.
export default function AppLinksSettings() {
  const toast = useToast()
  const [list, setList] = useState<AppLink[] | null>(null)
  const [saved, setSaved] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api
      .listApps()
      .then((l) => {
        setList(l)
        setSaved(JSON.stringify(l))
      })
      .catch(() => setList([]))
  }, [])
  if (!list) return null

  const dirty = JSON.stringify(list) !== saved
  const edit = (i: number, patch: Partial<AppLink>) => setList(list.map((a, j) => (j === i ? { ...a, ...patch } : a)))
  async function save() {
    if (!list) return
    setBusy(true)
    try {
      const out = await api.saveApps(list)
      setList(out)
      setSaved(JSON.stringify(out))
      toast.success(t('Saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="settings-stack">
      <section className="card wide">
        <h2>
          <TipLabel help="media-servers#add-ons">{t('Add-ons')}</TipLabel>
        </h2>
        <p style={{ color: 'var(--text-dim)' }}>
          {t('Other programs you run next to Mediarium, such as a channel guide or a dashboard, can have a link here. They show up in the players menu (the play button at the bottom of the sidebar). Mediarium does not run them or load their code, and each is its own project with its own rules.')}
        </p>
        {list.length === 0 && <p>{t('No add-ons yet.')}</p>}
        {list.map((a, i) => (
          <div key={a.id || i} className="addon-row">
            <select aria-label={t('Icon')} value={a.icon || 'grid'} onChange={(e) => edit(i, { icon: e.target.value })}>
              {ICONS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
            <input aria-label={t('Name')} placeholder={t('Name')} value={a.name} maxLength={40} onChange={(e) => edit(i, { name: e.target.value })} />
            <input aria-label={t('Address')} placeholder="http://192.168.1.20:8080" value={a.url} maxLength={300} onChange={(e) => edit(i, { url: e.target.value })} />
            <select aria-label={t('Opens')} value={a.mode} onChange={(e) => edit(i, { mode: e.target.value as AppLink['mode'] })}>
              <option value="tab">{t('In a new tab')}</option>
              <option value="frame">{t('Inside Mediarium')}</option>
            </select>
            <label className="inline-field">
              <input type="checkbox" checked={a.enabled} onChange={(e) => edit(i, { enabled: e.target.checked })} />
              {t('On')}
            </label>
            <button type="button" className="btn-sm" onClick={() => setList(list.filter((_, j) => j !== i))}>
              {t('Remove')}
            </button>
          </div>
        ))}
        <div className="row-actions">
          <button type="button" className="btn-sm btn-with-icon" disabled={list.length >= 20} onClick={() => setList([...list, { id: '', name: '', url: '', mode: 'tab', icon: 'grid', enabled: true }])}>
            <Icon name="plus" size={14} /> {t('Add an add-on')}
          </button>
          <button type="button" className="primary" disabled={!dirty || busy} onClick={() => void save()}>
            {t('Save')}
          </button>
        </div>
        <p className="field-hint">
          {t('"Inside Mediarium" shows the program in a frame on its own page. Some programs refuse to be shown in a frame; then use "In a new tab". To let an add-on read your library, make it a read-only API key on your Profile page.')}{' '}
          <a href={`${DOCS_URL}/other-apps.md#add-ons`} target="_blank" rel="noreferrer">
            {t('Learn more')}
          </a>
        </p>
        <p className="field-hint">
          <Link to="/settings/security">{t('Your API keys')}</Link>
        </p>
      </section>
    </div>
  )
}
