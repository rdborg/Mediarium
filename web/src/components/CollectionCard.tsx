import { useCallback, useEffect, useState } from 'react'
import { api, type CollectionInfo, type CollectionRef, type QualityProfile, type SourcePref } from '../api'
import { plural, st, t } from '../i18n'
import type { NewTitleFolder } from '../api'
import { laterChoices } from '../newTitleFolder'
import Icon from './Icon'
import PosterCard from './PosterCard'
import { sortProfiles } from './qualityBlurb'
import { describeState } from './state'
import Switch from './Switch'
import { useToast } from './Toast'
import { addStart } from '../addDefaults'

// On a movie's page: the collection (series of films) it belongs to, the films
// in it and which are in the library, and, for administrators, "Add the whole
// collection" and "also add new films in this collection".
export default function CollectionCard({ collection, admin, onChanged }: { collection: CollectionRef; admin: boolean; onChanged: () => void }) {
  const toast = useToast()
  const [info, setInfo] = useState<CollectionInfo | null>(null)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)

  const load = useCallback(() => {
    api
      .collection(collection.id)
      .then((c) => {
        setInfo(c)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [collection.id])
  useEffect(load, [load])

  if (!info) {
    return error ? (
      <section className="card" style={{ marginBottom: 24 }}>
        <h2>{collection.name}</h2>
        <p className="error-text">{error}</p>
      </section>
    ) : null
  }

  const missing = info.parts.filter((p) => !p.libraryId)
  return (
    <section className="card collection-card" style={{ marginBottom: 24 }}>
      <div className="rail-head">
        <h2>{info.name}</h2>
        <span>{t('{n} of {total} in your library', { n: info.inLibrary, total: info.parts.length })}</span>
      </div>
      {info.overview && <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>{info.overview}</p>}
      <div className="poster-grid" style={{ ['--poster-min' as string]: '120px', minHeight: 0 }}>
        {info.parts.map((p) => (
          <PosterCard
            key={p.tmdbId}
            to={`/title/${p.tmdbId}`}
            poster={p.posterUrl}
            title={p.title}
            meta={p.year || (p.released ? undefined : t('Not out yet'))}
            kind="movie"
            dim={!p.libraryId}
            state={p.libraryId ? describeState(p.status === 'downloaded' ? 'downloaded' : p.status === 'downloading' ? 'downloading' : 'searching') : undefined}
          />
        ))}
      </div>
      {admin && (
        <div style={{ marginTop: 16, display: 'grid', gap: 12 }}>
          {missing.length > 0 && !open && (
            <div>
              <button className="primary btn-with-icon" onClick={() => setOpen(true)}>
                <Icon name="plus" size={16} /> {t('Add the whole collection')}
              </button>
              <span className="field-hint" style={{ marginLeft: 12 }}>
                {plural(missing.length, 'One film is not in your library.', '{n} films are not in your library.', { n: missing.length })}
              </span>
            </div>
          )}
          {open && (
            <AddAll
              info={info}
              missing={missing.length}
              onClose={() => setOpen(false)}
              onDone={(message) => {
                toast.success(message)
                setOpen(false)
                load()
                onChanged()
              }}
            />
          )}
          {!open && <FollowSwitch info={info} onChanged={setInfo} />}
        </div>
      )}
    </section>
  )
}

function FollowSwitch({ info, onChanged }: { info: CollectionInfo; onChanged: (c: CollectionInfo) => void }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  async function change(follow: boolean) {
    setBusy(true)
    try {
      const c = await api.followCollection(info.id, { follow, profileId: info.followProfileId || undefined, rootPath: info.followRootPath || undefined, monitored: info.followMonitored })
      onChanged(c)
      toast.success(follow ? t('New films in this collection will be added by themselves.') : t('New films in this collection are left alone.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Switch
      checked={info.follow}
      onChange={(v) => void change(v)}
      disabled={busy}
      label={t('Also add new films in this collection')}
      description={
        info.follow
          ? t('When a new film joins it, Mediarium adds it with the quality profile and folder chosen when you turned this on.')
          : t('Films that join the collection later are added to your library. The ones it has now are not added by this.')
      }
    />
  )
}

function AddAll({ info, missing, onClose, onDone }: { info: CollectionInfo; missing: number; onClose: () => void; onDone: (message: string) => void }) {
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultProfileId, setDefaultProfileId] = useState(0)
  const [folders, setFolders] = useState<string[]>([])
  const [newNow, setNewNow] = useState<NewTitleFolder | undefined>()
  const [profileId, setProfileId] = useState(info.followProfileId ?? 0)
  const [rootPath, setRootPath] = useState(info.followRootPath ?? '')
  const [monitored, setMonitored] = useState(true)
  const [searchNow, setSearchNow] = useState(true)
  const [follow, setFollow] = useState(info.follow)
  const [sources] = useState<SourcePref>('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .listProfiles()
      .then((r) => {
        setProfiles(sortProfiles(r.profiles))
        setDefaultProfileId(r.defaultId)
        const start = addStart(r)
        setMonitored(start.monitored)
        setSearchNow(start.searchNow)
      })
      .catch(() => undefined)
    api
      .libraryFolders()
      .then((f) => {
        setFolders(f.movies)
        setNewNow(f.newTitles.movies)
      })
      .catch(() => undefined)
  }, [])

  const defaultProfile = profiles.find((p) => p.id === defaultProfileId)

  async function submit() {
    setBusy(true)
    setError('')
    try {
      const res = await api.addCollection(info.id, { profileId: profileId || undefined, rootPath: rootPath || undefined, monitored, sources, searchNow: searchNow && monitored, follow })
      onDone(st(res.message))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="notice" style={{ display: 'grid', gap: 12 }}>
      <strong>{plural(missing, 'Add the one film that is missing', 'Add the {n} films that are missing', { n: missing })}</strong>
      <div className="grid-form">
        <label>
          {t('Quality profile')}
          <select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))} disabled={busy}>
            <option value={0}>{defaultProfile ? t('Default ({name})', { name: defaultProfile.name }) : t('Default')}</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        {folders.length > 1 && (
          <label>
            {t('Keep them in')}
            <select value={rootPath} onChange={(e) => setRootPath(e.target.value)} disabled={busy}>
              {laterChoices(folders, newNow).map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
        )}
        <label>
          {t('Monitoring')}
          <select value={monitored ? 'yes' : 'no'} onChange={(e) => setMonitored(e.target.value === 'yes')} disabled={busy}>
            <option value="yes">{t('Monitored: search for them and download them')}</option>
            <option value="no">{t('Not monitored: just add them to the library')}</option>
          </select>
        </label>
      </div>
      <Switch checked={searchNow && monitored} onChange={setSearchNow} disabled={busy || !monitored} label={t('Start searching right away')} description={t('Films that are not out yet wait until they are.')} />
      <Switch checked={follow} onChange={setFollow} disabled={busy} label={t('Also add new films in this collection')} description={t('Films that join the collection later are added with these choices.')} />
      {error && <p className="error-text">{error}</p>}
      <div style={{ display: 'flex', gap: 8 }}>
        <button className="primary" onClick={() => void submit()} disabled={busy}>
          {busy ? t('Adding…') : plural(missing, 'Add the film', 'Add {n} films', { n: missing })}
        </button>
        <button onClick={onClose} disabled={busy}>
          {t('Cancel')}
        </button>
      </div>
    </div>
  )
}
