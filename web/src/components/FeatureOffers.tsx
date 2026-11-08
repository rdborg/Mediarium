import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import { offersAfterUpdate, offersInSettings, type Offer, type OfferChoices, type OfferId } from '../featureOffers'
import { useToast } from './Toast'
import { t } from '../i18n'

// What is on already, for the offers (featureOffers.ts). Read once when shown.
function useOfferState(enabled: boolean) {
  const { modules, refresh } = useModules()
  const [state, setState] = useState<{ choices: OfferChoices; udp: boolean; tvdb: boolean; routes: boolean; rss: boolean; prowlarr: boolean; omdb: boolean; soulseek: boolean } | null>(null)
  const load = useCallback(async () => {
    const [choices, settings, tvdb, indexers, lists, prowlarr, omdb, soulseek] = await Promise.all([
      api.featureOffers().then((r) => r.choices).catch(() => ({}) as OfferChoices),
      api.getSettings().catch(() => null),
      api.getTheTVDB().catch(() => null),
      api.listIndexers().catch(() => []),
      api.importLists().catch(() => null),
      api.getProwlarr().catch(() => null),
      api.getOMDb().catch(() => null),
      api.getSoulseek().catch(() => null),
    ])
    setState({
      choices,
      udp: !!settings?.discoverThroughTunnel,
      tvdb: !!tvdb?.set,
      routes: indexers.some((i) => !!i.torrentRoute),
      rss: !!lists?.lists.some((l) => l.source === 'rss'),
      prowlarr: !!prowlarr?.hasKey,
      omdb: !!omdb?.set,
      soulseek: !!soulseek?.hasKey,
    })
  }, [])
  useEffect(() => {
    if (enabled) void load()
  }, [enabled, load])
  const isOn = (id: OfferId): boolean => {
    if (!state) return true // nothing is offered until it is known what is on
    switch (id) {
      case 'anime':
        return modules.anime.enabled
      case 'lifecycle':
        return modules.lifecycle.enabled
      case 'archiver':
        return modules.archiver.enabled
      case 'udp-dht':
        return state.udp
      case 'thetvdb':
        return state.tvdb
      case 'torrent-route':
        return state.routes
      case 'rss-lists':
        return state.rss
      case 'prowlarr':
        return state.prowlarr
      case 'omdb':
        return state.omdb
      case 'soulseek':
        return state.soulseek || !modules.music.enabled // only offered with music on
    }
  }
  return { state, isOn, reload: load, refreshModules: refresh }
}

// The rows of offers: in the What's new window after an update (mode
// "update"), and under Settings > Media types (mode "settings"), where a Never
// can be taken back. Administrators only; nothing shows for anyone else.
export default function FeatureOffers({ mode, version, lastSeen, onOpen }: { mode: 'update' | 'settings'; version: string; lastSeen?: string | null; onOpen?: () => void }) {
  const admin = isAdmin(useAuth().user)
  const toast = useToast()
  const navigate = useNavigate()
  const { state, isOn, reload, refreshModules } = useOfferState(admin)
  const [busy, setBusy] = useState<OfferId | null>(null)
  if (!admin || !state) return null
  const offers = mode === 'update' ? offersAfterUpdate(lastSeen ?? null, version, state.choices, isOn) : offersInSettings(version, isOn)
  if (offers.length === 0) return null

  async function answer(o: Offer, choice: 'later' | 'never' | 'clear') {
    setBusy(o.id)
    try {
      await api.answerFeatureOffer({ id: o.id, choice, version })
      await reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  async function take(o: Offer) {
    // The archiver can't be switched on from here: it needs the warning ticked
    // and a yt-dlp file first, so its button opens the card that has them.
    const needsSetup = o.action.kind === 'module' && o.action.module === 'archiver'
    if (o.action.kind === 'open' || needsSetup) {
      await api.answerFeatureOffer({ id: o.id, choice: 'later', version }).catch(() => undefined)
      onOpen?.()
      navigate(o.action.kind === 'open' ? o.action.to : '/settings/modules')
      return
    }
    setBusy(o.id)
    try {
      if (o.action.kind === 'module') {
        await api.setModules({ [o.action.module]: true })
        await refreshModules()
      } else {
        await api.putSettings({ [o.action.field]: true })
      }
      toast.success(t('{name} is on.', { name: o.title }))
      await reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section className="feature-offers">
      <h3>{mode === 'update' ? t('New, and off until you switch it on') : t('New things you can switch on')}</h3>
      <ul>
        {offers.map((o) => {
          const never = state.choices[o.id] === 'never'
          return (
            <li key={o.id}>
              <div className="offer-text">
                <strong>{o.title}</strong>
                <small>{o.text}</small>
              </div>
              <div className="offer-actions">
                <button className="btn-sm primary" disabled={busy === o.id} onClick={() => void take(o)}>
                  {o.action.kind === 'open' || o.id === 'archiver' ? t('Set it up') : t('Switch on')}
                </button>
                {mode === 'update' && (
                  <>
                    <button className="btn-sm" disabled={busy === o.id} onClick={() => void answer(o, 'later')}>
                      {t('Not now')}
                    </button>
                    <button className="btn-sm" disabled={busy === o.id} onClick={() => void answer(o, 'never')}>
                      {t('Never')}
                    </button>
                  </>
                )}
                {mode === 'settings' && never && (
                  <button className="btn-sm" disabled={busy === o.id} onClick={() => void answer(o, 'clear')}>
                    {t('Offer it again after updates')}
                  </button>
                )}
              </div>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
