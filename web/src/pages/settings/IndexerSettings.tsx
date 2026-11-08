import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, type FlareSolverrStatus, type IndexerConfig } from '../../api'
import Icon from '../../components/Icon'
import IndexerCard from '../../components/IndexerCard'
import IndexerForm from '../../components/IndexerForm'
import IrcAnnounceCard from '../../components/IrcAnnounceCard'
import ProwlarrLinkCard from '../../components/ProwlarrLinkCard'
import { useToast } from '../../components/Toast'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'
import { FieldError, FormProblem, useValidation } from '../../useValidation'
import { url as urlCheck } from '../../validate'
import { t, tx } from '../../i18n'
import TipLabel from '../../components/TipLabel'
import { FILTER_FROM, sortIndexers, visibleIndexers } from './indexerOrder'

// Indexers: search services for releases. One form adds either kind (Usenet or
// torrent); everything you have added is listed together on the right.
export default function IndexerSettings() {
  const confirm = useConfirm()
  const toast = useToast()
  const [testingAll, setTestingAll] = useState(false)

  // Tests every enabled indexer, one after another, then says how many passed.
  async function testAll() {
    if (!list) return
    setTestingAll(true)
    const on = list.filter((i) => i.enabled)
    const failed: string[] = []
    for (const i of on) {
      try {
        const r = await api.testIndexer(i.id)
        if (!r.ok) failed.push(i.name)
      } catch {
        failed.push(i.name)
      }
    }
    setTestingAll(false)
    reload()
    if (failed.length === 0) toast.success(on.length === 1 ? t('The indexer answered.') : t('All {n} indexers answered.', { n: on.length }))
    else toast.error(t('{failed} of {total} did not answer: {names}.', { failed: failed.length, total: on.length, names: failed.join(', ') }))
  }
  const [list, setList] = useState<IndexerConfig[] | null>(null)
  const [query, setQuery] = useState('')
  const [torrentOn, setTorrentOn] = useState(true)
  const [flare, setFlare] = useState('')
  const [flareSaved, setFlareSaved] = useState('')
  const [bundled, setBundled] = useState(false)
  const [flareState, setFlareState] = useState<FlareSolverrStatus | null>(null)
  // Blank is fine: it turns the helper off (or falls back to the built-in one).
  const v = useValidation({ flare: urlCheck(flare, { requireScheme: true, example: 'http://flaresolverr:8191' }) })

  const shown = useMemo(() => (list ? visibleIndexers(list, query) : []), [list, query])

  const reload = useCallback(() => {
    // A refresh that fails keeps what is showing instead of claiming there are none.
    api.listIndexers().then((l) => setList(sortIndexers(l))).catch(() => setList((cur) => cur ?? []))
  }, [])
  useEffect(reload, [reload])
  useLive(reload, 15000)
  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setTorrentOn(s.torrentEnabled !== false)
        setFlare(s.flareSolverrUrl ?? '')
        setFlareSaved(s.flareSolverrUrl ?? '')
        setBundled(s.flareSolverrBundled === true)
      })
      .catch(() => undefined)
  }, [])
  const loadFlareState = useCallback(() => {
    api.flareSolverrStatus().then(setFlareState).catch(() => undefined)
  }, [])
  useEffect(loadFlareState, [loadFlareState, flareSaved])
  useLive(loadFlareState, 20000)

  async function saveFlare() {
    if (!v.attempt()) return
    try {
      const s = await api.putSettings({ flareSolverrUrl: flare.trim() })
      setFlareSaved(s.flareSolverrUrl ?? '')
      toast.success(flare.trim() ? t('FlareSolverr address saved.') : t('FlareSolverr turned off.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove(i: IndexerConfig) {
    if (!(await confirm({ title: t('Remove {name}?', { name: i.name }), body: <p>{t('Searches stop using it. You can add it again later.')}</p>, confirmLabel: t('Remove indexer'), danger: true }))) return
    try {
      await api.deleteIndexer(i.id)
      toast.success(t('{name} removed.', { name: i.name }))
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggle(i: IndexerConfig) {
    try {
      await api.setIndexerEnabled(i.id, !i.enabled)
      toast.success(i.enabled ? t('{name} disabled.', { name: i.name }) : t('{name} enabled.', { name: i.name }))
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="settings-stack">
      <p className="span-all" style={{ color: 'var(--text-dim)', margin: 0 }}>
        {tx('{indexers} are the sites Mediarium searches for downloads. Sign up on one, then add its address and API key here.', { indexers: <strong>{t('Indexers')}</strong> })}
      </p>
      <div className="indexer-cols span-all">
      <fieldset className="group usenet indexer-add">
        <legend>
          <Icon name="plus" size={14} /> {t('Add an indexer')}
        </legend>
        <IndexerForm onAdded={reload} torrentDisabled={!torrentOn} />
      </fieldset>
      <fieldset className="group folders indexer-list">
        <legend>
          <Icon name="list" size={14} /> {t('Your indexers')} {list && <small>({list.length})</small>}
        </legend>
        {list && list.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>{t('None yet. Pick a site from the list above, or paste a link.')}</p>}
        {list && list.length > 1 && (
          <div className="indexer-toolbar">
            {list.length > FILTER_FROM && <input type="search" className="indexer-filter" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('Filter by name')} aria-label={t('Filter by name')} />}
            <button className="btn-sm" disabled={testingAll} onClick={() => void testAll()}>
              {testingAll ? t('Testing…') : t('Test all')}
            </button>
          </div>
        )}
        {list && list.length > 0 && shown.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>{t('No indexer has that name.')}</p>}
        {shown.length > 0 && (
          <div className="indexer-cards">
            {shown.map((i) => (
              <IndexerCard key={i.id} indexer={i} onChanged={reload} onToggle={() => void toggle(i)} onRemove={() => void remove(i)} />
            ))}
          </div>
        )}
      </fieldset>
      <ProwlarrLinkCard onSynced={reload} />
      <fieldset className="group torrent flare-card">
        <legend>
          <Icon name="shield" size={14} /> <TipLabel text={t('Some sites show a "checking your browser" page that only a real browser passes. FlareSolverr opens it for Mediarium and passes the cookies back.')} learnMore="indexers#sites-behind-a-cloudflare-check">{t('Cloudflare sites (optional)')}</TipLabel>
        </legend>
        {flareState?.configured && (
          <div className={`flare-status ${flareState.running ? 'ok' : 'down'}`} role="status">
            <span className="flare-dot" aria-hidden="true" />
            <strong>{flareState.running ? (bundled && !flareSaved ? t('Built in and running') : t('Connected and running')) : t('Not answering right now')}</strong>
            {flareState.running && flareState.version && <small>{t('version {version}', { version: flareState.version })}</small>}
          </div>
        )}
        {bundled ? (
          <p style={{ marginTop: 0 }}>
            {t('A Cloudflare helper is built in, so sites with a "checking your browser" page just work. Use the box below only if you\'d rather run your own.')}
          </p>
        ) : (
          <>
            <p style={{ marginTop: 0 }}>
              {tx('Some sites show a "checking your browser" page that only a real browser can pass. Run the free {name} helper next to Mediarium and enter its address here. The {full} version of Mediarium has it built in.', {
                name: <strong>FlareSolverr</strong>,
                full: <code>-full</code>,
              })}
            </p>
            <details className="how-details" style={{ marginBottom: 12 }}>
              <summary>{t('How to add FlareSolverr to Docker')}</summary>
              <pre className="code-block">{`  flaresolverr:
    image: ghcr.io/flaresolverr/flaresolverr:latest
    container_name: flaresolverr
    restart: unless-stopped
    environment:
      - TZ=Etc/UTC`}</pre>
              <p>
                {tx('Add this under {services} in your compose file, run {command}, then enter {address} below.', {
                  services: <code>services:</code>,
                  command: <code>docker compose up -d</code>,
                  address: <code>http://flaresolverr:8191</code>,
                })}
              </p>
            </details>
          </>
        )}
        <label className="flare-field">
          <TipLabel text={t('Sites with a Cloudflare check are fetched through this helper. Leave it empty to turn the helper off, or to use the built-in one if you have it.')}>{bundled ? t('Use my own helper instead (optional)') : t('FlareSolverr address')}</TipLabel>
          <input aria-label={bundled ? t('Use my own helper instead (optional)') : t('FlareSolverr address')} value={flare} onChange={(e) => setFlare(e.target.value)} placeholder="http://flaresolverr:8191" {...v.bind('flare', flare, setFlare)} />
          <FieldError v={v} name="flare" />
        </label>
        <button className="primary" onClick={() => void saveFlare()} disabled={flare.trim() === flareSaved}>
          {t('Save')}
        </button>
        <FormProblem v={v} />
      </fieldset>
      </div>
      <IrcAnnounceCard />
    </div>
  )
}
