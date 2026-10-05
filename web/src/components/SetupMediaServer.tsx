import { useState } from 'react'
import { type MediaServer, type MediaServerKind } from '../api'
import { FieldError, useValidation } from '../useValidation'
import { firstError, required, url as urlCheck } from '../validate'
import Icon from './Icon'
import { FindServers, JellyfinEmbySignIn, PlexSignIn } from './MediaServerSignIn'
import { MEDIA_SERVER_BRAND, MediaServerMark } from './mediaServerBrand'

const KINDS: MediaServerKind[] = ['plex', 'jellyfin', 'emby']
const EXAMPLE: Partial<Record<MediaServerKind, string>> = {
  plex: 'http://192.168.1.10:32400',
  jellyfin: 'http://192.168.1.10:8096',
  emby: 'http://192.168.1.10:8096',
}

// The optional media server step: sign in to Plex, Jellyfin or Emby so the
// "Watch in" links work and the server looks again when new files arrive.
export default function SetupMediaServer({
  preferred,
  servers,
  onAdded,
}: {
  // The kind that matches the media player the person picked, if any.
  preferred?: MediaServerKind
  servers: MediaServer[]
  onAdded: () => void
}) {
  const [pick, setPick] = useState<MediaServerKind | null>(preferred ?? servers[0]?.kind ?? null)
  const [address, setAddress] = useState('')
  const example = pick ? EXAMPLE[pick] : EXAMPLE.jellyfin
  const v = useValidation({ address: firstError(required(address, `Add the address of your server, for example ${example}.`), urlCheck(address, { example })) })

  return (
    <div className="ms-step">
      {servers.length > 0 && (
        <ul className="ms-connected">
          {servers.map((s) => (
            <li key={s.id}>
              <MediaServerMark kind={s.kind} size={26} />
              <span className="ms-found-name">
                <strong>{s.name || MEDIA_SERVER_BRAND[s.kind].label}</strong>
                <small>
                  {MEDIA_SERVER_BRAND[s.kind].label} · {s.baseUrl}
                </small>
              </span>
              <span className="badge downloaded">
                <Icon name="check" size={12} /> Connected
              </span>
            </li>
          ))}
        </ul>
      )}

      <div className="ms-pick" role="radiogroup" aria-label="Your media server">
        {KINDS.map((k) => (
          <button
            key={k}
            type="button"
            role="radio"
            aria-checked={pick === k}
            className={`choice-card ms-pick-card${pick === k ? ' active' : ''}`}
            style={{ ['--mc' as string]: MEDIA_SERVER_BRAND[k].color }}
            onClick={() => setPick(k)}
          >
            <MediaServerMark kind={k} size={30} />
            <strong>{MEDIA_SERVER_BRAND[k].label}</strong>
          </button>
        ))}
      </div>

      {pick === 'plex' && <PlexSignIn onAdded={onAdded} />}
      {(pick === 'jellyfin' || pick === 'emby') && (
        <div className="ms-form">
          <label>
            {MEDIA_SERVER_BRAND[pick].label} address
            <input value={address} onChange={(e) => setAddress(e.target.value)} placeholder={example} spellCheck={false} {...v.bind('address', address, setAddress)} />
            <FieldError v={v} name="address" />
            <small className="field-hint">The address you open {MEDIA_SERVER_BRAND[pick].label} at in your browser, including the port number.</small>
          </label>
          <FindServers
            onPick={(f) => {
              setPick(f.kind)
              setAddress(f.address)
            }}
          />
          <JellyfinEmbySignIn kind={pick} baseUrl={address} checkAddress={() => v.attempt()} onAdded={onAdded} />
        </div>
      )}
      {pick === null && <p className="fbox-calm">Pick yours to connect it, or press Continue to skip.</p>}
    </div>
  )
}
