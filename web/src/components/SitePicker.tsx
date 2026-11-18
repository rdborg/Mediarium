import { useEffect, useMemo, useRef, useState } from 'react'
import { api, ApiError, type IndexerConfig, type IndexerDefinition } from '../api'
import Icon from './Icon'
import { useConfirm } from './ConfirmProvider'
import { useToast } from './Toast'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { apiKey as apiKeyCheck, firstError, maxLength, required, url as urlCheck } from '../validate'
import { locale, plural, t, tx } from '../i18n'

type Filter = 'all' | 'public' | 'semi-private' | 'private'

// Pick a site from the community list of search definitions (the same ones
// Prowlarr and Jackett use), fill in what that site asks for, and add it.
// The list is downloaded only when this opens; nothing is pre-selected.
export default function SitePicker({ protocol, onAdded }: { protocol: 'torrent' | 'usenet'; onAdded: (i: IndexerConfig) => void }) {
  const toast = useToast()
  const [defs, setDefs] = useState<IndexerDefinition[] | null>(null)
  const [meta, setMeta] = useState<{ updatedAt?: string; source?: string; licence?: string }>({})
  const [error, setError] = useState('')
  const [missing, setMissing] = useState(false)
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [picked, setPicked] = useState<IndexerDefinition | null>(null)
  const [values, setValues] = useState<Record<string, string>>({})
  const [baseUrl, setBaseUrl] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [uploading, setUploading] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const confirm = useConfirm()

  // Sites decide for themselves what they ask for. Keys and tokens must be one unbroken string.
  const keyLike = (n: string) => /api.?key|pass.?key|rss.?key|token|secret/i.test(n)
  const errs: Record<string, string | null> = { name: maxLength(name, 80, t('The name')) }
  // A site that runs on the person's own network takes the address of their own server.
  const ownAddress = !!picked?.local && !picked.custom
  if (ownAddress) {
    const example = 'http://192.168.1.10:3333'
    errs.baseUrl = firstError(required(baseUrl, t('Add the address of the indexer, for example {example}.', { example })), urlCheck(baseUrl, { requireScheme: true, example }), maxLength(baseUrl, 200, t('The address')))
  }
  for (const s of picked?.settings ?? []) {
    if (s.type.startsWith('info') || s.type === 'checkbox' || s.type === 'select') continue
    const val = values[s.name] ?? ''
    errs[`s.${s.name}`] = firstError(maxLength(val, 2000, s.label || s.name), keyLike(s.name) ? apiKeyCheck(val) : null)
  }
  const v = useValidation(errs)

  const load = (refresh = false) => {
    setError('')
    if (refresh) setRefreshing(true)
    api
      .indexerDefinitions(refresh)
      .then((r) => {
        setDefs(r.definitions)
        setMeta({ updatedAt: r.updatedAt, source: r.source, licence: r.licence })
      })
      .catch((e) => {
        if (e instanceof ApiError && e.status === 404) setMissing(true)
        else setError(e instanceof Error ? e.message : String(e))
      })
      .finally(() => setRefreshing(false))
  }
  useEffect(() => load(false), [])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (defs ?? [])
      // Files people add show whatever they are for: there's no Usenet list.
      .filter((d) => d.protocol === protocol || d.custom)
      .filter((d) => filter === 'all' || d.type === filter)
      .filter((d) => !needle || d.name.toLowerCase().includes(needle) || (d.description ?? '').toLowerCase().includes(needle))
      .sort((a, b) => a.name.localeCompare(b.name))
  }, [defs, q, filter, protocol])

  // A definition file of the person's own, such as the one a self-hosted
  // indexer ships for Prowlarr. It's checked on the server before it's kept.
  async function addFile(file: File) {
    if (file.size > 1024 * 1024) {
      toast.error(t('That file is too large. Definition files are well under 1 MB.'))
      return
    }
    setUploading(true)
    try {
      const added = await api.addIndexerDefinition(await file.text())
      toast.success(t('{name} was added to the list.', { name: added.name }))
      load(false)
      pick(added)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setUploading(false)
    }
  }

  async function removeFile(d: IndexerDefinition) {
    const ok = await confirm({
      title: t('Remove {name}?', { name: d.name }),
      body: t('The definition file you added is deleted from this server. You can add it again at any time.'),
      confirmLabel: t('Remove'),
      danger: true,
    })
    if (!ok) return
    try {
      await api.removeIndexerDefinition(d.id)
      toast.success(t('{name} was removed.', { name: d.name }))
      load(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  function pick(d: IndexerDefinition) {
    v.reset()
    setPicked(d)
    setName(d.name)
    setBaseUrl(d.links?.[0] ?? '')
    const init: Record<string, string> = {}
    for (const s of d.settings ?? []) {
      if (s.default !== undefined && s.default !== null) init[s.name] = String(s.default)
    }
    setValues(init)
  }

  async function add() {
    if (!picked) return
    if (!v.attempt()) return
    setBusy(true)
    try {
      const created = await api.createIndexer({ name: name.trim() || picked.name, definitionId: picked.id, baseUrl, apiKey: '', protocol, settings: values })
      toast.success(t('{name} added. Press Test to check it works.', { name: created.name }))
      setPicked(null)
      onAdded(created)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (missing) {
    return (
      <div className="notice notice-info">
        {tx("The list of sites isn't available from this server. Use {paste} instead, or add torrents later in Settings > Finding releases > Indexers & Search.", { paste: <strong>{t('Paste a Torznab link')}</strong> })}
      </div>
    )
  }
  if (error) {
    return (
      <div className="notice notice-warn">
        {t("Couldn't load the site list ({error}).", { error })}{' '}
        <button className="btn-sm" onClick={() => load(true)}>
          {t('Try again')}
        </button>
      </div>
    )
  }
  if (!defs) return <div className="skeleton" style={{ height: 180 }} />

  if (picked) {
    const fields = (picked.settings ?? []).filter((s) => !s.type.startsWith('info'))
    const infos = (picked.settings ?? []).filter((s) => s.type.startsWith('info') && s.label)
    return (
      <div className="site-detail grid-form">
        <button className="btn-back btn-sm" onClick={() => setPicked(null)} style={{ justifySelf: 'start' }}>
          {t('← Back to the list')}
        </button>
        <div>
          <strong style={{ fontSize: '1.05rem' }}>{picked.name}</strong> <span className={`badge site-${picked.type}`}>{typeLabel(picked.type)}</span>
          {picked.language && <span className="badge">{picked.language}</span>}
          {picked.description && <p style={{ margin: '6px 0 0', color: 'var(--text-dim)' }}>{picked.description}</p>}
        </div>
        {infos.map((s) => (
          <div key={s.name} className="notice notice-info">
            {s.label}
          </div>
        ))}
        <div className="form-cols">
          <label>
            {t('Name in Mediarium')}
            <input value={name} onChange={(e) => setName(e.target.value)} {...v.bind('name', name, setName)} />
            <FieldError v={v} name="name" />
          </label>
          {picked.custom ? (
            <label>
              {t('Site address')}
              <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} list="site-links" spellCheck={false} placeholder="http://192.168.1.20:8080/" />
              <datalist id="site-links">
                {(picked.links ?? []).map((l) => (
                  <option key={l} value={l} />
                ))}
              </datalist>
              <small style={{ color: 'var(--text-dim)', fontWeight: 400 }}>{t('Change it if the site runs somewhere else, such as on another computer at home.')}</small>
            </label>
          ) : ownAddress ? (
            <label>
              {t('Address')}
              <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} spellCheck={false} placeholder="http://192.168.1.10:3333" {...v.bind('baseUrl', baseUrl, setBaseUrl)} />
              <FieldError v={v} name="baseUrl" />
              <small style={{ color: 'var(--text-dim)', fontWeight: 400 }}>{t('The address of your own server, for example http://192.168.1.10:3333. Inside Docker, 127.0.0.1 is the Mediarium container itself.')}</small>
            </label>
          ) : picked.links && picked.links.length > 1 && (
            <label>
              {t('Site address')}
              <select value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)}>
                {picked.links.map((l) => (
                  <option key={l} value={l}>
                    {l}
                  </option>
                ))}
              </select>
            </label>
          )}
          {fields.map((s) =>
            s.type === 'checkbox' ? (
              <label key={s.name} className="check-row">
                <input type="checkbox" checked={values[s.name] === 'true'} onChange={(e) => setValues({ ...values, [s.name]: e.target.checked ? 'true' : 'false' })} />
                {s.label || s.name}
              </label>
            ) : s.type === 'select' ? (
              <label key={s.name}>
                {s.label || s.name}
                <select value={values[s.name] ?? ''} onChange={(e) => setValues({ ...values, [s.name]: e.target.value })}>
                  {(s.options ?? []).map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <label key={s.name}>
                {s.label || s.name}
                <input
                  type={s.type === 'password' ? 'password' : 'text'}
                  value={values[s.name] ?? ''}
                  onChange={(e) => setValues({ ...values, [s.name]: e.target.value })}
                  autoComplete="off"
                  spellCheck={false}
                  {...v.bind(`s.${s.name}`, s.type === 'password' ? undefined : values[s.name], (t) => setValues((cur) => ({ ...cur, [s.name]: t })))}
                />
                <FieldError v={v} name={`s.${s.name}`} />
              </label>
            ),
          )}
        </div>
        {fields.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>{t('This site needs nothing else. Press Add.')}</p>}
        <div>
          <button className="primary btn-with-icon" onClick={() => void add()} disabled={busy}>
            <Icon name="plus" size={15} /> {t('Add {name}', { name: picked.name })}
          </button>
          <FormProblem v={v} action="add" />
        </div>
      </div>
    )
  }

  const mine = (defs ?? []).filter((d) => d.custom)
  const counts = (f: Filter) => (defs ?? []).filter((d) => (d.protocol === protocol || d.custom) && (f === 'all' || d.type === f)).length
  return (
    <div className="site-picker grid-form">
      <div className="toolbar" style={{ margin: 0 }}>
        <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Search sites')} aria-label={t('Search sites')} style={{ flex: '1 1 220px', maxWidth: 'none' }} />
        <div className="chip-row">
          {(['all', 'public', 'semi-private', 'private'] as Filter[]).map((f) => (
            <button key={f} className={`chip${filter === f ? ' active' : ''}`} onClick={() => setFilter(f)}>
              {f === 'all' ? t('All') : typeLabel(f)} <small>{counts(f)}</small>
            </button>
          ))}
        </div>
      </div>
      {mine.length > 0 && (
        <div className="site-mine">
          <strong>{t('Definition files you added')}</strong>
          {mine.map((d) => (
            <span key={d.id} className="site-mine-row">
              <Icon name="list" size={14} /> {d.name} <small>{d.protocol === 'usenet' ? t('Usenet') : t('Torrents')}</small>
              {d.supported === false && <small className="error-text">{d.problem}</small>}
              <button className="icon-btn" onClick={() => void removeFile(d)} aria-label={t('Remove {name}', { name: d.name })} title={t('Remove {name}', { name: d.name })}>
                <Icon name="trash" size={14} />
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="site-list" role="list">
        {shown.length === 0 && <p style={{ color: 'var(--text-dim)', margin: 8 }}>{t('No sites match.')}</p>}
        {shown.map((d) => (
          <button
            key={d.id}
            role="listitem"
            className={`site-row${d.supported === false ? ' unsupported' : ''}`}
            onClick={() => d.supported !== false && pick(d)}
            disabled={d.supported === false}
            title={d.supported === false ? d.problem || t("This site can't be used yet.") : undefined}
          >
            <span className="site-name">{d.name}</span>
            {d.custom && <span className="badge">{d.protocol === 'usenet' ? t('Added by you, Usenet') : t('Added by you')}</span>}
            <span className={`badge site-${d.type}`}>{typeLabel(d.type)}</span>
            {d.language && <span className="site-lang">{d.language}</span>}
            <span className="site-desc">{d.supported === false ? t('Not supported yet: {reason}', { reason: d.problem || t('unknown reason') }) : d.description}</span>
          </button>
        ))}
      </div>
      <p className="site-source">
        {plural(shown.length, '{n} site.', '{n} sites.')}{' '}
        {tx('List from the community {defs}', {
          defs: meta.source ? (
            <a href={meta.source} target="_blank" rel="noreferrer">
              {t('site definitions')}
            </a>
          ) : (
            t('site definitions')
          ),
        })}
        {meta.licence ? ` (${meta.licence})` : ''}
        {meta.updatedAt ? ', ' + t('updated {date}', { date: new Date(meta.updatedAt).toLocaleDateString(locale()) }) : ''}.{' '}
        <button className="btn-sm" onClick={() => load(true)} disabled={refreshing}>
          {refreshing ? t('Updating…') : t('Update list')}
        </button>{' '}
        {t('You choose which sites to use and are responsible for following the law where you live.')}
      </p>
      <p className="site-source">
        <button className="btn-sm btn-with-icon" onClick={() => fileInput.current?.click()} disabled={uploading}>
          <Icon name="plus" size={14} /> {uploading ? t('Adding…') : t('Add a definition file')}
        </button>{' '}
        {t("For a site that isn't in the list, such as an indexer you run yourself that comes with a definition file (.yml) for Prowlarr or Jackett.")}
        <input
          ref={fileInput}
          type="file"
          accept=".yml,.yaml,text/yaml,application/x-yaml"
          hidden
          onChange={(e) => {
            const file = e.target.files?.[0]
            e.target.value = ''
            if (file) void addFile(file)
          }}
        />
      </p>
    </div>
  )
}

function typeLabel(type: string): string {
  return type === 'public' ? t('Public') : type === 'private' ? t('Private') : type === 'semi-private' ? t('Semi-private') : type
}
