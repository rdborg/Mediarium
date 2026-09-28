import { useEffect, useState } from 'react'
import { useToast } from '../../components/Toast'
import { api, type Settings as SettingsData } from '../../api'
import FolderStatus from '../../components/FolderStatus'
import HardlinkWarning from '../../components/HardlinkWarning'
import NamingPreview from '../../components/NamingPreview'

function LibrarySection() {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [moviesPath, setMoviesPath] = useState('')
  const [tvPath, setTvPath] = useState('')
  const [downloadsPath, setDownloadsPath] = useState('')
  const [namingPreset, setNamingPreset] = useState('plex')
  const [customFormat, setCustomFormat] = useState('')
  const [illegalCharMode, setIllegalCharMode] = useState('strip')
  const [illegalCharReplacement, setIllegalCharReplacement] = useState('-')
  const [importConflictPolicy, setImportConflictPolicy] = useState('skip')
  const toast = useToast()

  useEffect(() => {
    api.getSettings().then((s) => {
      setSettings(s)
      setMoviesPath(s.moviesPath)
      setTvPath(s.tvPath ?? '')
      setDownloadsPath(s.downloadsPath)
      setNamingPreset(s.namingPreset || 'plex')
      setCustomFormat(s.movieNameFormat)
      setIllegalCharMode(s.illegalCharMode || 'strip')
      setIllegalCharReplacement(s.illegalCharReplacement || '-')
      setImportConflictPolicy(s.importConflictPolicy || 'skip')
    })
  }, [])

  async function save() {
        try {
      const updated = await api.putSettings({
        moviesPath,
        tvPath,
        downloadsPath,
        namingPreset,
        movieNameFormat: customFormat,
        illegalCharMode,
        illegalCharReplacement,
        importConflictPolicy,
      })
      setSettings(updated)
      toast.success('Saved.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (!settings) return null

  return (
    <section className="card grid-form wide">
      <h2>Library Paths & Naming</h2>
      <label>
        Movies folder
        <input value={moviesPath} onChange={(e) => setMoviesPath(e.target.value)} />
      </label>
      <FolderStatus path={moviesPath} />
      <label>
        TV folder
        <input value={tvPath} onChange={(e) => setTvPath(e.target.value)} />
      </label>
      <FolderStatus path={tvPath} />
      <label>
        Downloads folder
        <input value={downloadsPath} onChange={(e) => setDownloadsPath(e.target.value)} />
      </label>
      <FolderStatus path={downloadsPath} />
      <HardlinkWarning pathA={moviesPath} pathB={downloadsPath} />
      <label>
        Naming preset
        <select value={namingPreset} onChange={(e) => setNamingPreset(e.target.value)}>
          <option value="plex">Plex</option>
          <option value="jellyfin">Jellyfin</option>
          <option value="kodi">Kodi</option>
          <option value="minimal">Minimal</option>
          <option value="custom">Custom</option>
        </select>
      </label>
      {namingPreset === 'custom' && (
        <label>
          Custom format
          <input value={customFormat} onChange={(e) => setCustomFormat(e.target.value)} placeholder="{Movie Title} ({Year}) [{Quality}]" />
        </label>
      )}
      <NamingPreview preset={namingPreset} format={namingPreset === 'custom' ? customFormat : undefined} />
      <label>
        Illegal character handling
        <select value={illegalCharMode} onChange={(e) => setIllegalCharMode(e.target.value)}>
          <option value="strip">Strip</option>
          <option value="replace">Replace</option>
        </select>
      </label>
      {illegalCharMode === 'replace' && (
        <label>
          Replacement character
          <input value={illegalCharReplacement} onChange={(e) => setIllegalCharReplacement(e.target.value)} maxLength={3} />
        </label>
      )}
      <label>
        If an import would overwrite an existing file
        <select value={importConflictPolicy} onChange={(e) => setImportConflictPolicy(e.target.value)}>
          <option value="skip">Skip (leave the existing file alone)</option>
          <option value="overwrite">Always overwrite</option>
          <option value="overwrite_if_better">Overwrite only if the new file is better quality</option>
          <option value="ask">Always ask (review each one in Activity / Queue)</option>
        </select>
      </label>
      <button className="primary" onClick={save}>
        Save
      </button>
    </section>
  )
}

export default function MediaSettings() {
  return (
    <div className="settings-stack">
      <LibrarySection />
    </div>
  )
}
