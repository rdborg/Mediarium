import { useCallback, useEffect, useState } from 'react'
import { api, TORRENT_ROUTE_EVENT, type DebridAccount } from '../api'
import { locale, t, tx } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'
import TipLabel from './TipLabel'

type Service = '' | 'realdebrid' | 'torbox' | 'premiumize'

const KEY_HELP: Record<Exclude<Service, ''>, { label: string; where: string; url: string }> = {
  realdebrid: { label: 'Real-Debrid', where: 'real-debrid.com/apitoken', url: 'https://real-debrid.com/apitoken' },
  torbox: { label: 'TorBox', where: 'torbox.app/settings', url: 'https://torbox.app/settings' },
  premiumize: { label: 'Premiumize', where: 'premiumize.me/account', url: 'https://www.premiumize.me/account' },
}

// Settings > Downloading: send torrent releases to a debrid service, which
// downloads them on its own servers; Mediarium then brings the files home.
export default function DebridCard() {
  const toast = useToast()
  const [service, setService] = useState<Service>('')
  const [savedService, setSavedService] = useState<Service>('')
  const [hasKey, setHasKey] = useState(false)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; account?: DebridAccount; error?: string } | null>(null)
  // TorBox and Premiumize can say which releases they already have: ask, and prefer those.
  const [preferCached, setPreferCached] = useState(true)
  const [lowerScore, setLowerScore] = useState(false)
  const [savedCached, setSavedCached] = useState({ preferCached: true, cachedLowerScore: false })

  const load = useCallback(() => {
    api
      .debrid()
      .then((d) => {
        setService(d.service)
        setSavedService(d.service)
        setHasKey(d.hasKey)
        setPreferCached(d.preferCached)
        setLowerScore(d.cachedLowerScore)
        setSavedCached({ preferCached: d.preferCached, cachedLowerScore: d.cachedLowerScore })
      })
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])
  // Saving a download client no longer turns the debrid service off, but the card reloads anyway.
  useEffect(() => {
    window.addEventListener(TORRENT_ROUTE_EVENT, load)
    return () => window.removeEventListener(TORRENT_ROUTE_EVENT, load)
  }, [load])

  const keyKept = hasKey && service === savedService && key === ''
  const help = service ? KEY_HELP[service] : null

  async function test() {
    if (!service) return
    setBusy(true)
    setResult(null)
    try {
      setResult(await api.testDebrid(service, key))
    } catch (e) {
      setResult({ ok: false, error: e instanceof Error ? e.message : String(e) })
    } finally {
      setBusy(false)
    }
  }

  async function save() {
    setBusy(true)
    try {
      const d = await api.saveDebrid(service, key, { preferCached, cachedLowerScore: lowerScore })
      setSavedService(d.service)
      setHasKey(d.hasKey)
      setSavedCached({ preferCached: d.preferCached, cachedLowerScore: d.cachedLowerScore })
      setKey('')
      window.dispatchEvent(new Event(TORRENT_ROUTE_EVENT))
      toast.success(d.service ? t('Saved. Torrent releases now go to {name}.', { name: KEY_HELP[d.service as 'realdebrid'].label }) : t('Saved. Torrents are downloaded here again.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const changed = service !== savedService || key !== '' || preferCached !== savedCached.preferCached || lowerScore !== savedCached.cachedLowerScore
  return (
    <fieldset className="group torrent">
      <legend>
        <Icon name="cloud" size={14} /> <TipLabel help="downloads#debrid-services-real-debrid-torbox-premiumize">{t('Debrid service')}</TipLabel>
      </legend>
      <p style={{ marginTop: 0 }}>
        {t('A debrid service (Real-Debrid, TorBox, Premiumize) downloads torrents on its own fast servers. Mediarium then downloads the finished files from it over https, so no torrent traffic leaves your home and nothing is seeded from here. The built-in torrent client can stay off.')}
      </p>
      <div className="form-cols">
        <label>
          {t('Service')}
          <select
            value={service}
            onChange={(e) => {
              setService(e.target.value as Service)
              setResult(null)
            }}
          >
            <option value="">{t('None (download torrents here)')}</option>
            <option value="realdebrid">Real-Debrid</option>
            <option value="torbox">TorBox</option>
            <option value="premiumize">Premiumize</option>
          </select>
        </label>
        {service && (
          <label>
            {t('API key')}
            <input
              type="password"
              value={key}
              onChange={(e) => {
                setKey(e.target.value)
                setResult(null)
              }}
              placeholder={keyKept ? t('Saved. Type a new one to replace it.') : ''}
              autoComplete="off"
              spellCheck={false}
            />
            {help && (
              <small className="field-help">
                {tx('Find it at {link} while signed in.', {
                  link: (
                    <a href={help.url} target="_blank" rel="noreferrer">
                      {help.where}
                    </a>
                  ),
                })}
              </small>
            )}
          </label>
        )}
      </div>
      {(service === 'torbox' || service === 'premiumize') && (
        <>
          <label className="check-hint">
            <input type="checkbox" checked={preferCached} onChange={(e) => setPreferCached(e.target.checked)} />
            <span>
              <TipLabel help="downloads#releases-that-are-already-cached-torbox-and-premiumize">{t('Prefer releases {name} already has', { name: help?.label ?? '' })}</TipLabel>
              <small>{t('Mediarium asks {name} which of the torrents it found are already stored there, marks them "Cached" in the release list, and picks one of those over an equal release that has to be downloaded first. It never picks a worse quality for it. Only the torrents\' hashes are sent.', { name: help?.label ?? '' })}</small>
            </span>
          </label>
          {preferCached && (
            <label className="check-hint">
              <input type="checkbox" checked={lowerScore} onChange={(e) => setLowerScore(e.target.checked)} />
              <span>
                {t('Accept a lower score for a cached release')}
                <small>{t('Off: a cached release only wins when the quality and the score from your profile are equal. On: it wins over a higher-scoring release of the same quality.')}</small>
              </span>
            </label>
          )}
        </>
      )}
      {service === 'realdebrid' && <p style={{ color: 'var(--text-dim)' }}>{t('Real-Debrid no longer tells other apps which torrents it has cached, so releases are not marked or preferred for that.')}</p>}
      <div className="form-actions" style={{ justifyContent: 'flex-start' }}>
        {service && (
          <button onClick={() => void test()} disabled={busy || (!key && !keyKept)}>
            {busy ? t('Testing…') : t('Test')}
          </button>
        )}
        <button className="primary" onClick={() => void save()} disabled={busy || !changed || (service !== '' && !key && !keyKept)}>
          {t('Save')}
        </button>
      </div>
      {result && (
        <div className={`indexer-result ${result.ok ? 'ok' : 'bad'}`}>
          <Icon name={result.ok ? 'check' : 'warning'} size={14} />{' '}
          {result.ok && result.account
            ? [
                t('Connected as {name}.', { name: result.account.name || '?' }),
                result.account.premium
                  ? result.account.expires
                    ? t('Premium until {date}.', { date: new Date(result.account.expires).toLocaleDateString(locale()) })
                    : t('Premium.')
                  : t('This account is not premium, so most downloads will be refused.'),
                result.account.fairUse ? t('{percent}% of this month\'s fair-use limit is used.', { percent: Math.round(result.account.fairUse * 100) }) : '',
              ]
              .filter(Boolean)
              .join(' ')
            : result.error}
        </div>
      )}
    </fieldset>
  )
}
