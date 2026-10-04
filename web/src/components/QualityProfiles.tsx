import { Fragment, useCallback, useEffect, useState } from 'react'
import FallbackEditor from './FallbackEditor'
import Icon from './Icon'
import { useToast } from './Toast'
import { profileBlurb, sortProfiles } from './qualityBlurb'
import { api, type PreferredTerm, type QualityProfile } from '../api'
import { useConfirm } from './ConfirmProvider'
import { firstError, maxLength, required } from '../validate'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { useReveal } from '../useReveal'

interface Draft {
  id?: number
  name: string
  allowed: string[]
  cutoff: string
  upgradeAllowed: boolean
  // One term per line; preferred lines are "term = score".
  mustContain: string
  mustNotContain: string
  preferred: string
  fallback: number[]
  // The largest release, in GB, as typed ("" = no limit).
  maxSize: string
}

const linesOf = (text: string) => text.split('\n').map((l) => l.trim()).filter(Boolean)

function parsePreferred(text: string): PreferredTerm[] {
  return linesOf(text).map((line) => {
    const idx = line.lastIndexOf('=')
    if (idx < 0) return { term: line, score: 10 }
    const score = Number(line.slice(idx + 1).trim())
    return { term: line.slice(0, idx).trim(), score: Number.isFinite(score) ? score : 0 }
  })
}

// Limits the server applies (internal/quality/repo.go).
const MAX_TERMS = 50
const MAX_TERM_LEN = 60
const MAX_SCORE = 10000

// One term per line, no more than 50, each short and free of the | character.
function termLines(text: string, what: string): string | null {
  const lines = linesOf(text)
  if (lines.length > MAX_TERMS) return `Use at most ${MAX_TERMS} lines here. You have ${lines.length}.`
  const long = lines.find((l) => l.length > MAX_TERM_LEN)
  if (long) return `Each ${what} line can be at most ${MAX_TERM_LEN} characters. "${long.slice(0, 24)}..." is too long.`
  if (lines.some((l) => l.includes('|'))) return "Terms can't contain the | character. Put each term on its own line instead."
  return null
}

// Preferred lines look like "x265 = 50". A line with no = counts as a score of 10.
function preferredLines(text: string): string | null {
  const lines = linesOf(text)
  const tooMany = termLines(text, 'preferred term')
  if (tooMany) return tooMany
  for (const line of lines) {
    const idx = line.lastIndexOf('=')
    if (idx < 0) continue
    const term = line.slice(0, idx).trim()
    const score = line.slice(idx + 1).trim()
    if (!term) return `"${line}" has a score but no term. Write it like x265 = 50.`
    if (!/^[+-]?\d+$/.test(score) || Math.abs(Number(score)) > MAX_SCORE) return `The score in "${line}" must be a whole number between -${MAX_SCORE.toLocaleString('en-US')} and ${MAX_SCORE.toLocaleString('en-US')}, like x265 = 50.`
  }
  return null
}

const draftFrom = (p: QualityProfile): Draft => ({
  id: p.id,
  name: p.name,
  allowed: p.allowed,
  cutoff: p.cutoff,
  upgradeAllowed: p.upgradeAllowed,
  mustContain: p.mustContain.join('\n'),
  mustNotContain: p.mustNotContain.join('\n'),
  preferred: p.preferred.map((x) => `${x.term} = ${x.score}`).join('\n'),
  fallback: p.fallback ?? [],
  maxSize: p.maxSizeGB ? String(p.maxSizeGB) : '',
})

const blankDraft = (tiers: string[]): Draft => ({
  name: '',
  allowed: tiers.filter((t) => t === 'WEBDL-1080p' || t === 'Bluray-1080p'),
  cutoff: 'Bluray-1080p',
  upgradeAllowed: false,
  mustContain: '',
  mustNotContain: '',
  preferred: '',
  fallback: [],
  maxSize: '',
})

// "1080p copy", or "1080p copy 2" when that name is taken.
function copyName(name: string, profiles: QualityProfile[]): string {
  const taken = new Set(profiles.map((p) => p.name.toLowerCase()))
  let n = `${name} copy`.slice(0, 60)
  for (let i = 2; taken.has(n.toLowerCase()); i++) n = `${name} copy ${i}`.slice(0, 60)
  return n
}

const tierLabel = (t: string) => (t === 'Unknown' ? 'Unknown (untagged releases)' : t)

// Editable quality profiles, Radarr/Sonarr style: which qualities are
// acceptable, the cutoff at which upgrading stops, and whether upgrades
// happen at all (off unless switched on). The default profile applies to
// anything without its own.
export default function QualityProfilesSection() {
  const confirm = useConfirm()
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [tiers, setTiers] = useState<string[]>([])
  const [defaultId, setDefaultId] = useState(0)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const orderedFor = (d: Draft) => tiers.filter((t) => d.allowed.includes(t))
  const errors = {
    name: firstError(required(draft?.name, 'Give this profile a name, for example Movies 1080p.'), maxLength(draft?.name ?? '', 60, 'The name')),
    allowed: draft && draft.allowed.length === 0 ? 'Tick at least one quality to accept.' : null,
    cutoff: draft && draft.allowed.length > 0 && !orderedFor(draft).includes(draft.cutoff) ? 'Choose the quality to stop upgrading at.' : null,
    mustContain: termLines(draft?.mustContain ?? '', 'required term'),
    mustNotContain: termLines(draft?.mustNotContain ?? '', 'excluded term'),
    preferred: preferredLines(draft?.preferred ?? ''),
    maxSize: draft && draft.maxSize.trim() !== '' && !(Number(draft.maxSize) > 0 && Number(draft.maxSize) <= 1000) ? 'Write a size from 0.1 to 1000 GB, or leave it empty for no limit.' : null,
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
      .listProfiles()
      .then((r) => {
        setProfiles(sortProfiles(r.profiles))
        setTiers(r.tiers)
        setDefaultId(r.defaultId)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  async function save() {
    if (!draft || !v.attempt()) return
    setError('')
    setSaving(true)
    try {
      const body = {
        name: draft.name.trim(),
        allowed: draft.allowed,
        cutoff: draft.cutoff,
        upgradeAllowed: draft.upgradeAllowed,
        mustContain: linesOf(draft.mustContain),
        mustNotContain: linesOf(draft.mustNotContain),
        preferred: parsePreferred(draft.preferred),
        fallback: draft.fallback,
        maxSizeGB: draft.maxSize.trim() === '' ? 0 : Number(draft.maxSize),
      }
      if (draft.id) await api.updateProfile(draft.id, body)
      else await api.createProfile(body)
      openDraft(null)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  async function remove(p: QualityProfile) {
    if (!(await confirm({ title: `Delete the "${p.name}" profile?`, body: <p>Movies and shows using it switch to the default profile.</p>, confirmLabel: 'Delete profile', danger: true }))) return
    setError('')
    try {
      await api.deleteProfile(p.id)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function makeDefault(id: number) {
    setError('')
    try {
      await api.putSettings({ defaultProfileId: id })
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  function toggleTier(t: string) {
    if (!draft) return
    const allowed = draft.allowed.includes(t) ? draft.allowed.filter((x) => x !== t) : [...draft.allowed, t]
    // Keep the cutoff valid: if its tier was just unticked, fall back to the best remaining one.
    const ordered = tiers.filter((x) => allowed.includes(x))
    const cutoff = ordered.includes(draft.cutoff) ? draft.cutoff : (ordered[ordered.length - 1] ?? '')
    setDraft({ ...draft, allowed, cutoff })
  }

  const orderedAllowed = draft ? tiers.filter((t) => draft.allowed.includes(t)) : []
  const defaultProfile = profiles.find((p) => p.id === defaultId)

  const form = draft && (
    <div className="profile-form" ref={formRef}>
      <h3>{draft.id ? 'Edit profile' : 'New profile'}</h3>
      <div className="grid-form profile-form-col">
        <label>
          Name
          <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} {...v.bind('name', draft.name, (name) => setDraft({ ...draft, name }))} />
          <FieldError v={v} name="name" />
        </label>
        <fieldset className="profile-fieldset">
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
        <label>
          Largest download (GB)
          <input inputMode="decimal" value={draft.maxSize} placeholder="no limit" style={{ maxWidth: 140 }} onChange={(e) => setDraft({ ...draft, maxSize: e.target.value })} {...v.bind('maxSize', draft.maxSize, (maxSize) => setDraft({ ...draft, maxSize }))} />
          <small style={{ color: 'var(--text-dim)' }}>Bigger releases are skipped. Leave empty for no limit. Fakes that are far too small are always skipped.</small>
          <FieldError v={v} name="maxSize" />
        </label>
        <label className="check-hint">
          <input type="checkbox" checked={draft.upgradeAllowed} onChange={(e) => setDraft({ ...draft, upgradeAllowed: e.target.checked })} />
          <span>
            Keep looking for better versions after it is downloaded
            <small>Swaps a download for a better one until it reaches the quality above.</small>
          </span>
        </label>
      </div>
      <div className="grid-form profile-form-col">
        <fieldset className="profile-fieldset">
          <legend>If nothing is found at this quality, also try</legend>
          <FallbackEditor profiles={profiles} selfId={draft.id} value={draft.fallback} onChange={(fallback) => setDraft({ ...draft, fallback })} />
        </fieldset>
        <details open={!!(draft.mustContain || draft.mustNotContain || draft.preferred)}>
          <summary style={{ cursor: 'pointer' }}>Release restrictions &amp; preferred terms (optional)</summary>
          <div className="grid-form" style={{ marginTop: 8 }}>
            <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem', margin: 0 }}>
              One term per line. Matched anywhere in the release title, ignoring capitals, with dots and dashes counting as spaces.
            </p>
            <label>
              Must contain at least one of
              <textarea rows={3} value={draft.mustContain} onChange={(e) => setDraft({ ...draft, mustContain: e.target.value })} placeholder="x265" {...v.bind('mustContain', draft.mustContain, (mustContain) => setDraft({ ...draft, mustContain }))} />
              <FieldError v={v} name="mustContain" />
            </label>
            <label>
              Must not contain any of
              <textarea rows={3} value={draft.mustNotContain} onChange={(e) => setDraft({ ...draft, mustNotContain: e.target.value })} placeholder={'HDR\nhardcoded'} {...v.bind('mustNotContain', draft.mustNotContain, (mustNotContain) => setDraft({ ...draft, mustNotContain }))} />
              <FieldError v={v} name="mustNotContain" />
            </label>
            <label>
              Preferred terms, with a score (higher wins between releases of the same quality)
              <textarea rows={3} value={draft.preferred} onChange={(e) => setDraft({ ...draft, preferred: e.target.value })} placeholder={'x265 = 50\nREPACK = 10\nDUBBED = -100'} {...v.bind('preferred', draft.preferred, (preferred) => setDraft({ ...draft, preferred }))} />
              <FieldError v={v} name="preferred" />
            </label>
          </div>
        </details>
      </div>
      <div className="profile-form-actions">
        <button className="primary" onClick={save} disabled={saving}>
          Save profile
        </button>
        <button onClick={() => openDraft(null)}>Cancel</button>
        <FormProblem v={v} verb="save this profile" />
      </div>
    </div>
  )

  return (
    <section className="card wide">
      <h2>Quality profiles</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        A profile sets which releases are acceptable and whether Mediarium keeps looking for a better version. Movies and shows use the default unless you pick another on their page.
      </p>
      {error && <p className="error-text">{error}</p>}

      <table className="profiles-table" style={{ marginBottom: 16 }}>
        <thead>
          <tr>
            <th>Name</th>
            <th>Qualities</th>
            <th style={{ whiteSpace: 'nowrap' }}>Stops upgrading at</th>
            <th style={{ whiteSpace: 'nowrap' }}>Better versions</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {profiles.map((p) => (
            <Fragment key={p.id}>
            <tr className={[draft?.id === p.id ? 'editing' : '', p.id === defaultId ? 'is-default' : ''].filter(Boolean).join(' ') || undefined}>
              <td>
                {p.name} {p.id === defaultId && <span className="badge downloaded">default</span>}
                {p.inUse > 0 && <div style={{ color: 'var(--text-dim)', fontSize: '0.75rem' }}>used by {p.inUse}</div>}
                <div style={{ color: 'var(--text-dim)', fontSize: '0.8rem', maxWidth: 320 }}>{profileBlurb(p)}</div>
                {(p.fallback ?? []).length > 0 && (
                  <div className="fallback-summary">
                    If nothing is found: {(p.fallback ?? []).map((id) => profiles.find((x) => x.id === id)?.name).filter(Boolean).join(' → ')}
                  </div>
                )}
              </td>
              <td style={{ color: 'var(--text-dim)', fontSize: '0.85rem' }}>
                {p.allowed.map(tierLabel).join(', ')}
                {(p.mustContain.length > 0 || p.mustNotContain.length > 0 || p.preferred.length > 0) && (
                  <div>
                    {p.mustContain.length > 0 && <span>requires: {p.mustContain.join(', ')}. </span>}
                    {p.mustNotContain.length > 0 && <span>excludes: {p.mustNotContain.join(', ')}. </span>}
                    {p.preferred.length > 0 && <span>prefers: {p.preferred.map((x) => `${x.term} (${x.score})`).join(', ')}.</span>}
                  </div>
                )}
              </td>
              <td style={{ whiteSpace: 'nowrap' }}>{tierLabel(p.cutoff)}</td>
              <td>{p.upgradeAllowed ? 'on' : 'off'}</td>
              <td style={{ whiteSpace: 'nowrap' }}>
                <button onClick={() => openDraft(draftFrom(p))}>Edit</button>{' '}
                <button onClick={() => openDraft({ ...draftFrom(p), id: undefined, name: copyName(p.name, profiles) })}>Duplicate</button>{' '}
                {p.id !== defaultId && <button onClick={() => makeDefault(p.id)}>Make default</button>}{' '}
                {p.id !== defaultId && <button onClick={() => remove(p)}>Delete</button>}
              </td>
            </tr>
            {draft?.id === p.id && (
              <tr className="profile-edit-row">
                <td colSpan={5}>{form}</td>
              </tr>
            )}
            </Fragment>
          ))}
        </tbody>
      </table>

      {defaultProfile && (
        <DefaultFallback key={defaultProfile.id} profile={defaultProfile} profiles={profiles} onSaved={load} />
      )}

      {draft && !draft.id && form}
      {!draft && <button onClick={() => openDraft(blankDraft(tiers))}>New profile…</button>}
    </section>
  )
}

// The default profile's fallback order, right under the list where the
// default is chosen: pick the default first, then its fallbacks.
function DefaultFallback({ profile, profiles, onSaved }: { profile: QualityProfile; profiles: QualityProfile[]; onSaved: () => void }) {
  const toast = useToast()
  const [value, setValue] = useState<number[]>(profile.fallback ?? [])
  const [saving, setSaving] = useState(false)
  const changed = JSON.stringify(value) !== JSON.stringify(profile.fallback ?? [])

  async function save() {
    setSaving(true)
    try {
      await api.updateProfile(profile.id, {
        name: profile.name,
        allowed: profile.allowed,
        cutoff: profile.cutoff,
        upgradeAllowed: profile.upgradeAllowed,
        mustContain: profile.mustContain,
        mustNotContain: profile.mustNotContain,
        preferred: profile.preferred,
        fallback: value,
      })
      toast.success(value.length ? `Saved: ${value.length} fallback ${value.length === 1 ? 'quality' : 'qualities'}.` : `Saved: no fallback, it waits for ${profile.name}.`)
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <fieldset className="group quality-fallback">
      <legend>
        <Icon name="sliders" size={14} /> When {profile.name} (your default) finds nothing
      </legend>
      <p>
        Tick the qualities to try instead and drag them into order. A title grabbed this way is upgraded once a{' '}
        <strong>{profile.name}</strong> release appears.
      </p>
      <FallbackEditor profiles={profiles} selfId={profile.id} value={value} onChange={setValue} />
      <div style={{ marginTop: 12 }}>
        <button className="primary" onClick={() => void save()} disabled={!changed || saving}>
          {saving ? 'Saving…' : 'Save order'}
        </button>
      </div>
    </fieldset>
  )
}
