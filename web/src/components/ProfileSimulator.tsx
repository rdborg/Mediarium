import { useEffect, useState } from 'react'
import { api, type IndexerConfig, type ProfileSimulation, type QualityProfile } from '../api'
import { st, t } from '../i18n'
import { signed } from '../releaseWhy'
import ReleaseChecks from './ReleaseChecks'

// How a parsed name is listed: a label and its value, left out when empty.
function parsedRows(p: ProfileSimulation['parsed']): { label: string; value: string }[] {
  const rows: { label: string; value: string | undefined }[] = [
    { label: t('Quality'), value: p.quality },
    { label: t('Source'), value: p.source },
    { label: t('Resolution'), value: p.resolution },
    { label: t('Codec'), value: p.codec },
    { label: t('Audio'), value: p.audioCodec },
    { label: t('HDR'), value: p.hdr },
    { label: t('Group'), value: p.group },
    { label: t('Languages'), value: p.languages?.length ? p.languages.join(', ') + (p.multi ? ' (' + t('multi') + ')' : '') : p.multi ? t('multi') : undefined },
    { label: t('Year'), value: p.year ? String(p.year) : undefined },
    { label: t('Season'), value: p.season ? String(p.season) + (p.seasonPack ? ' (' + t('whole season') + ')' : '') : undefined },
    { label: t('Episodes'), value: p.episodes?.length ? p.episodes.join(', ') : undefined },
    { label: t('Edition'), value: p.edition },
    { label: t('Fixed release (PROPER or REPACK)'), value: p.fixed ? t('Yes') : undefined },
  ]
  return rows.filter((r): r is { label: string; value: string } => !!r.value)
}

// Settings > Library > Quality: "Try this profile". A release name is run
// through a quality profile the way a search would run it, so a profile can be
// tested before it is trusted. Nothing is searched or saved.
export default function ProfileSimulator() {
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [indexers, setIndexers] = useState<IndexerConfig[]>([])
  const [profileId, setProfileId] = useState(0)
  const [kind, setKind] = useState<'movie' | 'tv'>('movie')
  const [title, setTitle] = useState('')
  const [sizeGB, setSizeGB] = useState('')
  const [indexer, setIndexer] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<ProfileSimulation | null>(null)

  const loadProfiles = () =>
    api
      .listProfiles()
      .then((r) => {
        setProfiles(r.profiles)
        setProfileId((cur) => (r.profiles.some((p) => p.id === cur) ? cur : r.defaultId || r.profiles[0]?.id || 0))
      })
      .catch(() => undefined)
  useEffect(() => {
    void loadProfiles()
    api.listIndexers().then(setIndexers).catch(() => undefined)
  }, [])

  const sizeValue = sizeGB.trim() === '' ? 0 : Number(sizeGB.replace(',', '.'))
  const sizeBad = Number.isNaN(sizeValue) || sizeValue < 0

  async function run() {
    setBusy(true)
    setError('')
    try {
      setResult(await api.simulateProfile({ title: title.trim(), profileId, kind, sizeMB: Math.round(sizeValue * 1000), indexer }))
    } catch (e) {
      setResult(null)
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const checks = result?.checks ?? []
  return (
    <section className="card span-all profile-sim">
      <h2>{t('Try this profile')}</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('Paste a release name, pick a profile and see how it reads and scores, and whether an automatic search would take it. Nothing is searched or downloaded.')}
      </p>
      <label className="field">
        <span>{t('Release name')}</span>
        <input value={title} maxLength={500} placeholder="Some.Movie.2020.1080p.WEB-DL.x265-GROUP" spellCheck={false} onChange={(e) => setTitle(e.target.value)} />
      </label>
      <div className="profile-sim-row">
        <label className="field">
          <span>{t('Profile')}</span>
          <select value={profileId} onFocus={() => void loadProfiles()} onChange={(e) => setProfileId(Number(e.target.value))}>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>{t('Kind')}</span>
          <select value={kind} onChange={(e) => setKind(e.target.value === 'tv' ? 'tv' : 'movie')}>
            <option value="movie">{t('Movie')}</option>
            <option value="tv">{t('TV')}</option>
          </select>
        </label>
        <label className="field">
          <span>{t('Size in GB (optional)')}</span>
          <input inputMode="decimal" value={sizeGB} aria-invalid={sizeBad ? true : undefined} placeholder="4.5" onChange={(e) => setSizeGB(e.target.value)} />
        </label>
        <label className="field">
          <span>{t('Indexer (optional)')}</span>
          <select value={indexer} onChange={(e) => setIndexer(e.target.value)}>
            <option value="">{t('None')}</option>
            {indexers.map((i) => (
              <option key={i.id} value={i.name}>
                {i.name}
              </option>
            ))}
          </select>
        </label>
      </div>
      <p style={{ marginBottom: 0 }}>
        <button className="primary" disabled={busy || title.trim() === '' || sizeBad || profiles.length === 0} onClick={() => void run()}>
          {busy ? t('Trying…') : t('Try it')}
        </button>
      </p>
      {error && <p style={{ color: 'var(--danger)' }}>{st(error)}</p>}
      {result && (
        <div className="profile-sim-result" aria-live="polite">
          <p className="profile-sim-verdict">
            <span className={`badge ${result.verdict === 'grab' ? 'ok' : 'conflict'}`}>{result.verdict === 'grab' ? t('Would be taken') : t('Would be skipped')}</span>
            <span>{st(result.summary)}</span>
          </p>
          <div className="rel-why">
            <div className="rel-why-col">
              <h4>{t('How the name reads')}</h4>
              <ul className="rel-why-formats">
                {parsedRows(result.parsed).map((r) => (
                  <li key={r.label}>
                    <span>{r.label}</span>
                    <span>{r.value}</span>
                  </li>
                ))}
              </ul>
            </div>
            <div className="rel-why-col">
              <h4>{t('Why this score')}</h4>
              {(result.formats?.length ?? 0) + (result.terms?.length ?? 0) + (result.fixScore ? 1 : 0) > 0 ? (
                <ul className="rel-why-formats">
                  {(result.formats ?? []).map((f) => (
                    <li key={`f${f.name}`}>
                      <span>{f.name}</span>
                      <span className={`format-score-cell${f.score < 0 ? ' neg' : ''}`}>{signed(f.score)}</span>
                    </li>
                  ))}
                  {(result.terms ?? []).map((x) => (
                    <li key={`t${x.term}`}>
                      <span>{x.term}</span>
                      <span className={`format-score-cell${x.score < 0 ? ' neg' : ''}`}>{signed(x.score)}</span>
                    </li>
                  ))}
                  {!!result.fixScore && (
                    <li>
                      <span>{t('Fixed release (PROPER or REPACK)')}</span>
                      <span className="format-score-cell">{signed(result.fixScore)}</span>
                    </li>
                  )}
                </ul>
              ) : (
                <p className="rel-why-none">{t('No custom format matches')}</p>
              )}
              <p className="rel-why-total">
                <span>{t('Total')}</span>
                <span className={`format-score-cell${(result.score ?? 0) < 0 ? ' neg' : ''}`}>{signed(result.score ?? 0)}</span>
              </p>
              {result.score !== undefined && <p className="rel-why-limits">{t('Minimum score: {n}', { n: signed(result.minScore ?? 0) })}</p>}
            </div>
            {checks.length > 0 && (
              <div className="rel-why-col">
                <h4>{t('Why this release?')}</h4>
                <ReleaseChecks checks={checks} />
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
