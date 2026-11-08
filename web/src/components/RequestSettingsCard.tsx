import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type QualityProfile, type RequestSettings } from '../api'
import { t, tx } from '../i18n'
import Icon from './Icon'
import { sortProfiles } from './qualityBlurb'
import Switch from './Switch'
import { useToast } from './Toast'
import './RequestPortal.css'
import TipLabel from './TipLabel'

// Settings > Account & Access > All accounts: the request portal. Messages to the
// person who asked, a limit on requests, 4K requests and problem reports.
// Everything is off or unlimited until it is set here.
export default function RequestSettingsCard() {
  const toast = useToast()
  const [data, setData] = useState<RequestSettings | null>(null)
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [busy, setBusy] = useState(false)
  const [count, setCount] = useState('')

  const load = useCallback(() => {
    api
      .requestSettings()
      .then((d) => {
        setData(d)
        setCount(d.options.quotaCount > 0 ? String(d.options.quotaCount) : '')
      })
      .catch(() => setData(null))
  }, [])
  useEffect(load, [load])
  useEffect(() => {
    api.listProfiles().then((r) => setProfiles(sortProfiles(r.profiles))).catch(() => undefined)
  }, [])

  async function save(body: Parameters<typeof api.putRequestSettings>[0], done: string) {
    setBusy(true)
    try {
      const d = await api.putRequestSettings(body)
      setData(d)
      setCount(d.options.quotaCount > 0 ? String(d.options.quotaCount) : '')
      toast.success(done)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!data) return null
  const o = data.options
  const askers = data.accounts.filter((a) => a.mustAsk)
  const members = data.accounts.filter((a) => !a.admin)
  // The ratings an age limit can be set to; each stands for its whole level (PG-13 and TV-14 count the same).
  const ageChoices: { value: string; label: string }[] = [
    { value: '', label: t('No age limit') },
    { value: 'G', label: t('Up to G and TV-G (all ages)') },
    { value: 'PG', label: t('Up to PG and TV-PG') },
    { value: 'PG-13', label: t('Up to PG-13 and TV-14') },
    { value: 'R', label: t('Up to R and TV-MA') },
  ]
  const suggested = profiles.find((p) => /4k|2160/i.test(p.name))
  return (
    <fieldset className="group span-all rp-card">
      <legend>
        <Icon name="list" size={14} /> {t('Requests')}
      </legend>
      <p className="rp-lead">
        {t('What people see when they ask for a title instead of adding it (an account that may not add titles by itself), and when they report a problem. Everything here is off or unlimited until you change it.')}
      </p>

      <Switch
        checked={o.notifyRequester}
        disabled={busy}
        onChange={(v) => void save({ notifyRequester: v }, v ? t('People are told when their request is answered.') : t('People are no longer sent messages.'))}
        label={t('Tell people when their request is answered')}
        description={t('They get a message when a request is approved, declined or ready to watch, and when their report is answered. It goes by email (needs an email notification under Settings > Connections > Notifications and an address on their account) and to the phones and browsers they allow notifications on in their profile. Each person can switch it off for themselves.')}
        showState
      />

      <div className="rp-block">
        <h3 className="rp-h3"><TipLabel help="accounts#the-request-portal-settings">{t('Limit requests')}</TipLabel></h3>
        <div className="rp-row">
          <label>
            <TipLabel text={t('How many requests one person can have waiting or approved in the period you choose. Empty means no limit, and admins are never limited.')}>{t('Requests each person may make')}</TipLabel>
            <input
              type="number"
              min={0}
              max={1000}
              value={count}
              placeholder={t('No limit')}
              onChange={(e) => setCount(e.target.value)}
              onBlur={() => {
                const n = Math.max(0, Math.min(1000, Math.floor(Number(count) || 0)))
                if (n !== o.quotaCount) void save({ quotaCount: n }, n > 0 ? t('Saved: {n} requests each.', { n }) : t('Saved: no limit on requests.'))
              }}
            />
          </label>
          <label>
            {t('Counted over')}
            <select value={o.quotaPeriod} disabled={busy} onChange={(e) => void save({ quotaPeriod: e.target.value as 'day' | 'week' }, t('Saved.'))}>
              <option value="day">{t('the last day')}</option>
              <option value="week">{t('the last week')}</option>
            </select>
          </label>
        </div>
        <p className="field-hint">{t('Leave it empty for no limit. Waiting and approved requests count; declined ones and requests taken back do not. Administrators are never limited.')}</p>
        {askers.length > 0 && (
          <table className="rp-people">
            <thead>
              <tr>
                <th>{t('Person')}</th>
                <th>{t('Their own limit')}</th>
              </tr>
            </thead>
            <tbody>
              {askers.map((a) => (
                <tr key={a.id}>
                  <td>{a.name}</td>
                  <td>
                    <select
                      value={a.quota < 0 ? 'default' : a.quota === 0 ? 'none' : 'custom'}
                      disabled={busy}
                      aria-label={t('Limit for {name}', { name: a.name })}
                      onChange={(e) => {
                        const v = e.target.value
                        const n = v === 'default' ? -1 : v === 'none' ? 0 : Math.max(o.quotaCount, 1)
                        void save({ quotas: { [String(a.id)]: n } }, t('Saved.'))
                      }}
                    >
                      <option value="default">{o.quotaCount > 0 ? t('The one above ({n})', { n: o.quotaCount }) : t('The one above (no limit)')}</option>
                      <option value="none">{t('No limit')}</option>
                      <option value="custom">{t('A number of their own')}</option>
                    </select>{' '}
                    {a.quota > 0 && (
                      <input
                        type="number"
                        min={1}
                        max={1000}
                        defaultValue={a.quota}
                        aria-label={t('Requests for {name}', { name: a.name })}
                        style={{ width: 80 }}
                        onBlur={(e) => {
                          const n = Math.max(1, Math.min(1000, Math.floor(Number(e.target.value) || 1)))
                          if (n !== a.quota) void save({ quotas: { [String(a.id)]: n } }, t('Saved.'))
                        }}
                      />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {members.length > 0 && (
        <div className="rp-block">
          <h3 className="rp-h3"><TipLabel help="accounts#the-request-portal-settings">{t('Age limits')}</TipLabel></h3>
          <p className="field-hint">{t('The most mature rating a person may ask for or add. Ratings from different countries are put on one scale, so PG-13 and TV-14 count the same. A title with no rating is allowed unless you tick the box.')}</p>
          <table className="rp-people rp-age">
            <thead>
              <tr>
                <th>{t('Person')}</th>
                <th>{t('Highest rating')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {members.map((a) => {
                const known = ageChoices.some((c) => c.value === a.maxRating)
                return (
                  <tr key={a.id}>
                    <td>{a.name}</td>
                    <td>
                      <select
                        value={a.maxRating}
                        disabled={busy}
                        aria-label={t('Age limit for {name}', { name: a.name })}
                        onChange={(e) => void save({ ageLimits: { [String(a.id)]: { maxRating: e.target.value, blockUnrated: e.target.value === '' ? false : a.blockUnrated } } }, t('Saved.'))}
                      >
                        {!known && <option value={a.maxRating}>{t('Up to {rating}', { rating: a.maxRating })}</option>}
                        {ageChoices.map((c) => (
                          <option key={c.value} value={c.value}>
                            {c.label}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td>
                      {a.maxRating !== '' && (
                        <label className="rp-unrated">
                          <input
                            type="checkbox"
                            checked={a.blockUnrated}
                            disabled={busy}
                            onChange={(e) => void save({ ageLimits: { [String(a.id)]: { maxRating: a.maxRating, blockUnrated: e.target.checked } } }, t('Saved.'))}
                          />
                          {t('Also refuse titles with no rating')}
                        </label>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className="rp-block">
        <h3 className="rp-h3"><TipLabel help="accounts#the-request-portal-settings">{t('4K requests')}</TipLabel></h3>
        <Switch
          checked={o.fourK}
          disabled={busy || (!o.fourK && o.fourKProfileId <= 0)}
          onChange={(v) => void save({ fourK: v }, v ? t('People can ask for a 4K copy.') : t('People can no longer ask for a 4K copy.'))}
          label={t('Let people ask for a 4K copy')}
          description={t('A request can ask for a second copy of the title in 4K. When you approve it, the title is added as usual if it is not there yet, and then a second copy in the quality below.')}
          showState
        />
        <div className="rp-row">
          <label>
            <TipLabel text={t('The quality profile the extra 4K copy is downloaded with, for example 4K & over. It also needs a second-copies folder under Settings > Library > Folders.')}>{t('Quality of the 4K copy')}</TipLabel>
            <select
              value={o.fourKProfileId}
              disabled={busy}
              onChange={(e) => void save({ fourKProfileId: Number(e.target.value) }, t('Saved.'))}
            >
              <option value={0}>{t('Choose a quality profile')}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          {o.fourKProfileId <= 0 && suggested && (
            <button className="btn-sm" disabled={busy} onClick={() => void save({ fourKProfileId: suggested.id, fourK: true }, t('People can ask for a 4K copy.'))}>
              {t('Use {name} and switch it on', { name: suggested.name })}
            </button>
          )}
        </div>
        {o.fourK && (!data.folders.movie || !data.folders.tv) && (
          <p className="field-hint">
            {tx('Second copies need a folder of their own, set in {link}. Without one, 4K is not offered for {kinds}.', {
              link: <Link to="/settings/media">{t('Settings > Library > Folders')}</Link>,
              kinds: !data.folders.movie && !data.folders.tv ? t('movies or shows') : !data.folders.movie ? t('movies') : t('shows'),
            })}
          </p>
        )}
      </div>

      <div className="rp-block">
        <h3 className="rp-h3"><TipLabel help="accounts#the-request-portal-settings">{t('Problem reports')}</TipLabel></h3>
        <Switch
          checked={o.reports}
          disabled={busy}
          onChange={(v) => void save({ reports: v }, v ? t('People can report a problem with a title.') : t('Reports are switched off.'))}
          label={t('Let people report a problem with a title')}
          description={t('Everyone signed in gets a "Report a problem" button on movie and show pages (wrong file, no sound, bad subtitles). The reports are listed under Activity > Reports, where you answer them.')}
          showState
        />
      </div>
    </fieldset>
  )
}
