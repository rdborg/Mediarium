import { useCallback, useEffect, useState } from 'react'
import { api, type PreferredTerm, type QualityProfile } from '../api'

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

const draftFrom = (p: QualityProfile): Draft => ({
  id: p.id,
  name: p.name,
  allowed: p.allowed,
  cutoff: p.cutoff,
  upgradeAllowed: p.upgradeAllowed,
  mustContain: p.mustContain.join('\n'),
  mustNotContain: p.mustNotContain.join('\n'),
  preferred: p.preferred.map((x) => `${x.term} = ${x.score}`).join('\n'),
})

const blankDraft = (tiers: string[]): Draft => ({
  name: '',
  allowed: tiers.filter((t) => t === 'WEBDL-1080p' || t === 'Bluray-1080p'),
  cutoff: 'Bluray-1080p',
  upgradeAllowed: true,
  mustContain: '',
  mustNotContain: '',
  preferred: '',
})

const tierLabel = (t: string) => (t === 'Unknown' ? 'Unknown (untagged releases)' : t)

// Editable quality profiles, Radarr/Sonarr style: which qualities are
// acceptable, the cutoff at which upgrading stops, and whether upgrades
// happen at all. The default profile applies to anything without its own.
export default function QualityProfilesSection() {
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [tiers, setTiers] = useState<string[]>([])
  const [defaultId, setDefaultId] = useState(0)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .listProfiles()
      .then((r) => {
        setProfiles(r.profiles)
        setTiers(r.tiers)
        setDefaultId(r.defaultId)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  async function save() {
    if (!draft) return
    setError('')
    try {
      const body = {
        name: draft.name,
        allowed: draft.allowed,
        cutoff: draft.cutoff,
        upgradeAllowed: draft.upgradeAllowed,
        mustContain: linesOf(draft.mustContain),
        mustNotContain: linesOf(draft.mustNotContain),
        preferred: parsePreferred(draft.preferred),
      }
      if (draft.id) await api.updateProfile(draft.id, body)
      else await api.createProfile(body)
      setDraft(null)
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove(p: QualityProfile) {
    if (!window.confirm(`Delete the "${p.name}" profile?`)) return
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

  return (
    <section className="card wide">
      <h2>Quality profiles</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        A profile decides which releases are acceptable and when Mediarium stops looking for a better one. The default
        applies to everything unless you pick a different profile on a movie's or show's page.
      </p>
      {error && <p className="error-text">{error}</p>}

      <table style={{ marginBottom: 16 }}>
        <thead>
          <tr>
            <th>Name</th>
            <th>Qualities</th>
            <th>Cutoff</th>
            <th>Upgrades</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {profiles.map((p) => (
            <tr key={p.id}>
              <td>
                {p.name} {p.id === defaultId && <span className="badge downloaded">default</span>}
                {p.inUse > 0 && <div style={{ color: 'var(--text-dim)', fontSize: '0.75rem' }}>used by {p.inUse}</div>}
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
              <td>{tierLabel(p.cutoff)}</td>
              <td>{p.upgradeAllowed ? 'on' : 'off'}</td>
              <td style={{ whiteSpace: 'nowrap' }}>
                <button onClick={() => setDraft(draftFrom(p))}>Edit</button>{' '}
                {p.id !== defaultId && <button onClick={() => makeDefault(p.id)}>Make default</button>}{' '}
                {p.id !== defaultId && <button onClick={() => remove(p)}>Delete</button>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {draft ? (
        <div className="grid-form">
          <h3 style={{ margin: 0 }}>{draft.id ? 'Edit profile' : 'New profile'}</h3>
          <label>
            Name
            <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
          </label>
          <fieldset style={{ border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: 12 }}>
            <legend>Accepted qualities</legend>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(190px, 1fr))', gap: 6 }}>
              {tiers.map((t) => (
                <label key={t} style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                  <input type="checkbox" checked={draft.allowed.includes(t)} onChange={() => toggleTier(t)} />
                  {tierLabel(t)}
                </label>
              ))}
            </div>
          </fieldset>
          <label>
            Stop upgrading once this quality is reached
            <select value={draft.cutoff} onChange={(e) => setDraft({ ...draft, cutoff: e.target.value })}>
              {orderedAllowed.map((t) => (
                <option key={t} value={t}>
                  {tierLabel(t)}
                </option>
              ))}
            </select>
          </label>
          <label style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
            <input type="checkbox" checked={draft.upgradeAllowed} onChange={(e) => setDraft({ ...draft, upgradeAllowed: e.target.checked })} />
            Keep looking for better releases after the first download
          </label>
          <details open={!!(draft.mustContain || draft.mustNotContain || draft.preferred)}>
            <summary style={{ cursor: 'pointer' }}>Release restrictions &amp; preferred terms (optional)</summary>
            <div className="grid-form" style={{ marginTop: 8 }}>
              <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem', margin: 0 }}>
                One term per line, matched anywhere in the release title (case-insensitive; dots and dashes count as spaces).
              </p>
              <label>
                Must contain at least one of
                <textarea rows={3} value={draft.mustContain} onChange={(e) => setDraft({ ...draft, mustContain: e.target.value })} placeholder="x265" />
              </label>
              <label>
                Must not contain any of
                <textarea rows={3} value={draft.mustNotContain} onChange={(e) => setDraft({ ...draft, mustNotContain: e.target.value })} placeholder={'HDR\nhardcoded'} />
              </label>
              <label>
                Preferred terms, with a score (higher wins between releases of the same quality)
                <textarea rows={3} value={draft.preferred} onChange={(e) => setDraft({ ...draft, preferred: e.target.value })} placeholder={'x265 = 50\nREPACK = 10\nDUBBED = -100'} />
              </label>
            </div>
          </details>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="primary" onClick={save} disabled={!draft.name.trim() || draft.allowed.length === 0}>
              Save profile
            </button>
            <button onClick={() => setDraft(null)}>Cancel</button>
          </div>
        </div>
      ) : (
        <button onClick={() => setDraft(blankDraft(tiers))}>New profile…</button>
      )}
    </section>
  )
}
