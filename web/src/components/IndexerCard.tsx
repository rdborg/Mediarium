import { useEffect, useState } from 'react'
import { api, type IndexerConfig, type TorrentRoute } from '../api'
import { timeAgo } from '../format'
import Icon from './Icon'
import { friendlyIndexerError } from './IndexerForm'
import SeedRules from './SeedRules'
import { useToast } from './Toast'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { apiKey as apiKeyCheck, firstError, maxLength, numberRange, required, url as urlCheck } from '../validate'
import { plural, t, st } from '../i18n'
import { untilText } from '../delayText'
import TipLabel from './TipLabel'
import KnownFixHint from './KnownFixHint'
import { EXTRA_KINDS, addToText, buildExtra, suggestionsFor, textsFrom, type CapCategory, type ExtraKind } from '../extraCategories'

// How long a site test runs before the card explains the wait.
const SLOW_TEST_HINT_MS = 5000

// One saved indexer: name, type and address on top, actions underneath, the
// last test result on its own line, and an inline form to edit it.
export default function IndexerCard({ indexer: i, onChanged, onToggle, onRemove }: { indexer: IndexerConfig; onChanged: () => void; onToggle: () => void; onRemove: () => void }) {
  const toast = useToast()
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; message: string; detail?: string } | null>(null)
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(i.name)
  const [baseUrl, setBaseUrl] = useState(i.baseUrl)
  const [apiKey, setApiKey] = useState('')
  const [perDay, setPerDay] = useState(String(i.requestsPerDay ?? 0))
  const [saving, setSaving] = useState(false)
  const [prio, setPrio] = useState(String(i.priority ?? 25))
  const [minSeed, setMinSeed] = useState(String(i.minSeeders ?? 0))
  const site = i.kind === 'cardigann'
  // Extra category numbers per kind of media, as typed; the suggestions are what the indexer lists.
  const [extraText, setExtraText] = useState(() => textsFrom(i.extraCategories))
  const [caps, setCaps] = useState<CapCategory[] | null>(null)
  const [capsLoading, setCapsLoading] = useState(false)
  const extra = buildExtra(extraText)
  const kindNames: Record<ExtraKind, string> = { movies: t('Movies'), tv: t('TV'), music: t('Music'), books: t('Books'), audiobooks: t('Audiobooks') }
  // A site behind a Cloudflare check can take up to a minute to test, so after a
  // few seconds the card says what it is waiting for.
  const [slowTest, setSlowTest] = useState(false)
  useEffect(() => {
    if (!testing || !site) {
      setSlowTest(false)
      return
    }
    const timer = window.setTimeout(() => setSlowTest(true), SLOW_TEST_HINT_MS)
    return () => window.clearTimeout(timer)
  }, [testing, site])
  const v = useValidation({
    name: firstError(required(name, t('Give this indexer a name.')), maxLength(name, 80, t('The name'))),
    perDay: numberRange(perDay, t('Requests per day'), { min: 0, max: 100000 }),
    ...(site ? {} : Object.fromEntries(EXTRA_KINDS.map((k) => [`extra_${k}`, extra.errors[k] ?? null]))),
    ...(site
      ? i.localSite
        ? { baseUrl: firstError(required(baseUrl, t('Add the address of the indexer, for example {example}.', { example: 'http://192.168.1.10:3333' })), urlCheck(baseUrl, { requireScheme: true, example: 'http://192.168.1.10:3333' }), maxLength(baseUrl, 200, t('The address'))) }
        : {}
      : {
          baseUrl: firstError(required(baseUrl, t('Add the address of the indexer, for example {example}.', { example: 'https://api.example.com' })), urlCheck(baseUrl, { example: 'https://api.example.com' })),
          apiKey: apiKeyCheck(apiKey),
        }),
  })

  async function test() {
    setTesting(true)
    setResult(null)
    try {
      const r = await api.testIndexer(i.id)
      setResult({ ok: r.ok, message: r.ok ? r.message : friendlyIndexerError(r.message), detail: r.ok ? undefined : r.detail })
    } catch (e) {
      setResult({ ok: false, message: e instanceof Error ? e.message : String(e) })
    } finally {
      setTesting(false)
    }
  }

  // Searches skip an indexer that keeps failing; this asks it again at once.
  const pausedUntil = i.pausedUntil ? new Date(i.pausedUntil) : null
  const paused = !!pausedUntil // the server only sends it while the pause lasts
  async function tryAgain() {
    try {
      await api.resumeIndexer(i.id)
      toast.success(t('{name} will be asked again by the next search.', { name: i.name }))
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  // The priority is saved when the box is left or Enter is pressed, not on every key.
  async function savePriority() {
    const n = Number(prio)
    if (prio.trim() === '' || !Number.isInteger(n) || n < 1 || n > 50) {
      toast.error(t('Priority must be a whole number from 1 to 50.'))
      setPrio(String(i.priority ?? 25))
      return
    }
    if (n === (i.priority ?? 25)) return
    try {
      await api.setIndexerPriority(i.id, n)
      toast.success(t('{name} now has priority {n}.', { name: i.name, n }))
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setPrio(String(i.priority ?? 25))
    }
  }

  // Two short lines: what the daily limit and the feed check say, then the last test.
  const limitText = i.dailyLimit ? (i.limitNote ? st(i.limitNote) : t('{used} of {limit} requests today', { used: i.requestsToday ?? 0, limit: i.dailyLimit })) : ''
  const limitBad = !!i.dailyLimit && !!i.limitNote
  const info = [limitText, i.rssNote ? st(i.rssNote) : ''].filter(Boolean).join(' \u00b7 ')
  const testFull = i.lastTestAt ? (i.lastTestError ? t('Last test failed {when}: {error}', { when: timeAgo(i.lastTestAt), error: friendlyIndexerError(i.lastTestError) }) : t('Last test passed {when}', { when: timeAgo(i.lastTestAt) })) : ''
  // A pass from more than a day ago says little any more, so it is shown dimmer. A failure never fades.
  const testOld = !!i.lastTestAt && !i.lastTestError && Date.now() - new Date(i.lastTestAt).getTime() > 86400000

  // The minimum is saved the same way: when the box is left or Enter is pressed.
  async function saveMinSeeders() {
    const n = Number(minSeed)
    if (minSeed.trim() === '' || !Number.isInteger(n) || n < 0 || n > 1000) {
      toast.error(t('Minimum seeders must be a whole number from 0 to 1000. Use 0 to turn it off.'))
      setMinSeed(String(i.minSeeders ?? 0))
      return
    }
    if (n === (i.minSeeders ?? 0)) return
    try {
      await api.updateIndexer(i.id, { minSeeders: n })
      toast.success(n === 0 ? t('{name} no longer skips releases for too few seeders (0 means off).', { name: i.name }) : t('{name} now skips releases with fewer seeders than {n}.', { name: i.name, n }))
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setMinSeed(String(i.minSeeders ?? 0))
    }
  }

  // Saved as soon as another place is picked.
  async function saveRoute(route: TorrentRoute) {
    try {
      await api.updateIndexer(i.id, { torrentRoute: route })
      const done: Record<TorrentRoute, string> = {
        '': t('Torrents from {name} go the usual way again.', { name: i.name }),
        client: t('Torrents from {name} now go to the download client.', { name: i.name }),
        debrid: t('Torrents from {name} now go to the debrid service.', { name: i.name }),
        builtin: t('Torrents from {name} now go to the built-in client.', { name: i.name }),
      }
      toast.success(done[route])
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const routeNames: Record<TorrentRoute, string> = { '': t('The usual way'), client: t('The download client'), debrid: t('The debrid service'), builtin: t('The built-in client') }

  async function showCategories() {
    setCapsLoading(true)
    try {
      setCaps((await api.indexerCategories(i.id)).categories)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setCapsLoading(false)
    }
  }

  async function save() {
    if (!v.attempt()) return
    setSaving(true)
    try {
      await api.updateIndexer(i.id, { name: name.trim(), requestsPerDay: Number(perDay.trim() || '0'), ...(site ? (i.localSite ? { baseUrl: baseUrl.trim() } : {}) : { baseUrl: baseUrl.trim(), apiKey: apiKey.trim(), extraCategories: extra.extra }) })
      toast.success(t('{name} saved.', { name: name.trim() || i.name }))
      setEditing(false)
      setApiKey('')
      v.reset()
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className={`indexer-card${i.enabled ? '' : ' is-off'}`}>
      <div className="indexer-top">
        <span className={`tile-ico proto-${i.protocol}`}>
          <Icon name={i.protocol === 'torrent' ? 'magnet' : 'server'} size={18} />
        </span>
        <div className="indexer-info">
          <div className="indexer-title">
            <strong title={i.name}>{i.name}</strong>
            <span className="badge">{i.protocol === 'torrent' ? 'Torrent' : 'Usenet'}</span>
            {i.fromProwlarr && <span className="badge" title={t('Kept in step with your Prowlarr. Change its name, address or on/off switch in Prowlarr.')}>{t('From Prowlarr')}</span>}
            {!i.enabled && <span className="badge">{t('Off')}</span>}
          </div>
          <small className="indexer-url" title={i.baseUrl}>
            {i.baseUrl}
          </small>
          {paused && pausedUntil && (
            <small className="indexer-health bad">
              <TipLabel text={t('After 3 failures in a row an indexer is left out of searches for 5 minutes, then 10, 20, 40 and so on, up to 6 hours. Try again now clears it.')} learnMore="indexers#when-an-indexer-keeps-failing">
                {t('Paused until {time} after {failures} in a row, because {why}.', {
                  time: untilText(i.pausedUntil ?? ''),
                  failures: plural(i.failures ?? 0, '{n} failure', '{n} failures'),
                  why: i.pausedWhy ?? t('it returned an error'),
                })}
              </TipLabel>
            </small>
          )}
          {paused && <KnownFixHint error={i.pausedWhy} />}
          {info && (
            <small className={`indexer-health idx-line idx-info ${limitBad ? 'bad' : 'ok'}`} title={info}>
              <span className="idx-text">{info}</span>
            </small>
          )}
          {!result && i.lastTestAt && (i.lastTestError ? (
            <details className="idx-more">
              <summary className="indexer-health idx-line bad" title={testFull}>
                <span className="idx-dot" aria-hidden="true" />
                <span className="idx-text">{t('Failed {when}', { when: timeAgo(i.lastTestAt) })}</span>
              </summary>
              <small className="idx-why">{friendlyIndexerError(i.lastTestError)}</small>
            </details>
          ) : (
            <small className={`indexer-health idx-line ok${testOld ? ' old' : ''}`} title={testFull}>
              <span className="idx-dot" aria-hidden="true" />
              <span className="idx-text">{t('Passed {when}', { when: timeAgo(i.lastTestAt) })}</span>
            </small>
          ))}
          {!result && !!i.lastTestAt && <KnownFixHint error={i.lastTestError} />}
        </div>
        <label className="indexer-priority">
          <TipLabel text={t('Lower number = tried first. Quality always comes first. Between two equally good releases, the one with the lower number wins.')} learnMore="indexers#priority-health-and-test-all">{t('Priority')}</TipLabel>
          <input type="number" min={1} max={50} step={1} value={prio} style={{ width: 72 }} onChange={(e) => setPrio(e.target.value)} onBlur={() => void savePriority()} onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur() }} />
        </label>
      </div>

      {testing && slowTest && (
        <div className="indexer-result" role="status">
          <Icon name="refresh" size={14} /> {t('Solving the Cloudflare check, this can take up to a minute.')}
        </div>
      )}
      {result && (
        <div className={`indexer-result ${result.ok ? 'ok' : 'bad'}`}>
          <Icon name={result.ok ? 'check' : 'warning'} size={14} /> {st(result.message)}
          {!result.ok && <KnownFixHint error={result.message} />}
          {!result.ok && result.detail && <small className="idx-detail">{result.detail}</small>}
        </div>
      )}

      {i.protocol === 'torrent' && (
        <label className="indexer-minseed">
          <TipLabel text={t('Releases from this indexer with fewer seeders than this are skipped. Releases that a debrid service already has cached are not skipped. 0 means off.')} learnMore="indexers#minimum-seeders">{t('Minimum seeders')}</TipLabel>
          <input type="number" min={0} max={1000} step={1} value={minSeed} onChange={(e) => setMinSeed(e.target.value)} onBlur={() => void saveMinSeeders()} onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur() }} />
        </label>
      )}
      {i.protocol === 'torrent' && (
        <label className="indexer-minseed">
          <TipLabel text={t('Send the torrents from this indexer to one place, for example a private tracker to your download client so it seeds there, and public ones to debrid. If the place you pick is not set up, the usual way is used: the download client, then debrid, then the built-in client.')} learnMore="indexers#where-torrents-go">{t('Where torrents go')}</TipLabel>
          <select value={i.torrentRoute ?? ''} onChange={(e) => void saveRoute(e.target.value as TorrentRoute)} style={{ width: 'auto' }}>
            {(['', 'client', 'debrid', 'builtin'] as TorrentRoute[]).map((r) => (
              <option key={r} value={r}>{routeNames[r]}</option>
            ))}
          </select>
        </label>
      )}
      {i.protocol === 'torrent' && <SeedRules indexerId={i.id} />}

      <div className="indexer-actions">
        <button className="btn-sm" onClick={() => void test()} disabled={testing}>
          {testing ? t('Testing…') : t('Test')}
        </button>
        {paused && (
          <button className="btn-sm" onClick={() => void tryAgain()}>
            {t('Try again now')}
          </button>
        )}
        <button className="btn-sm" onClick={() => setEditing((v) => !v)} aria-expanded={editing}>
          {editing ? t('Cancel') : t('Edit')}
        </button>
        <button className="btn-sm" onClick={onToggle}>
          {i.enabled ? t('Disable') : t('Enable')}
        </button>
        <button className="btn-sm btn-danger" onClick={onRemove}>
          {t('Remove')}
        </button>
      </div>

      {editing && (
        <div className="indexer-edit grid-form">
          <label>
            {t('Name')}
            <input value={name} onChange={(e) => setName(e.target.value)} {...v.bind('name', name, setName)} />
            <FieldError v={v} name="name" />
          </label>
          {site ? (
            <>
              {i.localSite && (
                <label>
                  {t('Address')}
                  <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} spellCheck={false} {...v.bind('baseUrl', baseUrl, setBaseUrl)} />
                  <FieldError v={v} name="baseUrl" />
                  <small style={{ color: 'var(--text-dim)', fontWeight: 400 }}>{t('The address of your own server, for example http://192.168.1.10:3333. Inside Docker, 127.0.0.1 is the Mediarium container itself.')}</small>
                </label>
              )}
              <small style={{ color: 'var(--text-dim)' }}>{t('This site was added from the list. To change its sign-in details, remove it and add it again.')}</small>
            </>
          ) : (
            <>
              <label>
                {t('Address')}
                <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} {...v.bind('baseUrl', baseUrl, setBaseUrl)} />
                <FieldError v={v} name="baseUrl" />
              </label>
              <label>
                {t('API key')}
                <input value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={t('Leave empty to keep the saved key')} autoComplete="off" {...v.bind('apiKey', apiKey, setApiKey)} />
                <FieldError v={v} name="apiKey" />
              </label>
            </>
          )}
          <label>
            <TipLabel text={t('The most requests a day Mediarium makes to this indexer. Free indexers often allow only a few, such as 25. Leave it at 0 to use the limit the indexer reports, if it reports one.')} learnMore="indexers#daily-limits">{t('Requests per day')}</TipLabel>
            <input type="number" min={0} max={100000} value={perDay} onChange={(e) => setPerDay(e.target.value)} {...v.bind('perDay', perDay, setPerDay)} />
            <FieldError v={v} name="perDay" />
          </label>
          {!site && (
            <div className="indexer-extra">
              <TipLabel text={t('Category numbers your indexer files releases under besides the standard ones, for example 105000 for TV or 7120 for German books. They are searched together with the standard categories. Separate several with commas.')} learnMore="indexers#extra-categories">{t('Extra categories')}</TipLabel>
              {EXTRA_KINDS.map((k) => {
                const hints = caps ? suggestionsFor(k, caps, extra.extra[k] ?? []) : []
                return (
                  <div key={k} className="indexer-extra-row">
                    <label>
                      <span>{kindNames[k]}</span>
                      <input value={extraText[k]} onChange={(e) => setExtraText({ ...extraText, [k]: e.target.value })} placeholder={t('For example {example}', { example: k === 'books' ? '7120' : '105000' })} spellCheck={false} inputMode="numeric" {...v.bind(`extra_${k}`, extraText[k], (val: string) => setExtraText({ ...extraText, [k]: val }))} />
                    </label>
                    <FieldError v={v} name={`extra_${k}`} />
                    {hints.length > 0 && (
                      <div className="indexer-extra-hints">
                        {hints.map((c) => (
                          <button key={c.id} type="button" className="btn-sm" title={c.name} onClick={() => setExtraText({ ...extraText, [k]: addToText(extraText[k], c.id) })}>
                            {c.id} {c.name}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                )
              })}
              {caps === null ? (
                <button type="button" className="btn-sm" onClick={() => void showCategories()} disabled={capsLoading}>
                  {capsLoading ? t('Asking the indexer…') : t('Show the categories this indexer lists')}
                </button>
              ) : (
                caps.length === 0 && <small style={{ color: 'var(--text-dim)' }}>{t('The indexer did not list any categories.')}</small>
              )}
            </div>
          )}
          <div>
            <button className="primary btn-sm" onClick={() => void save()} disabled={saving}>
              {saving ? t('Saving…') : t('Save changes')}
            </button>
            <FormProblem v={v} />
          </div>
        </div>
      )}
    </div>
  )
}
