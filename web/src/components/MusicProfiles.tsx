import { useCallback, useEffect, useState } from 'react'
import { api, type MusicProfile } from '../api'
import { useModules } from '../ModulesContext'
import { useConfirm } from './ConfirmProvider'
import FallbackEditor from './FallbackEditor'
import Icon from './Icon'
import { useToast } from './Toast'
import { firstError, maxLength, required } from '../validate'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { useReveal } from '../useReveal'

interface Draft {
  id?: number
  name: string
  allowed: string[]
  cutoff: string
  upgradeAllowed: boolean
  fallback: number[]
}

const draftFrom = (p: MusicProfile): Draft => ({ id: p.id, name: p.name, allowed: p.allowed, cutoff: p.cutoff, upgradeAllowed: p.upgradeAllowed, fallback: p.fallback ?? [] })
const blankDraft = (tiers: string[]): Draft => ({
  name: '',
  allowed: tiers.filter((t) => t === 'FLAC'),
  cutoff: 'FLAC',
  upgradeAllowed: false,
  fallback: [],
})

const tierLabel = (t: string) => (t === 'Unknown' ? 'Unknown (the release doesn\'t say)' : t)

// One line about what a profile does, for the cards and the fallback list.
function blurb(p: MusicProfile, all: MusicProfile[]): string {
  const cutoff = p.upgradeAllowed ? `It keeps looking for better until it reaches ${p.cutoff}.` : 'It takes what it finds and doesn\'t upgrade.'
  const fallback = (p.fallback ?? []).map((id) => all.find((x) => x.id === id)?.name).filter(Boolean)
  return `${cutoff}${fallback.length > 0 ? ` If nothing fits, it falls back to ${fallback.join(', then ')}.` : ''}`
}

// The music quality profiles: what each one takes, where it stops upgrading,
// what it falls back to, and which is the default for artists that have none
// of their own. Same rules as the movie and show profiles above.
export default function MusicProfilesSection() {
  const on = useModules().on('music')
  const confirm = useConfirm()
  const toast = useToast()
  const [profiles, setProfiles] = useState<MusicProfile[] | null>(null)
  const [tiers, setTiers] = useState<string[]>([])
  const [draft, setDraft] = useState<Draft | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const errors = {
    name: firstError(required(draft?.name, 'Give this profile a name, for example Lossless only.'), maxLength(draft?.name ?? '', 60, 'The name')),
    allowed: draft && draft.allowed.length === 0 ? 'Tick at least one format to accept.' : null,
    cutoff: draft && draft.allowed.length > 0 && !draft.allowed.includes(draft.cutoff) ? 'Choose the format to stop upgrading at.' : null,
  }
  const v = useValidation(errors)
  // Counts the times a form has been opened, so it can be scrolled into view
  // and given the cursor each time (see useReveal).
  const [opened, setOpened] = useState(0)
  const formRef = useReveal<HTMLDivElement>(opened)
  function openDraft(d: Draft | null) {
    v.reset()
    setDraft(d)
    if (d) setOpened((n) => n + 1)
  }

  const load = useCallback(() => {
    api
      .musicProfiles()
      .then(setProfiles)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => {
    if (!on) return
    load()
    api.musicTiers().then(setTiers).catch(() => undefined)
  }, [on, load])

  async function save() {
    if (!draft || !v.attempt()) return
    setSaving(true)
    setError('')
    try {
      const body = { name: draft.name.trim(), allowed: draft.allowed, cutoff: draft.cutoff, upgradeAllowed: draft.upgradeAllowed, fallback: draft.fallback }
      if (draft.id) await api.updateMusicProfile(draft.id, body)
      else await api.createMusicProfile(body)
      toast.success(draft.id ? 'Profile saved.' : 'Profile created.')
      openDraft(null)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  async function remove(p: MusicProfile) {
    if (!(await confirm({ title: `Delete the "${p.name}" profile?`, body: <p>This can't be undone. You can't delete the default profile, or one an artist is using.</p>, confirmLabel: 'Delete profile', danger: true }))) return
    setError('')
    try {
      await api.deleteMusicProfile(p.id)
      toast.success(`${p.name} deleted.`)
      load()
    } catch (e) {
      // For example "it is the default" or "2 artists use it": say so as it is.
      const message = e instanceof Error ? e.message : String(e)
      setError(message)
      toast.error(message)
    }
  }

  async function makeDefault(p: MusicProfile) {
    setError('')
    try {
      await api.putSettings({ musicDefaultProfileId: p.id })
      toast.success(`${p.name} is now the default for new artists.`)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  function toggleTier(t: string) {
    if (!draft) return
    const allowed = draft.allowed.includes(t) ? draft.allowed.filter((x) => x !== t) : [...draft.allowed, t]
    // Keep the cutoff valid: if its tier was just unticked, use the best one left.
    const ordered = tiers.filter((x) => allowed.includes(x))
    const cutoff = ordered.includes(draft.cutoff) ? draft.cutoff : (ordered[ordered.length - 1] ?? '')
    setDraft({ ...draft, allowed, cutoff })
  }

  if (!on) return null
  const orderedAllowed = draft ? tiers.filter((t) => draft.allowed.includes(t)) : []
  return (
    <section className="card span-all music-profiles-card">
      <h2>
        <Icon name="music" size={18} /> Music
      </h2>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        Music qualities, worst to best: {tiers.length > 0 ? tiers.filter((t) => t !== 'Unknown').join(', ') : 'MP3-192 up to FLAC 24bit'}. The highlighted format is where a profile stops upgrading.
      </p>
      {error && <p className="error-text">{error}</p>}
      {profiles === null && !error && <div className="skeleton" style={{ height: 110 }} />}
      {profiles && (
        <div className="music-profiles">
          {profiles.map((p) => (
            <article key={p.id} className={`music-profile${p.default ? ' is-default' : ''}`}>
              <header>
                <strong>{p.name}</strong>
                {p.default && (
                  <span className="badge downloaded" title="Artists without a profile of their own follow this one">
                    Default
                  </span>
                )}
                {p.inUse > 0 && <small className="music-profile-use">used by {p.inUse} {p.inUse === 1 ? 'artist' : 'artists'}</small>}
              </header>
              <div className="music-profile-tiers" aria-label="Formats this profile takes">
                {[...p.allowed].sort((a, b) => tiers.indexOf(a) - tiers.indexOf(b)).map((t) => (
                  <span key={t} className={`chip-tier${t === p.cutoff ? ' cutoff' : ''}`} title={t === p.cutoff ? 'Stops upgrading here' : undefined}>
                    {t}
                  </span>
                ))}
              </div>
              <p>{blurb(p, profiles)}</p>
              <div className="music-profile-actions">
                <button className="btn-sm" onClick={() => openDraft(draftFrom(p))}>
                  Edit
                </button>
                {!p.default && (
                  <button className="btn-sm" onClick={() => void makeDefault(p)}>
                    Make default
                  </button>
                )}
                {!p.default && (
                  <button className="btn-sm btn-danger" onClick={() => void remove(p)}>
                    Delete
                  </button>
                )}
              </div>
            </article>
          ))}
        </div>
      )}

      {draft ? (
        <div className="grid-form music-profile-form" ref={formRef}>
          <h3 style={{ margin: 0 }}>{draft.id ? 'Edit profile' : 'New profile'}</h3>
          <label>
            Name
            <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} {...v.bind('name', draft.name, (name) => setDraft({ ...draft, name }))} />
            <FieldError v={v} name="name" />
          </label>
          <fieldset style={{ border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: 12 }}>
            <legend>Accepted qualities</legend>
            <div className="profile-tiers">
              {tiers.map((t) => (
                <label key={t}>
                  <input type="checkbox" checked={draft.allowed.includes(t)} onChange={() => toggleTier(t)} />
                  {tierLabel(t)}
                </label>
              ))}
            </div>
            <FieldError v={v} name="allowed" />
          </fieldset>
          <label>
            Stop upgrading once this quality is reached
            <select value={draft.cutoff} onChange={(e) => setDraft({ ...draft, cutoff: e.target.value })} {...v.bind('cutoff')}>
              {orderedAllowed.map((t) => (
                <option key={t} value={t}>
                  {tierLabel(t)}
                </option>
              ))}
            </select>
            <FieldError v={v} name="cutoff" />
          </label>
          <label className="check-hint">
            <input type="checkbox" checked={draft.upgradeAllowed} onChange={(e) => setDraft({ ...draft, upgradeAllowed: e.target.checked })} />
            <span>
              Keep looking for better versions after it is downloaded
              <small>Swaps a download for a better one until it reaches the quality above.</small>
            </span>
          </label>
          <fieldset style={{ border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: 12 }}>
            <legend>If nothing is found at this quality, also try</legend>
            <FallbackEditor profiles={profiles ?? []} selfId={draft.id} value={draft.fallback} onChange={(fallback) => setDraft({ ...draft, fallback })} describe={(p) => blurb(p, profiles ?? [])} />
          </fieldset>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="primary" onClick={() => void save()} disabled={saving}>
              {saving ? 'Saving…' : 'Save profile'}
            </button>
            <button onClick={() => openDraft(null)}>Cancel</button>
          </div>
          <FormProblem v={v} verb="save this profile" />
        </div>
      ) : (
        <div style={{ marginTop: 14 }}>
          <button className="btn-with-icon" onClick={() => openDraft(blankDraft(tiers))} disabled={profiles === null}>
            <Icon name="plus" size={15} /> New profile…
          </button>
        </div>
      )}
    </section>
  )
}
