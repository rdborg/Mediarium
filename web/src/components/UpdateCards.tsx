import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type UpdateNotice, type UpdateState } from '../api'
import { DOCS_URL } from '../docs'
import { timeAgo } from '../format'
import { reloadWhenBack } from '../reloadWhenBack'
import { checkedText, installSteps, programSummary } from '../updateSteps'
import '../update.css'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import Loading from './Loading'
import Switch from './Switch'
import { useToast } from './Toast'

// ---- Shared bits ----

function useNotice() {
  const [notice, setNotice] = useState<UpdateNotice | null>(null)
  const load = useCallback(() => api.updateLatest().then(setNotice).catch(() => undefined), [])
  useEffect(() => {
    void load()
  }, [load])
  return { notice, setNotice, load }
}

// Only ever link to the project's own release pages.
function safeReleaseUrl(url: string | undefined): string | null {
  return url && url.startsWith('https://github.com/') ? url : null
}

// ---- The notice: what is new, and how to update ----

function UpdateBody({ notice, onNotice }: { notice: UpdateNotice; onNotice: (n: UpdateNotice) => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const latest = notice.latest
  const job = notice.job
  const waiting = useRef(false)

  // While the update runs, look at how it is going. Once it says it is
  // restarting, wait for Mediarium to come back with the new version.
  useEffect(() => {
    if (!job?.active || job.state === 'restarting') return
    const t = setInterval(() => {
      api.updateInstallStatus().then(onNotice).catch(() => undefined)
    }, 1000)
    return () => clearInterval(t)
  }, [job?.active, job?.state, onNotice])

  useEffect(() => {
    if (job?.state !== 'restarting' || waiting.current) return
    waiting.current = true
    void reloadWhenBack(job.version).then((back) => {
      if (!back) {
        waiting.current = false
        toast.info("Mediarium didn't come back. Start it again, then reload this page.")
      }
    })
  }, [job?.state, job?.version, toast])

  if (!latest) return null
  const link = safeReleaseUrl(latest.url)
  const steps = installSteps(notice.install)

  async function install() {
    const ok = await confirm({
      title: `Update to ${latest?.version}?`,
      body: (
        <p>
          Mediarium downloads the new version, checks it's signed by the Mediarium project and restarts. That takes about a minute, and downloads in progress stop and can be resumed afterwards. Your settings and library are kept.
        </p>
      ),
      confirmLabel: 'Update now',
    })
    if (!ok) return
    try {
      onNotice(await api.updateInstall())
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const working = !!job?.active
  return (
    <>
      <div className="update-head">
        <div>
          <h3>Version {latest.version} is available</h3>
          <p className="update-sub" style={{ margin: '2px 0 0' }}>
            You are running {notice.running}.{latest.prerelease ? ' This is a pre-release.' : ''}
          </p>
        </div>
        {link && (
          <a className="btn-link" href={link} target="_blank" rel="noopener noreferrer">
            <Icon name="external" size={15} /> Release page
          </a>
        )}
      </div>
      {latest.notes && (
        <pre className="update-notes" aria-label="What is new">
          {latest.notes}
          {latest.moreNotes ? '\n…' : ''}
        </pre>
      )}

      {working && (
        <div className="update-progress" role="status">
          <span className="spinner-dot" aria-hidden="true" /> {job?.message}
        </div>
      )}
      {job?.state === 'failed' && (
        <div className="update-box warn" role="alert">
          <p>
            <strong>{job.message}</strong> {job.error} Nothing was changed. Try again, or update the way you installed Mediarium (steps below).
          </p>
        </div>
      )}

      {notice.canInstall && !working && (
        <div className="update-actions">
          <button className="primary btn-with-icon" onClick={() => void install()}>
            <Icon name="download" size={15} /> Update now
          </button>
          <small style={{ color: 'var(--text-dim)' }}>Signed by the Mediarium project and checked before it is installed.</small>
        </div>
      )}
      {!notice.canInstall && notice.installNote && !working && <p className="update-sub">{notice.installNote}</p>}

      {!working && (
        <div className="update-steps">
          {steps.groups.length === 1 ? (
            <div className="update-box">
              <strong>{steps.groups[0].title}</strong>
              <ol>
                {steps.groups[0].steps.map((s) => (
                  <li key={s}>{s}</li>
                ))}
              </ol>
            </div>
          ) : (
            <>
              <strong>{notice.canInstall ? 'Or update the way you installed it' : 'How to update'}</strong>
              {steps.groups.map((g) => (
                <details key={g.title}>
                  <summary>{g.title}</summary>
                  <ol>
                    {g.steps.map((s) => (
                      <li key={s}>{s}</li>
                    ))}
                  </ol>
                </details>
              ))}
            </>
          )}
          {steps.notes.map((n) => (
            <p key={n} className="update-note">
              {n}
            </p>
          ))}
        </div>
      )}
    </>
  )
}

// ---- The dashboard card: only there when there is something new ----

const HIDDEN_KEY = 'mediarium-update-hidden'

function readHidden(): string {
  try {
    return localStorage.getItem(HIDDEN_KEY) ?? ''
  } catch {
    return ''
  }
}

export function DashboardUpdate() {
  const { notice, setNotice } = useNotice()
  const [hidden, setHidden] = useState(readHidden)
  if (!notice?.latest) return null
  const working = !!notice.job?.active
  if (!working && (!notice.available || hidden === notice.latest.version)) return null

  function hide() {
    if (!notice?.latest) return
    try {
      localStorage.setItem(HIDDEN_KEY, notice.latest.version)
    } catch {
      // Not remembered across visits; it still goes away for now.
    }
    setHidden(notice.latest.version)
  }

  return (
    <section className="card update-card" aria-label="A new version is available">
      <UpdateBody notice={notice} onNotice={setNotice} />
      {!working && (
        <div style={{ marginTop: 6 }}>
          <button type="button" className="link-button" onClick={hide}>
            Hide until the next version
          </button>
        </div>
      )}
    </section>
  )
}

// ---- Settings > System: the Updates card ----

export function UpdatesCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const { notice, setNotice, load } = useNotice()
  const [state, setState] = useState<UpdateState | null>(null)
  const [checking, setChecking] = useState(false)
  const [busy, setBusy] = useState(false)

  const loadState = useCallback(() => api.updateState().then(setState).catch(() => undefined), [])
  useEffect(() => {
    void loadState()
  }, [loadState])

  async function checkNow() {
    setChecking(true)
    try {
      setNotice(await api.updateCheck())
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setChecking(false)
    }
  }

  async function setOption(patch: Partial<UpdateState['options']>, said: string) {
    try {
      await api.putSystemOptions(patch)
      toast.success(said)
      void loadState()
      void load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function goBack() {
    if (!state?.pushed) return
    const restart = state.control.canRestart
    const ok = await confirm({
      title: 'Go back to the version in the image?',
      body: (
        <p>
          The installed update ({state.pushed.version}) is removed and Mediarium goes back to the version in its Docker image ({state.image || 'the image'}).
          {restart ? ' It restarts now.' : ' This happens the next time Mediarium starts.'} Your settings and library are kept.
        </p>
      ),
      confirmLabel: restart ? 'Remove and restart' : 'Remove',
      danger: true,
    })
    if (!ok) return
    setBusy(true)
    try {
      const r = await api.removeUpdate(restart)
      toast.success(r.restarting ? 'Removed. Restarting…' : 'Removed. The image version is used the next time Mediarium starts.')
      if (r.restarting) void reloadWhenBack(state.image || undefined)
      else void loadState()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function clearFailed() {
    setBusy(true)
    try {
      await api.removeUpdate(false)
      toast.success('Removed.')
      void loadState()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group usenet span-all">
      <legend>
        <Icon name="download" size={14} /> Updates
      </legend>
      {!state || !notice ? (
        <Loading height={90} />
      ) : (
        <>
          <p style={{ marginTop: 0 }}>{programSummary(state)}</p>
          <div className="update-actions" style={{ marginTop: 0 }}>
            <button className="btn-with-icon" onClick={() => void checkNow()} disabled={checking}>
              <Icon name="refresh" size={15} /> {checking ? 'Checking…' : 'Check now'}
            </button>
            <small style={{ color: 'var(--text-dim)' }}>{checkedText(notice, timeAgo)}</small>
          </div>

          {notice.available ? (
            <div className="update-box">
              <UpdateBody notice={notice} onNotice={setNotice} />
            </div>
          ) : (
            notice.checkedAt &&
            !notice.error && (
              <div className="update-box ok">
                <Icon name="check" size={15} /> You have the newest version.
              </div>
            )
          )}

          {state.pushed && (
            <div className="update-box">
              <p>
                <strong>An update is installed on top of the Docker image.</strong> Version {state.pushed.version}
                {state.pushed.running ? ' is running now' : ' starts the next time Mediarium restarts'}. The image has {state.image || 'an older version'}.
                {' '}If you recreate the container from a newer image, Mediarium uses whichever is newer.
              </p>
              <button className="danger-ghost btn-with-icon" onClick={() => void goBack()} disabled={busy}>
                <Icon name="refresh" size={15} /> Go back to the image's version
              </button>
            </div>
          )}
          {state.failed && !state.pushed && (
            <div className="update-box warn" role="alert">
              <p>
                <strong>An installed update was put aside.</strong> {state.failed.version || 'The update'} kept stopping right after it started (three times in a row), so Mediarium went back to the version in its Docker image.
              </p>
              <button className="btn-with-icon" onClick={() => void clearFailed()} disabled={busy}>
                <Icon name="trash" size={15} /> Remove it
              </button>
            </div>
          )}
          {state.selfRestart && (
            <div className="update-box warn" role="alert">
              <p>
                <strong>Mediarium restarted itself</strong> {timeAgo(state.selfRestart.at)} because it stopped answering.
              </p>
            </div>
          )}

          <div className="update-switches">
            <Switch
              checked={state.options.updateCheck}
              onChange={(v) => void setOption({ updateCheck: v }, v ? 'Daily update check is on.' : 'Daily update check is off.')}
              label="Look for new versions once a day"
              showState
              description={`Asks GitHub for the newest version and sends nothing but your version number (${state.running}).`}
            />
            <Switch
              checked={state.options.autoInstall}
              disabled={!state.canInstall}
              onChange={(v) => void setOption({ autoInstall: v }, v ? 'Overnight installs are on.' : 'Overnight installs are off.')}
              label="Install new versions overnight"
              showState
              description={
                state.canInstall
                  ? 'Between 2 and 5 in the morning, when nothing is downloading. Only versions signed by the Mediarium project are installed.'
                  : 'Only works in the Mediarium Docker image.'
              }
            />
          </div>
        </>
      )}
    </fieldset>
  )
}

// ---- Settings > System: updates pushed through the API ----

export function PushCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [state, setState] = useState<UpdateState | null>(null)
  const load = useCallback(() => api.updateState().then(setState).catch(() => undefined), [])
  useEffect(() => {
    void load()
  }, [load])

  async function change(next: boolean) {
    if (next) {
      const ok = await confirm({
        title: 'Allow updates pushed through the API?',
        body: (
          <>
            <p>
              While this is on, an administrator can replace the Mediarium program by sending a new program file with an API key. Anyone who gets an administrator's API key could run their own program on your server.
            </p>
            <p>Switch it on only when you need it, and off again afterwards.</p>
          </>
        ),
        confirmLabel: 'Allow it',
        danger: true,
      })
      if (!ok) return
    }
    try {
      await api.putSystemOptions({ allowPush: next })
      toast.success(next ? 'Pushed updates are allowed.' : 'Pushed updates are switched off.')
      void load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <fieldset className="group alerts">
      <legend>
        <Icon name="shield" size={14} /> Updates pushed through the API
      </legend>
      {!state ? (
        <Loading height={70} />
      ) : (
        <>
          <Switch
            checked={state.options.allowPush}
            disabled={!state.canInstall}
            onChange={(v) => void change(v)}
            label="Allow updates pushed through the API"
            showState
            description={
              state.canInstall
                ? "For testing, and for people who can't rebuild their container."
                : 'Only works in the Mediarium Docker image.'
            }
          />
          <div className="update-box warn">
            <p style={{ margin: 0 }}>
              <strong>Be careful.</strong> With this on, an administrator, or a script with an administrator's API key, can replace the Mediarium program. It can only be switched on here while you&apos;re signed in, never with an API key.
            </p>
          </div>
          {state.options.allowPush && (
            <p style={{ color: 'var(--text-dim)', marginBottom: 0 }}>
              To push an update, send the program file to <code>POST /api/system/update</code> with its SHA-256 in the <code>X-Update-SHA256</code> header. Mediarium checks the file, installs it and restarts. See{' '}
              <a href={`${DOCS_URL}/INSTALL.md`} target="_blank" rel="noopener noreferrer">
                the install guide
              </a>
              .
            </p>
          )}
        </>
      )}
    </fieldset>
  )
}

// ---- Settings > System: restart ----

export function RestartCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [state, setState] = useState<UpdateState | null>(null)
  const [waiting, setWaiting] = useState(false)
  const load = useCallback(() => api.updateState().then(setState).catch(() => undefined), [])
  useEffect(() => {
    void load()
  }, [load])

  async function restart(safe: boolean) {
    const ok = await confirm(
      safe
        ? {
            title: 'Restart with automation paused?',
            body: (
              <p>
                Restarts once with automatic searching, downloading and refreshing paused, so you can fix a setting. The next restart turns everything back on. Downloads in progress stop and can be resumed afterwards.
              </p>
            ),
            confirmLabel: 'Restart paused',
          }
        : {
            title: 'Restart Mediarium?',
            body: <p>Mediarium is unavailable for about half a minute. Downloads in progress stop and can be resumed afterwards.</p>,
            confirmLabel: 'Restart',
          },
    )
    if (!ok) return
    try {
      await api.restartApp(safe)
      toast.success('Restarting…')
      setWaiting(true)
      const back = await reloadWhenBack()
      if (!back) {
        setWaiting(false)
        toast.info("Mediarium didn't come back. Start it again, then reload this page.")
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function shutDown() {
    const ok = await confirm({
      title: 'Shut down Mediarium?',
      body: <p>Mediarium stops, and nothing starts it again. To use it again, start it yourself.</p>,
      confirmLabel: 'Shut down',
      danger: true,
    })
    if (!ok) return
    try {
      await api.shutdownApp()
      toast.success('Mediarium is shutting down.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function setStuck(v: boolean) {
    try {
      await api.putSystemOptions({ autoRestartWhenStuck: v })
      toast.success(v ? 'Restart if stuck is on.' : 'Restart if stuck is off.')
      void load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const c = state?.control
  return (
    <fieldset className="group torrent">
      <legend>
        <Icon name="refresh" size={14} /> Restart
      </legend>
      {!state || !c ? (
        <Loading height={70} />
      ) : (
        <>
          <p style={{ marginTop: 0 }}>{c.note}</p>
          <div className="control-actions">
            <button className="primary btn-with-icon" onClick={() => void restart(false)} disabled={!c.canRestart || waiting}>
              <Icon name="refresh" size={15} /> {waiting ? 'Restarting…' : 'Restart Mediarium'}
            </button>
            <button className="btn-with-icon" onClick={() => void restart(true)} disabled={!c.canRestart || waiting}>
              <Icon name="pause" size={15} /> Restart with automation paused
            </button>
            {c.canShutdown && (
              <button className="danger-ghost btn-with-icon" onClick={() => void shutDown()}>
                <Icon name="stop" size={15} /> Shut down
              </button>
            )}
          </div>
          {!c.canRestart && <p className="error-text" style={{ marginTop: 0 }}>Restart isn't available here. Start Mediarium again yourself.</p>}
          <Switch
            checked={state.options.autoRestartWhenStuck}
            onChange={(v) => void setStuck(v)}
            label="Restart by itself if it stops answering"
            showState
            description="After three minutes without an answer. Never during a restore or an update."
          />
        </>
      )}
    </fieldset>
  )
}
