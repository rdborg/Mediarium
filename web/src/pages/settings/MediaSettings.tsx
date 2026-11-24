import ExtraFolders from '../../components/ExtraFolders'
import FilePermissionsCard from '../../components/FilePermissionsCard'
import MetadataFilesCard from '../../components/MetadataFilesCard'
import RenameCard from '../../components/RenameCard'
import ScriptCard from '../../components/ScriptCard'
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
import { t } from '../../i18n'
import TipLabel from '../../components/TipLabel'

// A quick check while typing; the full one (known tokens, padding, a title
// in every name) comes from the server with the preview and on save.
function namingFormat(value: string): string | null {
  const v = value.trim()
  if (v === '') return null
  if (/[\\/]/.test(v)) return t("A file name can't contain / or \\. Only write the name of the file, the folder is chosen for you.")
  const opens = (v.match(/\{/g) ?? []).length
  const closes = (v.match(/\}/g) ?? []).length
  if (opens !== closes) return t('A { or } is missing. Wrap each token in curly braces, like {Movie Title}.')
  return null
}

// The quick check for a folder format: the same brace check, with the wording for folders.
function folderFormat(value: string): string | null {
  const v = value.trim()
  if (v === '') return null
  if (/[\\/]/.test(v)) return t("A folder name can't contain / or \\. Season folders are made for you, so only write the name of the title's folder.")
  const opens = (v.match(/\{/g) ?? []).length
  const closes = (v.match(/\}/g) ?? []).length
  if (opens !== closes) return t('A { or } is missing. Wrap each token in curly braces, like {Movie Title}.')
  return null
}

// One of the two Library pages shares this form: the folders, or the naming rules.
// Each page saves only its own fields.
function LibrarySection({ part, onSettings }: { part: 'folders' | 'naming'; onSettings?: (s: SettingsData) => void }) {
  const [settings, setSettingsState] = useState<SettingsData | null>(null)
  const setSettings = useCallback(
    (s: SettingsData) => {
      setSettingsState(s)
      onSettings?.(s)
    },
    [onSettings],
  )
  const [moviesPath, setMoviesPath] = useState('')
  const [tvPath, setTvPath] = useState('')
  const [movieCopiesPath, setMovieCopiesPath] = useState('')
  const [tvCopiesPath, setTvCopiesPath] = useState('')
  const [musicPath, setMusicPath] = useState('')
  const [ebooksPath, setEbooksPath] = useState('')
  const [audiobooksPath, setAudiobooksPath] = useState('')
  const [otherPath, setOtherPath] = useState('')
  const [booksLayout, setBooksLayout] = useState('author_title')
  const [downloadsPath, setDownloadsPath] = useState('')
  const [namingPreset, setNamingPreset] = useState('plex')
  const [customFormat, setCustomFormat] = useState('')
  const [episodeFormat, setEpisodeFormat] = useState('')
  const [movieFolder, setMovieFolder] = useState('')
  const [seriesFolder, setSeriesFolder] = useState('')
  const [keepOriginalNames, setKeepOriginalNames] = useState(false)
  const [illegalCharMode, setIllegalCharMode] = useState('strip')
  const [illegalCharReplacement, setIllegalCharReplacement] = useState('-')
  const [importConflictPolicy, setImportConflictPolicy] = useState('skip')
  const [checkDuration, setCheckDuration] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [saving, setSaving] = useState(false)
  const toast = useToast()
  const navigate = useNavigate()
  const { on } = useModules()
  const musicOn = on('music')
  const ebooksOn = on('ebooks')
  const audiobooksOn = on('audiobooks')
  const otherOn = on('other')
  const pathErrors = {
    moviesPath: firstError(required(moviesPath, t('Add the folder your movies live in, for example /movies.')), folderPath(moviesPath, '/movies')),
    tvPath: folderPath(tvPath, '/tv'),
    movieCopiesPath: movieCopiesPath.trim() ? folderPath(movieCopiesPath, '/movies-4k') : null,
    tvCopiesPath: tvCopiesPath.trim() ? folderPath(tvCopiesPath, '/tv-4k') : null,
    musicPath: musicOn ? folderPath(musicPath, '/music') : null,
    ebooksPath: ebooksOn ? firstError(required(ebooksPath, t('Add the folder your ebooks go in, for example /books.')), folderPath(ebooksPath, '/books')) : null,
    audiobooksPath: audiobooksOn ? firstError(required(audiobooksPath, t('Add the folder your audiobooks go in, for example /audiobooks.')), folderPath(audiobooksPath, '/audiobooks')) : null,
    otherPath: otherOn ? firstError(required(otherPath, t('Add the folder your other items go in, for example /other.')), folderPath(otherPath, '/other')) : null,
    downloadsPath: firstError(required(downloadsPath, t('Add the folder downloads should go to, for example /downloads.')), folderPath(downloadsPath, '/downloads')),
  }
  const namingErrors = {
    customFormat: namingPreset === 'custom' ? firstError(required(customFormat, t('Add a naming format, for example {Movie Title} ({Year}).')), namingFormat(customFormat)) : null,
    episodeFormat: namingPreset === 'custom' ? namingFormat(episodeFormat) : null,
    movieFolder: namingPreset === 'custom' ? folderFormat(movieFolder) : null,
    seriesFolder: namingPreset === 'custom' ? folderFormat(seriesFolder) : null,
    illegalCharReplacement:
      illegalCharMode === 'replace'
        ? firstError(
            required(illegalCharReplacement, t('Type the character to use instead, for example -.')),
            // eslint-disable-next-line no-control-regex
            /[<>:"/\\|?*\u0000-\u001f]/.test(illegalCharReplacement) && t("That character isn't allowed in file names either. Try - or _."),
          )
        : null,
  }
  const v = useValidation(part === 'folders' ? pathErrors : namingErrors)
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
        setMovieCopiesPath(s.movieCopiesPath ?? '')
        setTvCopiesPath(s.tvCopiesPath ?? '')
        setMusicPath(s.musicPath ?? '')
        setEbooksPath(s.ebooksPath ?? '')
        setAudiobooksPath(s.audiobooksPath ?? '')
        setOtherPath(s.otherPath ?? '')
        setBooksLayout(s.booksFolderLayout === 'author_series' ? 'author_series' : 'author_title')
        setDownloadsPath(s.downloadsPath)
        setNamingPreset(s.namingPreset || 'plex')
        setCustomFormat(s.movieNameFormat)
        setEpisodeFormat(s.episodeNameFormat ?? '')
        setMovieFolder(s.movieFolderFormat ?? '')
        setSeriesFolder(s.seriesFolderFormat ?? '')
        setKeepOriginalNames(s.keepOriginalNames === true)
        setIllegalCharMode(s.illegalCharMode || 'strip')
        setIllegalCharReplacement(s.illegalCharReplacement || '-')
        setImportConflictPolicy(s.importConflictPolicy || 'skip')
        setCheckDuration(s.checkDuration !== false)
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : String(e)))
  }, [setSettings])
  useEffect(load, [load])

  async function save() {
    if (!v.attempt()) return
    setSaving(true)
    try {
      const updated = await api.putSettings(
        part === 'folders'
          ? {
              moviesPath,
              tvPath,
              movieCopiesPath: movieCopiesPath.trim(),
              tvCopiesPath: tvCopiesPath.trim(),
              musicPath: musicPath.trim(),
              ebooksPath: ebooksPath.trim() || undefined,
              audiobooksPath: audiobooksPath.trim() || undefined,
              otherPath: otherPath.trim() || undefined,
              booksFolderLayout: ebooksOn || audiobooksOn ? booksLayout : undefined,
              downloadsPath,
            }
          : {
              namingPreset,
              movieNameFormat: customFormat,
              episodeNameFormat: episodeFormat,
              movieFolderFormat: movieFolder,
              seriesFolderFormat: seriesFolder,
              keepOriginalNames,
              illegalCharMode,
              illegalCharReplacement,
              importConflictPolicy,
              checkDuration,
            },
      )
      setSettings(updated)
      toast.success(t('Saved.'))
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
        <button onClick={load}>{t('Try again')}</button>
      </section>
    ) : null
  }

  const saveRow = (
    <div className="span-all" style={{ marginBottom: 6 }}>
      <button className="primary" onClick={save} disabled={saving}>
        {t('Save')}
      </button>
      <FormProblem v={v} />
    </div>
  )

  if (part === 'folders') {
    return (
    <>
      <fieldset className="group folders span-all">
        <legend>
          <Icon name="folder" size={14} /> <TipLabel text={t('Keep downloads and libraries inside one shared parent folder, such as /data. Finished files are then hardlinked: instant, with no extra disk space.')} learnMore="install-docker#why-one-data-folder-hardlinks">{t('Library folders')}</TipLabel>
        </legend>
        <div className="path-cols">
          <div className="path-col">
            <label>
              {t('Movies folder')}
              <input value={moviesPath} onChange={(e) => setMoviesPath(e.target.value)} {...v.bind('moviesPath', moviesPath, setMoviesPath)} />
              <FieldError v={v} name="moviesPath" />
            </label>
            <FolderStatus path={moviesPath} />
          </div>
          <div className="path-col">
            <label>
              {t('TV folder')}
              <input value={tvPath} onChange={(e) => setTvPath(e.target.value)} {...v.bind('tvPath', tvPath, setTvPath)} />
              <FieldError v={v} name="tvPath" />
            </label>
            <FolderStatus path={tvPath} />
          </div>
          <div className={`path-col${musicOn ? '' : ' path-off'}`}>
            <label>
              {t('Music folder')}
              <input value={musicPath} onChange={(e) => setMusicPath(e.target.value)} placeholder="/music" spellCheck={false} disabled={!musicOn} {...v.bind('musicPath', musicPath, setMusicPath)} />
              <FieldError v={v} name="musicPath" />
            </label>
            {musicOn ? (
              <>
                <FolderStatus path={musicPath} />
                <div>
                  <button className="btn-sm btn-with-icon" onClick={() => navigate('/music/import')}>
                    <Icon name="folder" size={15} /> {t('Import my existing collection')}
                  </button>
                </div>
              </>
            ) : (
              <small className="path-note">{t('Turn on Music in Media types first.')}</small>
            )}
          </div>
          <div className={`path-col${ebooksOn ? '' : ' path-off'}`}>
            <label>
              {t('Ebooks folder')}
              <input value={ebooksPath} onChange={(e) => setEbooksPath(e.target.value)} placeholder="/books" spellCheck={false} disabled={!ebooksOn} {...v.bind('ebooksPath', ebooksPath, setEbooksPath)} />
              <FieldError v={v} name="ebooksPath" />
            </label>
            {ebooksOn ? <FolderStatus path={ebooksPath} suggest={suggestFor('ebooks')} onUse={setEbooksPath} /> : <small className="path-note">{t('Turn on Ebooks in Media types first.')}</small>}
          </div>
          <div className={`path-col${audiobooksOn ? '' : ' path-off'}`}>
            <label>
              {t('Audiobooks folder')}
              <input value={audiobooksPath} onChange={(e) => setAudiobooksPath(e.target.value)} placeholder="/audiobooks" spellCheck={false} disabled={!audiobooksOn} {...v.bind('audiobooksPath', audiobooksPath, setAudiobooksPath)} />
              <FieldError v={v} name="audiobooksPath" />
            </label>
            {audiobooksOn ? <FolderStatus path={audiobooksPath} suggest={suggestFor('audiobooks')} onUse={setAudiobooksPath} /> : <small className="path-note">{t('Turn on Audiobooks in Media types first.')}</small>}
          </div>
          <div className={`path-col${otherOn ? '' : ' path-off'}`}>
            <label>
              {t('Other folder')}
              <input value={otherPath} onChange={(e) => setOtherPath(e.target.value)} placeholder="/other" spellCheck={false} disabled={!otherOn} {...v.bind('otherPath', otherPath, setOtherPath)} />
              <FieldError v={v} name="otherPath" />
            </label>
            {otherOn ? <FolderStatus path={otherPath} suggest={suggestFor('other')} onUse={setOtherPath} /> : <small className="path-note">{t('Turn on Other in Media types first.')}</small>}
          </div>
          <div className="path-col">
            <label>
              <TipLabel text={t('Where downloads are kept until they are imported. Put it next to your libraries so imports are hardlinks, not slow copies.')}>{t('Downloads folder')}</TipLabel>
              <input aria-label={t('Downloads folder')} value={downloadsPath} onChange={(e) => setDownloadsPath(e.target.value)} {...v.bind('downloadsPath', downloadsPath, setDownloadsPath)} />
              <FieldError v={v} name="downloadsPath" />
            </label>
            <FolderStatus path={downloadsPath} />
          </div>
        </div>
        {(ebooksOn || audiobooksOn) && (
          <div className="grid-form">
            <label>
              {t('How new books are filed')}
              <select value={booksLayout} onChange={(e) => setBooksLayout(e.target.value)}>
                <option value="author_title">{t('Author, then book')}</option>
                <option value="author_series">{t('Author, then series, then book')}</option>
              </select>
            </label>
            <small style={{ color: 'var(--text-dim)', textWrap: 'balance' }}>
              {booksLayout === 'author_series'
                ? t('New ebooks go in Author/Series/01. Title (Year).epub, new audiobooks in Author/Series/Title (Year)/, and a book with no series straight under its author. Books you already have stay where they are.')
                : t('New books go in Author/Title (Year)/. Choose Author, then series, then book to group them by series. Books you already have stay where they are.')}
            </small>
          </div>
        )}
        <HardlinkWarning
          libraries={[
            { label: t('movies'), path: moviesPath },
            { label: t('TV shows'), path: tvPath },
            ...(musicOn ? [{ label: t('music'), path: musicPath }] : []),
            ...(ebooksOn ? [{ label: t('ebooks'), path: ebooksPath }] : []),
            ...(audiobooksOn ? [{ label: t('audiobooks'), path: audiobooksPath }] : []),
            ...(otherOn ? [{ label: t('other media'), path: otherPath }] : []),
          ]}
          downloads={downloadsPath}
        />
      </fieldset>

      <fieldset className="group folders span-all">
        <legend>
          <Icon name="folder" size={14} /> {t('More folders for other titles (optional)')}
        </legend>
        <p style={{ marginTop: 0, color: 'var(--text-dim)' }}>
          {t('A place for different titles, such as a kids folder or a second drive. Each movie or show is kept in one folder: the one you pick when you add it (you can change it on its page). Add the folders to Plex, Jellyfin or Emby as libraries.')}
        </p>
        <div className="path-cols">
          <ExtraFolders kind="movies" />
          <ExtraFolders kind="tv" />
        </div>
      </fieldset>

      <fieldset className="group folders span-all">
        <legend>
          <Icon name="folder" size={14} /> {t('Second copies of the same title (optional)')}
        </legend>
        <p style={{ marginTop: 0, color: 'var(--text-dim)' }}>
          {t('The same title kept twice, in two qualities, such as a 4K copy for the big screen next to the 1080p one. The copies go in a folder of their own, so they never get mixed up with the main files. Add that folder to Plex, Jellyfin or Emby as its own library. Leave these empty to keep one copy of everything.')}
        </p>
        <div className="path-cols">
          <div className="path-col">
            <label>
              {t('Folder for second copies of movies')}
              <input value={movieCopiesPath} onChange={(e) => setMovieCopiesPath(e.target.value)} placeholder="/movies-4k" spellCheck={false} {...v.bind('movieCopiesPath', movieCopiesPath, setMovieCopiesPath)} />
              <FieldError v={v} name="movieCopiesPath" />
            </label>
            {movieCopiesPath.trim() && <FolderStatus path={movieCopiesPath} />}
          </div>
          <div className="path-col">
            <label>
              {t('Folder for second copies of shows')}
              <input value={tvCopiesPath} onChange={(e) => setTvCopiesPath(e.target.value)} placeholder="/tv-4k" spellCheck={false} {...v.bind('tvCopiesPath', tvCopiesPath, setTvCopiesPath)} />
              <FieldError v={v} name="tvCopiesPath" />
            </label>
            {tvCopiesPath.trim() && <FolderStatus path={tvCopiesPath} />}
          </div>
        </div>
        <small className="path-note">{t('For example /movies-4k and /tv-4k. Each title can then keep a second copy there, which you choose on its page.')}</small>
      </fieldset>
      {saveRow}
    </>
    )
  }

  return (
    <>
      <fieldset className="group span-all">
        <legend>
          <Icon name="list" size={14} /> {t('Naming and importing')}
        </legend>
        <div className="triple-cols">
          <div className="grid-form">
            <label>
              <TipLabel text={t('How imported files are named and filed, to suit your media player. Files already in the library keep their names until you rename them.')} learnMore="library#file-names">{t('Naming preset')}</TipLabel>
              <select value={namingPreset} onChange={(e) => setNamingPreset(e.target.value)}>
                <option value="plex">Plex</option>
                <option value="jellyfin">Jellyfin / Emby</option>
                <option value="kodi">Kodi</option>
                <option value="minimal">{t('Simple')}</option>
                <option value="custom">{t('Custom')}</option>
              </select>
            </label>
            {namingPreset === 'custom' && !keepOriginalNames ? (
              <small style={{ color: 'var(--text-dim)' }}>{t('Build your own movie and episode file names below.')}</small>
            ) : (
              <>
                <NamingPreview preset={namingPreset} keep={keepOriginalNames} />
                <NamingPreview preset={namingPreset} kind="tv" keep={keepOriginalNames} />
              </>
            )}
            <label className="check-row">
              <input type="checkbox" checked={keepOriginalNames} onChange={(e) => setKeepOriginalNames(e.target.checked)} />
              <span>{t('Keep the original file names')}</span>
            </label>
            <small style={{ color: 'var(--text-dim)' }}>
              {t('Files keep the name they came with. Folders are still named after the movie or show, so media servers find them. A file with a random name, like a8f7d6e5c4b3.mkv, takes the name of the release instead. If that name is random too, the naming preset is used.')}
            </small>
          </div>
          <div className="grid-form">
            <label>
              <TipLabel text={t('Some systems refuse characters like : ? and * in file names. Remove drops them from titles; Replace puts the character you choose in their place.')}>{t('Illegal characters in names')}</TipLabel>
              <select value={illegalCharMode} onChange={(e) => setIllegalCharMode(e.target.value)}>
                <option value="strip">{t('Remove them')}</option>
                <option value="replace">{t('Replace them')}</option>
              </select>
            </label>
            {illegalCharMode === 'replace' && (
              <label>
                {t('Replace with')}
                <input value={illegalCharReplacement} onChange={(e) => setIllegalCharReplacement(e.target.value)} maxLength={3} {...v.bind('illegalCharReplacement', illegalCharReplacement, setIllegalCharReplacement)} />
                <FieldError v={v} name="illegalCharReplacement" />
              </label>
            )}
            <small style={{ color: 'var(--text-dim)' }}>{t("Characters like : ? * aren't allowed in file names on some systems.")}</small>
          </div>
          <div className="grid-form">
            <label>
              <TipLabel text={t('What to do when an import would replace a file already in your library. Ask me parks a movie download in Activity until you decide. A show or a season pack is never parked: Mediarium keeps the file you have, like Skip.')}>{t('If a file already exists')}</TipLabel>
              <select value={importConflictPolicy} onChange={(e) => setImportConflictPolicy(e.target.value)}>
                <option value="skip">{t('Skip (leave the existing file alone)')}</option>
                <option value="overwrite">{t('Always overwrite')}</option>
                <option value="overwrite_if_better">{t('Overwrite only if the new file is better')}</option>
                <option value="ask">{t('Ask me each time (movies only, in Activity)')}</option>
              </select>
            </label>
            <small style={{ color: 'var(--text-dim)' }}>{t('What to do when a download would replace a file already in your library. With Skip, a title that looks for better versions still takes a better file in place of the old one. Ask me only waits for you with movies; for shows and season packs it keeps the file you have, like Skip.')}</small>
          </div>
          <div className="grid-form">
            <label className="check-row">
              <input type="checkbox" checked={checkDuration} onChange={(e) => setCheckDuration(e.target.checked)} />
              <span>{t('Check that the video is as long as the episode or movie')}</span>
            </label>
            <small style={{ color: 'var(--text-dim)' }}>{t('Before a download is imported, Mediarium reads how long the video is. If it is under half of the episode or movie length, it is a fake or cut off, so it is not imported, the release is blocklisted and another one is tried.')}</small>
          </div>
        </div>
        {namingPreset === 'custom' && <NamingBuilder
            movieFormat={customFormat}
            setMovieFormat={setCustomFormat}
            episodeFormat={episodeFormat}
            setEpisodeFormat={setEpisodeFormat}
            movieFolder={movieFolder}
            setMovieFolder={setMovieFolder}
            seriesFolder={seriesFolder}
            setSeriesFolder={setSeriesFolder}
            v={v}
          />}
        <div style={{ marginTop: 14 }}>
          <button className="primary" onClick={save} disabled={saving}>
            {t('Save')}
          </button>
          <FormProblem v={v} />
        </div>
      </fieldset>
    </>
  )
}

// Settings > Library and files > Folders: where the files live.
export default function MediaSettings() {
  return (
    <div className="settings-stack">
      <LibrarySection part="folders" />
      <FilePermissionsCard />
    </div>
  )
}

// Settings > Library and files > File names and importing: how files are named and what
// happens after a download, including the script that can run afterwards.
export function NamingSettings() {
  const [keepNames, setKeepNames] = useState(false)
  const onSettings = useCallback((s: SettingsData) => setKeepNames(s.keepOriginalNames === true), [])
  return (
    <div className="settings-stack">
      <LibrarySection part="naming" onSettings={onSettings} />
      <MetadataFilesCard />
      <RenameCard keepOriginalNames={keepNames} />
      <ScriptCard />
    </div>
  )
}
