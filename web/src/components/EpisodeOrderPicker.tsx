import { useState } from 'react'
import { api, can, type EpisodeOrder } from '../api'
import { useAuth } from '../AuthContext'
import { useToast } from './Toast'
import { t } from '../i18n'
import HelpLink from './HelpLink'

// On a show's page, next to Episode numbering: which list the show's regular
// seasons follow. TMDB counts a double-length episode as one, TheTVDB as two,
// as many people's file names do. TheTVDB's order needs a saved TheTVDB key;
// without one that choice is off and says why. hasKey comes with the show
// (the TheTVDB settings themselves are for administrators only).
export default function EpisodeOrderPicker({ id, value, hasKey, onChanged }: { id: number; value: EpisodeOrder; hasKey: boolean; onChanged: (order: EpisodeOrder) => void }) {
  const toast = useToast()
  const me = useAuth().user
  const [busy, setBusy] = useState(false)
  async function change(order: EpisodeOrder) {
    setBusy(true)
    try {
      const res = await api.setEpisodeOrder(id, order)
      onChanged(order)
      toast.success(order === 'tvdb' ? t("This show follows TheTVDB's episode order now.") : t("This show follows the movie database's episode order again."))
      if (res.leftAsIs.length > 0) {
        toast.error(t('Left as it was, because a file in it could not be matched with confidence: season {seasons}.', { seasons: res.leftAsIs.join(', ') }))
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const keyMissing = !hasKey && value !== 'tvdb'
  return (
    <label className="inline-field episode-order" title={keyMissing ? t('TheTVDB order needs a TheTVDB key. Save one in Settings, Info, lists and subtitles.') : undefined}>
      <span className="has-tip">
        {t('Episode order')}
        <HelpLink topic="library#episode-order" text={t("Which list this show's seasons follow. TMDB counts a double-length episode as one; TheTVDB counts it as two, as many file names do (The Office season 4 is 14 episodes on TMDB and 19 on TheTVDB). TheTVDB order needs a TheTVDB key. Your files stay on their episodes, and nothing is renamed.")} />
      </span>
      <select value={value} disabled={busy || !can(me, 'manage')} onChange={(e) => void change(e.target.value as EpisodeOrder)}>
        <option value="tmdb">{t('TMDB order (default)')}</option>
        <option value="tvdb" disabled={keyMissing}>
          {keyMissing ? t('TheTVDB order (needs a key)') : t('TheTVDB order')}
        </option>
      </select>
    </label>
  )
}
