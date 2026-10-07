import RenameCard from '../../components/RenameCard'
import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useToast } from '../../components/Toast'
import { api, type Settings as SettingsData } from '../../api'
import FolderStatus from '../../components/FolderStatus'
import Icon from '../../components/Icon'
import HardlinkWarning from '../../components/HardlinkWarning'
import NamingPreview from '../../components/NamingPreview'
import NamingBuilder from '../../components/NamingBuilder'
import { useModules } from '../../ModulesContext'
import { firstError, folderPath, required } from '../../validate'
import { coreParent, pathInside, type FolderKind } from '../../setupHelpers'
import { FieldError, FormProblem, useValidation } from '../../useValidation'

// A quick check while typing; the full one (known tokens, padding, a title
// in every name) comes from the server with the preview and on save.
function namingFormat(value: string): string | null {
  const v = value.trim()
  if (v === '') return null
  if (/[\\/]/.test(v)) return "A file name can't contain / or \\. Only write the name of the file, the folder is chosen for you."
  const opens = (v.match(/\{/g) ?? []).length
  const closes = (v.match(/\}/g) ?? []).length
  if (opens !== closes) return 'A { or } is missing. Wrap each token in curly braces, like {Movie Title}.'
  return null
}

function LibrarySection() {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [moviesPath, setMoviesPath] = useState('')
  const [tvPath, setTvPath] = useState('')
  const [musicPath, setMusicPath] = useState('')
  const [ebooksPath, setEbooksPath] = useState('')
  const [audiobooksPath, setAudiobooksPath] = useState('')
  const [downloadsPath, setDownloadsPath] = useState('')
  const [namingPreset, setNamingPreset] = useState('plex')
  const [customFormat, setCustomFormat] = useState('')
  const [episodeFormat, setEpisodeFormat] = useState('')
  const [illegalCharMode, setIllegalCharMode] = useState('strip')
  const [illegalCharReplacement, setIllegalCharReplacement] = useState('-')
  const [importConflictPolicy, setImportConflictPolicy] = useState('skip')
  const [loadError, setLoadError] = useState('')
  const [saving, setSaving] = useState(false)
  const toast = useToast()
  const navigate = useNavigate()
  const { on } = useModules()
  const musicOn = on('music')
  const ebooksOn = on('ebooks')
  const audiobooksOn = on('audiobooks')
  const errors = {
    moviesPath: firstError(required(moviesPath, 'Add the folder your movies live in, for example /movies.'), folderPath(moviesPath, '/movies')),
    tvPath: folderPath(tvPath, '/tv'),
    musicPath: musicOn ? folderPath(musicPath, '/music') : null,
    ebooksPath: ebooksOn ? firstError(required(ebooksPath, 'Add the folder your ebooks go in, for example /books.'), folderPath(ebooksPath, '/books')) : null,
    audiobooksPath: audiobooksOn ? firstError(required(audiobooksPath, 'Add the folder your audiobooks go in, for example /audiobooks.'), folderPath(audiobooksPath, '/audiobooks')) : null,
    downloadsPath: firstError(required(downloadsPath, 'Add the folder downloads should go to, for example /downloads.'), folderPath(downloadsPath, '/downloads')),
    customFormat: namingPreset === 'custom' ? firstError(required(customFormat, 'Add a naming format, for example {Movie Title} ({Year}).'), namingFormat(customFormat)) : null,
    episodeFormat: namingPreset === 'custom' ? namingFormat(episodeFormat) : null,
    illegalCharReplacement:
      illegalCharMode === 'replace'
        ? firstError(
            required(illegalCharReplacement, 'Type the character to use instead, for example -.'),
            // eslint-disable-next-line no-control-regex
            /[<>:"/\\|?*\u0000-\u001f]/.test(illegalCharReplacement) && "That character isn't allowed in file names either. Try - or _.",
          )
        : null,
  }
  const v = useValidation(errors)
  // Where a missing folder could go instead: next to movies, TV and downloads
  // when they share one mapped folder (for example /data/Ebooks).
  const suggestFor = (kind: FolderKind) => {
    const { parent, sample } = coreParent({ movies: moviesPath, tv: tvPath, downloads: downloadsPath })
    return parent ? pathInside(parent, kind, sample) : undefined
  }

  const load = useCallback(() => {
    setLoadError('')
    api
      .getSettings()
      .then((s) => {
        setSettings(s)
        setMoviesPath(s.moviesPath)
        setTvPath(s.tvPath ?? '')
        setMusicPath(s.musicPath ?? '')
        setEbooksPath(s.ebooksPath ?? '')
        setAudiobooksPath(s.audiobooksPath ?? '')
        setDownloadsPath(s.downloadsPath)
        setNamingPreset(s.namingPreset || 'plex')
        setCustomFormat(s.movieNameFormat)
        setEpisodeFormat(s.episodeNameFormat ?? '')
        setIllegalCharMode(s.illegalCharMode || 'strip')
        setIllegalCharReplacement(s.illegalCharReplacement || '-')
        setImportConflictPolicy(s.importConflictPolicy || 'skip')
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  async function save() {
    if (!v.attempt()) return
    setSaving(true)
    try {
      const updated = await api.putSettings({
        moviesPath,
        tvPath,
        musicPath: musicPath.trim(),
        ebooksPath: ebooksPath.trim() || undefined,
        audiobooksPath: audiobooksPath.trim() || undefined,
        downloadsPath,
        namingPreset,
        movieNameFormat: customFormat,
        episodeNameFormat: episodeFormat,
        illegalCharMode,
        illegalCharReplacement,
        importConflictPolicy,
      })
      setSettings(updated)
      toast.success('Saved.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  if (!settings) {
    return loadError ? (
      <section className="card span-all">
        <p className="error-text">{loadError}</p>
        <button onClick={load}>Try again</button>
      </section>
    ) : null
  }

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
              <input value={moviesPath} onChange={(e) => setMoviesPath(e.target.value)} {...v.bind('moviesPath', moviesPath, setMoviesPath)} />
              <FieldError v={v} name="moviesPath" />
            </label>
            <FolderStatus path={moviesPath} />
          </div>
          <div className="path-col">
            <label>
              TV folder
              <input value={tvPath} onChange={(e) => setTvPath(e.target.value)} {...v.bind('tvPath', tvPath, setTvPath)} />
              <FieldError v={v} name="tvPath" />
            </label>
            <FolderStatus path={tvPath} />
          </div>
          <div className={`path-col${musicOn ? '' : ' path-off'}`}>
            <label>
              Music folder
              <input value={musicPath} onChange={(e) => setMusicPath(e.target.value)} placeholder="/music" spellCheck={false} disabled={!musicOn} {...v.bind('musicPath', musicPath, setMusicPath)} />
              <FieldError v={v} name="musicPath" />
            </label>
            {musicOn ? (
              <>
                <FolderStatus path={musicPath} />
                <div>
                  <button className="btn-sm btn-with-icon" onClick={() => navigate('/music/import')}>
                    <Icon name="folder" size={15} /> Import my existing collection
                  </button>
                </div>
              </>
            ) : (
              <small className="path-note">Turn on Music in Media types first.</small>
            )}
          </div>
          <div className={`path-col${ebooksOn ? '' : ' path-off'}`}>
            <label>
              Ebooks folder
              <input value={ebooksPath} onChange={(e) => setEbooksPath(e.target.value)} placeholder="/books" spellCheck={false} disabled={!ebooksOn} {...v.bind('ebooksPath', ebooksPath, setEbooksPath)} />
              <FieldError v={v} name="ebooksPath" />
            </label>
            {ebooksOn ? <FolderStatus path={ebooksPath} suggest={suggestFor('ebooks')} onUse={setEbooksPath} /> : <small className="path-note">Turn on Ebooks in Media types first.</small>}
          </div>
          <div className={`path-col${audiobooksOn ? '' : ' path-off'}`}>
            <label>
              Audiobooks folder
              <input value={audiobooksPath} onChange={(e) => setAudiobooksPath(e.target.value)} placeholder="/audiobooks" spellCheck={false} disabled={!audiobooksOn} {...v.bind('audiobooksPath', audiobooksPath, setAudiobooksPath)} />
              <FieldError v={v} name="audiobooksPath" />
            </label>
            {audiobooksOn ? <FolderStatus path={audiobooksPath} suggest={suggestFor('audiobooks')} onUse={setAudiobooksPath} /> : <small className="path-note">Turn on Audiobooks in Media types first.</small>}
          </div>
          <div className="path-col">
            <label>
              Downloads folder
              <input value={downloadsPath} onChange={(e) => setDownloadsPath(e.target.value)} {...v.bind('downloadsPath', downloadsPath, setDownloadsPath)} />
              <FieldError v={v} name="downloadsPath" />
            </label>
            <FolderStatus path={downloadsPath} />
          </div>
        </div>
        <HardlinkWarning
          libraries={[
            { label: 'movies', path: moviesPath },
            { label: 'TV shows', path: tvPath },
            ...(musicOn ? [{ label: 'music', path: musicPath }] : []),
            ...(ebooksOn ? [{ label: 'ebooks', path: ebooksPath }] : []),
            ...(audiobooksOn ? [{ label: 'audiobooks', path: audiobooksPath }] : []),
          ]}
          downloads={downloadsPath}
        />
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
            {namingPreset === 'custom' ? (
              <small style={{ color: 'var(--text-dim)' }}>Build your own movie and episode file names below.</small>
            ) : (
              <>
                <NamingPreview preset={namingPreset} />
                <NamingPreview preset={namingPreset} kind="tv" />
              </>
            )}
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
                <input value={illegalCharReplacement} onChange={(e) => setIllegalCharReplacement(e.target.value)} maxLength={3} {...v.bind('illegalCharReplacement', illegalCharReplacement, setIllegalCharReplacement)} />
                <FieldError v={v} name="illegalCharReplacement" />
              </label>
            )}
            <small style={{ color: 'var(--text-dim)' }}>Characters like : ? * aren't allowed in file names on some systems.</small>
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
            <small style={{ color: 'var(--text-dim)' }}>What to do when a download would replace a file already in your library.</small>
          </div>
        </div>
        {namingPreset === 'custom' && <NamingBuilder movieFormat={customFormat} setMovieFormat={setCustomFormat} episodeFormat={episodeFormat} setEpisodeFormat={setEpisodeFormat} v={v} />}
        <div style={{ marginTop: 14 }}>
          <button className="primary" onClick={save} disabled={saving}>
            Save
          </button>
          <FormProblem v={v} />
        </div>
      </fieldset>
    </>
  )
}

export default function MediaSettings() {
  return (
    <div className="settings-stack">
      <LibrarySection />
      <RenameCard />
    </div>
  )
}
