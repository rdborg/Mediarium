import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type QualityProfile, type TitleCopies } from '../api'
import { formatBytes } from '../format'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import { useToast } from './Toast'
import { plural, t, tx, st } from '../i18n'

// Second copies of a movie or show: the same title kept again in another
// quality (a 4K copy next to the 1080p one), each downloaded into the copies
// folder. Administrators add and remove them; everyone sees them.
export default function CopiesCard({ kind, id, admin }: { kind: 'movie' | 'series'; id: number; admin: boolean }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [data, setData] = useState<TitleCopies | null>(null)
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [pick, setPick] = useState(0)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .titleCopies(kind, id)
      .then(setData)
      .catch(() => undefined)
  }, [kind, id])
  useEffect(load, [load])
  useEffect(() => {
    if (admin) {
      api
        .listProfiles()
        .then((p) => setProfiles(p.profiles))
        .catch(() => undefined)
    }
  }, [admin])

  if (!data || (!admin && data.items.length === 0)) return null
  const taken = new Set([data.mainProfileId, ...data.items.map((c) => c.profileId)])
  const choices = profiles.filter((p) => !taken.has(p.id))
  // Suggest the likeliest second quality first: 4K, then 1080p, then 720p.
  const likely = [/4k|2160/i, /1080/i, /720/i].map((re) => choices.find((p) => re.test(p.name))).find(Boolean)
  const chosen = choices.some((p) => p.id === pick) ? pick : (likely ?? choices[0])?.id ?? 0
  const isMovie = kind === 'movie'

  async function add() {
    if (!chosen) return
    setBusy(true)
    try {
      const c = await api.addTitleCopy(kind, id, chosen)
      toast.success(t('Added a second copy in {profile}. Mediarium is looking for it now.', { profile: c.profileName }))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function searchNow(copyId: number) {
    try {
      const r = kind === 'movie' ? await api.searchNowMovie(copyId) : await api.searchNowSeries(copyId)
      toast.info(st(r.message))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove(copyId: number, name: string) {
    const ok = await confirm({
      title: t('Remove the {name} copy?', { name }),
      body: <p>{isMovie ? t("Its file is deleted from the copies folder. The main movie isn't touched.") : t("Its files are deleted from the copies folder. The main show isn't touched.")}</p>,
      confirmLabel: t('Remove the copy'),
      danger: true,
    })
    if (!ok) return
    try {
      if (kind === 'movie') await api.deleteMovie(copyId, true)
      else await api.deleteSeries(copyId, true)
      toast.success(t('Removed the {name} copy.', { name }))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section className="card" style={{ marginBottom: 24 }}>
      <h2 style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <Icon name="disc" size={18} /> {t('Second copies')}
      </h2>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {isMovie
          ? tx('Keep this movie again in another quality, such as 4K next to 1080p. A second copy is looked for and upgraded on its own and goes in the copies folder{folder}.', {
              folder: data.folder ? <> (<code>{data.folder}</code>)</> : null,
            })
          : tx('Keep this show again in another quality, such as 4K next to 1080p. A second copy is looked for and upgraded on its own and goes in the copies folder{folder}.', {
              folder: data.folder ? <> (<code>{data.folder}</code>)</> : null,
            })}
      </p>
      {data.items.length > 0 && (
        <ul className="file-list" style={{ marginBottom: 12 }}>
          {data.items.map((c) => (
            <li key={c.id}>
              <span className="file-ico">
                <Icon name={kind === 'movie' ? 'film' : 'tv'} size={16} />
              </span>
              <div className="file-name">
                <span>{c.profileName}</span>
                <small>
                  {kind === 'movie'
                    ? c.status === 'downloaded'
                      ? [t('Downloaded'), c.quality, c.sizeBytes ? formatBytes(c.sizeBytes) : ''].filter(Boolean).join(' · ')
                      : c.status === 'downloading'
                        ? t('Downloading')
                        : t('Waiting for a release')
                    : plural(c.episodeCount ?? 0, '{done} of {n} episode', '{done} of {n} episodes', { done: c.downloadedCount ?? 0 })}
                </small>
              </div>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {kind === 'series' && (
                  <Link className="btn-sm" to={`/series/${c.id}`}>
                    {t('Open')}
                  </Link>
                )}
                {admin && (kind === 'series' || c.status !== 'downloaded') && (
                  <button className="btn-sm" onClick={() => void searchNow(c.id)}>
                    {t('Search now')}
                  </button>
                )}
                {admin && (
                  <button className="btn-sm danger-ghost" onClick={() => void remove(c.id, c.profileName)}>
                    {t('Remove')}
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {admin &&
        (!data.folder ? (
          <p className="field-hint">
            {tx('To keep second copies, first choose a folder for them in {link}.', { link: <Link to="/settings/media">{t('Settings > Library and files > Folders')}</Link> })}
          </p>
        ) : choices.length === 0 ? (
          <p className="field-hint">{t("There's a copy in every quality profile you have.")}</p>
        ) : (
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            <select value={chosen} onChange={(e) => setPick(Number(e.target.value))} aria-label={t('Quality of the second copy')}>
              {choices.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <button className="btn-with-icon" disabled={busy || !chosen} onClick={() => void add()}>
              <Icon name="plus" size={15} /> {t('Add a second copy')}
            </button>
          </div>
        ))}
    </section>
  )
}
