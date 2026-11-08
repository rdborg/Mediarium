import { useEffect, useState } from 'react'
import CopyBox from '../../components/CopyBox'
import { useToast } from '../../components/Toast'
import { api } from '../../api'
import { useModules } from '../../ModulesContext'
import { BASE_PATH } from '../../base'
import { DOCS_URL } from '../../docs'
import { t, tx } from '../../i18n'
import TipLabel from '../../components/TipLabel'
import Switch from '../../components/Switch'
import Loading from '../../components/Loading'
import McpAppsCard from '../../components/McpAppsCard'

// Apps made for Radarr, Sonarr and Lidarr (request apps, dashboards, phone
// apps) connect to Mediarium as if it were those apps: each has its own
// address, and they sign in with an administrator's API key.
const APPS = [
  { app: 'radarr', label: 'Radarr', what: t('Movies'), module: 'movies' },
  { app: 'sonarr', label: 'Sonarr', what: t('TV'), module: 'tv' },
  { app: 'lidarr', label: 'Lidarr', what: t('Music'), module: 'music' },
] as const

export default function OtherAppsSettings() {
  const toast = useToast()
  const { on } = useModules()
  const [key, setKey] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const origin = window.location.origin
  const url = new URL(origin)
  const port = url.port || (url.protocol === 'https:' ? '443' : '80')

  async function makeKey() {
    setBusy(true)
    try {
      const created = await api.createAPIKey('Other apps')
      setKey(created.key ?? null)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const apps = APPS.filter((a) => on(a.module))

  // The MCP address for AI assistants: off until an administrator switches it on.
  const [mcp, setMcp] = useState<boolean | null>(null)
  useEffect(() => {
    api.mcpSettings().then((m) => setMcp(m.enabled)).catch(() => setMcp(false))
  }, [])

  async function switchMcp(next: boolean) {
    try {
      const m = await api.putMcpSettings(next)
      setMcp(m.enabled)
      toast.success(m.enabled ? t('AI assistants can connect now.') : t('AI assistants can no longer connect.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  // The SABnzbd and qBittorrent look-alike addresses for phone apps: off until an administrator switches them on.
  const [dlApi, setDlApi] = useState<boolean | null>(null)
  useEffect(() => {
    api.downloaderApiSettings().then((m) => setDlApi(m.enabled)).catch(() => setDlApi(false))
  }, [])

  async function switchDlApi(next: boolean) {
    try {
      const m = await api.putDownloaderApiSettings(next)
      setDlApi(m.enabled)
      toast.success(m.enabled ? t('Phone apps can now see your downloads.') : t('Phone apps can no longer see your downloads.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="settings-stack">
      <section className="card wide">
        <h2>{t('Connect apps made for Radarr, Sonarr and Lidarr')}</h2>
        <p style={{ color: 'var(--text-dim)' }}>
          {t('Request apps such as Overseerr and Jellyseerr, dashboards such as Homepage and Homarr, and phone apps such as nzb360, LunaSea and Ruddarr can talk to Mediarium as if it were Radarr, Sonarr or Lidarr. Add Mediarium in them as that app, with the address and URL base below.')}
        </p>
        <div className="other-apps">
          {apps.map((a) => (
            <fieldset key={a.app} className="group">
              <legend>
                {t('{what}: add it as {app}', { what: a.what, app: a.label })}
              </legend>
              <dl className="other-app-fields">
                <dt>{t('Full address')}</dt>
                <dd>
                  <CopyBox text={`${origin}${BASE_PATH}/${a.app}`} />
                </dd>
                <dt><TipLabel text={t('The URL base is the part after the port, such as /radarr. Many apps have a separate box for it. Use it exactly as shown.')} learnMore="install-docker#running-under-a-sub-path-url-base">{t('Or, when the app asks for each part')}</TipLabel></dt>
                <dd>
                  {url.protocol === 'https:'
                    ? tx('Host {host}, port {port}, URL base {base}, SSL on', { host: <code>{url.hostname}</code>, port: <code>{port}</code>, base: <code>{BASE_PATH}/{a.app}</code> })
                    : tx('Host {host}, port {port}, URL base {base}', { host: <code>{url.hostname}</code>, port: <code>{port}</code>, base: <code>{BASE_PATH}/{a.app}</code> })}
                </dd>
              </dl>
            </fieldset>
          ))}
        </div>
      </section>

      <section className="card wide">
        <h2>{t('Phone apps for downloads (nzb360 and others)')}</h2>
        <p style={{ color: 'var(--text-dim)' }}>
          {t("Apps such as nzb360 can show the speed, the time left and the list of your downloads, and pause, resume or remove them, when Mediarium answers like SABnzbd (Usenet) and qBittorrent (torrents). Add Mediarium in the app as that program, with the addresses below.")}
        </p>
        {dlApi === null ? (
          <Loading height={60} />
        ) : (
          <>
            <Switch
              checked={dlApi}
              onChange={(v) => void switchDlApi(v)}
              label={t('Let phone apps see my downloads')}
              showState
              description={t("It only works with an administrator's API key. You can switch it off here at any time.")}
            />
            {dlApi && (
              <dl className="other-app-fields">
                <dt>{t('Usenet: add it as SABnzbd')}</dt>
                <dd>
                  <CopyBox text={`${origin}${BASE_PATH}/sabnzbd`} />
                </dd>
                <dt>{t('Torrents: add it as qBittorrent')}</dt>
                <dd>
                  <CopyBox text={`${origin}${BASE_PATH}/qbittorrent`} />
                </dd>
                <dt>{t('The key goes in')}</dt>
                <dd>
                  {t("SABnzbd: the API key box. qBittorrent: the password box (the user name can be anything). Make the key in the box further down.")}
                </dd>
                <dt>{t('Good to know')}</dt>
                <dd>
                  {t("Adding a release from the app only works when its name matches a movie or show in your library. Priority, renaming and speed limits are not available.")}
                </dd>
              </dl>
            )}
            <p>
              <a href={`${DOCS_URL}/download-apps.md`} target="_blank" rel="noreferrer">
                {t('How to set up nzb360 and similar apps')}
              </a>
            </p>
          </>
        )}
      </section>

      <section className="card wide">
        <h2>{t('Let AI assistants connect (MCP)')}</h2>
        <p style={{ color: 'var(--text-dim)' }}>
          {t("Chat programs and voice assistants that support MCP can then ask about your library, and, once you say yes, change a few things. They sign in with an administrator's API key.")}
        </p>
        {mcp === null ? (
          <Loading height={60} />
        ) : (
          <>
            <Switch
              checked={mcp}
              onChange={(v) => void switchMcp(v)}
              label={t('Let AI assistants connect (MCP)')}
              showState
              description={t("It only works with an administrator's API key. You can switch it off here at any time.")}
            />
            {mcp && (
              <dl className="other-app-fields">
                <dt>{t('MCP address')}</dt>
                <dd>
                  <CopyBox text={`${origin}${BASE_PATH}/mcp`} />
                </dd>
                <dt>{t('What it can do')}</dt>
                <dd>
                  {t("Questions are answered straight away. Adding titles or changing a profile is shown first and only happens after you confirm in the chat. A read-only key can only ask questions.")}
                </dd>
              </dl>
            )}
            {mcp && <McpAppsCard />}
            <p>
              <a href={`${DOCS_URL}/mcp.md`} target="_blank" rel="noreferrer">
                {t('How to connect your assistant')}
              </a>
            </p>
          </>
        )}
      </section>

      <section className="card wide">
        <h2>{t('The API key')}</h2>
        <p style={{ color: 'var(--text-dim)' }}>
          {t("The other app signs in with an API key of an administrator. Make one here, or under Accounts > API keys, and paste it into the app's API key field. It's shown only once.")}
        </p>
        {key ? (
          <div className="newkey">
            <CopyBox text={key} />
            <span style={{ color: 'var(--warning)', fontFamily: 'var(--font-body)' }}>{t("Copy it now. You won't see it again.")}</span>
          </div>
        ) : (
          <button className="primary" disabled={busy} onClick={() => void makeKey()}>
            {busy ? t('Making a key…') : t('Make a key for other apps')}
          </button>
        )}
      </section>

      <section className="card wide">
        <h2>{t('Good to know')}</h2>
        <ul className="other-app-tips">
          <li>
            {tx('{apps} pick any quality profile and root folder they offer. Mediarium uses the profile and decides the folder itself. For Sonarr, choose English as the language profile when asked.', {
              apps: <strong>{t('Overseerr and Jellyseerr:')}</strong>,
            })}
          </li>
          <li>
            {tx('{requests}, with the seasons that were asked for. Searching starts when the app asks for it, the same as Search now.', {
              requests: <strong>{t('Requests become titles in your library')}</strong>,
            })}
          </li>
          <li>
            {tx('{tags} the app adds (Overseerr can tag by who asked) show up as Mediarium tags on the title.', { tags: <strong>{t('Tags')}</strong> })}
          </li>
          <li>
            {t('Removing a title from the app removes it from Mediarium. Files go to the recycle bin, so they can be brought back for a few days.')}
          </li>
          <li>
            {tx('Inside Docker, the other app reaches Mediarium by its container name or the NAS address, not {localhost}.', { localhost: <code>localhost</code> })}
          </li>
        </ul>
        <p>
          <a href={`${DOCS_URL}/other-apps.md`} target="_blank" rel="noreferrer">
            {t('How to set up each app')}
          </a>
        </p>
      </section>
    </div>
  )
}
