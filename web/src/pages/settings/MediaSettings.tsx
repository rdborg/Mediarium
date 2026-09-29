import { useEffect, useState } from 'react'
import { useToast } from '../../components/Toast'
import { api, type Settings as SettingsData } from '../../api'
import FolderStatus from '../../components/FolderStatus'
import Icon from '../../components/Icon'
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
    <>
      <fieldset className="group folders span-all">
        <legend>
          <Icon name="folder" size={14} /> Library folders
        </legend>
        <div className="path-cols">
          <div className="path-col">
            <label>
              Movies folder
              <input value={moviesPath} onChange={(e) => setMoviesPath(e.target.value)} />
            </label>
            <FolderStatus path={moviesPath} />
          </div>
          <div className="path-col">
            <label>
              TV folder
              <input value={tvPath} onChange={(e) => setTvPath(e.target.value)} />
            </label>
            <FolderStatus path={tvPath} />
          </div>
          <div className="path-col">
            <label>
              Downloads folder
              <input value={downloadsPath} onChange={(e) => setDownloadsPath(e.target.value)} />
            </label>
            <FolderStatus path={downloadsPath} />
          </div>
        </div>
        <HardlinkWarning pathA={moviesPath} pathB={downloadsPath} />
      </fieldset>

      <fieldset className="group span-all">
        <legend>
          <Icon name="list" size={14} /> Naming and importing
        </legend>
        <div className="triple-cols">
          <div className="grid-form">
            <label>
              Naming preset
              <select value={namingPreset} onChange={(e) => setNamingPreset(e.target.value)}>
                <option value="plex">Plex</option>
                <option value="jellyfin">Jellyfin / Emby</option>
                <option value="kodi">Kodi</option>
                <option value="minimal">Simple</option>
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
          </div>
          <div className="grid-form">
            <label>
              Illegal characters in names
              <select value={illegalCharMode} onChange={(e) => setIllegalCharMode(e.target.value)}>
                <option value="strip">Remove them</option>
                <option value="replace">Replace them</option>
              </select>
            </label>
            {illegalCharMode === 'replace' && (
              <label>
                Replace with
                <input value={illegalCharReplacement} onChange={(e) => setIllegalCharReplacement(e.target.value)} maxLength={3} />
              </label>
            )}
            <small style={{ color: 'var(--text-dim)' }}>Characters like : ? * that are not allowed in file names on some systems.</small>
          </div>
          <div className="grid-form">
            <label>
              If a file already exists
              <select value={importConflictPolicy} onChange={(e) => setImportConflictPolicy(e.target.value)}>
                <option value="skip">Skip (leave the existing file alone)</option>
                <option value="overwrite">Always overwrite</option>
                <option value="overwrite_if_better">Overwrite only if the new file is better</option>
                <option value="ask">Ask me each time (in Activity)</option>
              </select>
            </label>
            <small style={{ color: 'var(--text-dim)' }}>What happens when a download would replace a file that is already in your library.</small>
          </div>
        </div>
        <div style={{ marginTop: 14 }}>
          <button className="primary" onClick={save}>
            Save
          </button>
        </div>
      </fieldset>
    </>
  )
}

export default function MediaSettings() {
  return (
    <div className="settings-stack">
      <LibrarySection />
    </div>
  )
}
