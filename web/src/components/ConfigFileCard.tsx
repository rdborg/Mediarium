import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type ConfigApplied, type ConfigChange, type ConfigReview, type ConfigStart } from '../api'
import { withBase } from '../base'
import { timeAgo } from '../format'
import { st, t } from '../i18n'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import Switch from './Switch'
import TipLabel from './TipLabel'
import { useToast } from './Toast'

// Settings > System > Settings file: everything that is not a secret as one
// file, and a file put back with a check first. Checking writes nothing; it
// shows what would be added, changed or removed. Applying saves a copy of the
// database and is all or nothing.

// What a block or list of the file is called on the Settings pages.
function kindLabel(key: string): string {
  switch (key) {
    case 'settings':
      return t('Settings')
    case 'downloadClient':
      return t('Torrent client')
    case 'debrid':
      return t('Debrid service')
    case 'proxy':
      return t('Proxy')
    case 'systemOptions':
      return t('Update options')
    case 'indexers':
      return t('Indexer')
    case 'usenetServers':
      return t('Usenet server')
    case 'mediaServers':
      return t('Media server')
    case 'qualityProfiles':
      return t('Quality profile')
    case 'notifications':
      return t('Notification')
  }
  return key
}

// "usenetServers[Main]" is "Usenet server: Main".
function sectionLabel(name: string): string {
  const m = /^(\w+)\[(.*)\]$/.exec(name)
  return m ? `${kindLabel(m[1])}: ${m[2]}` : kindLabel(name)
}

function changeLine(c: ConfigChange): string {
  if (c.new) return t('Will be added')
  if (c.removed) return t('Will be removed')
  const key = c.key ?? ''
  if (c.secret) return t('{key}: password or key will be set', { key })
  return `${key}: ${c.have ?? ''} → ${c.want ?? ''}`
}

const STATUS_ORDER = ['added', 'changed', 'removed', 'unchanged'] as const

export default function ConfigFileCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const input = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [text, setText] = useState('')
  const [replace, setReplace] = useState(false)
  const [review, setReview] = useState<ConfigReview | null>(null)
  const [hash, setHash] = useState('')
  const [busy, setBusy] = useState<'check' | 'apply' | ''>('')
  const [outcome, setOutcome] = useState<ConfigApplied | null>(null)
  const [start, setStart] = useState<ConfigStart | null>(null)

  const loadStart = useCallback(() => {
    api
      .configFileStart()
      .then(setStart)
      .catch(() => undefined)
  }, [])
  useEffect(loadStart, [loadStart])

  function forget() {
    setReview(null)
    setOutcome(null)
    setHash('')
  }

  async function choose(f: File | null) {
    setFile(f)
    forget()
    setText(f ? await f.text() : '')
  }

  async function check(nextReplace = replace) {
    if (!file) return
    setBusy('check')
    setOutcome(null)
    try {
      const res = await api.configFileCheck(text, nextReplace)
      setReview(res.review)
      setHash(res.hash)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  async function apply() {
    if (!review) return
    const ok = await confirm({
      title: t('Apply this settings file?'),
      body: (
        <>
          <p>{t('Mediarium saves a copy of the database first.')}</p>
          <p>{t('If it refuses a value, everything already changed is put back.')}</p>
          {review.removed > 0 && <p>{t('{n} items are removed. A removed indexer or server comes back without its password.', { n: review.removed })}</p>}
        </>
      ),
      confirmLabel: t('Apply'),
      danger: review.removed > 0,
    })
    if (!ok) return
    setBusy('apply')
    try {
      const res = await api.configFileApply(text, hash, replace)
      setOutcome(res)
      if (res.applied) {
        toast.success(t('Applied the settings file: {n} changed.', { n: res.changes }))
        setReview(null)
        loadStart()
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  async function setAtStart(enabled: boolean) {
    try {
      setStart(await api.setConfigFileStart(enabled))
      toast.success(enabled ? t('The settings file is applied at start.') : t('The settings file is not applied at start.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <fieldset className="group folders cfg-card">
      <legend>
        <Icon name="list" size={14} /> <TipLabel help="backups-updates#settings-file-with-a-check">{t('Settings file')}</TipLabel>
      </legend>
      <p style={{ marginTop: 0 }}>{t('Download your setup as one file, or check a file to see what it would change before you apply it.')}</p>
      <p className="field-hint">{t('Passwords and keys are never in the file.')}</p>
      <div className="backup-actions">
        <a className="btn-link primary-look cfg-nowrap" href={withBase('/api/system/config-file')} download>
          <Icon name="download" size={16} /> {t('Download settings file')}
        </a>
      </div>

      <div className="restore-row" style={{ marginTop: 16 }}>
        <input ref={input} type="file" accept=".yaml,.yml,.json,text/yaml,application/json" aria-label={t('Settings file to check')} onChange={(e) => void choose(e.target.files?.[0] ?? null)} />
        <button className="btn-with-icon cfg-nowrap" onClick={() => void check()} disabled={!file || busy !== ''}>
          <Icon name="check" size={15} /> {busy === 'check' ? t('Checking…') : t('Check this file')}
        </button>
      </div>
      <div style={{ marginTop: 10 }}>
        <Switch
          checked={replace}
          onChange={(v) => {
            setReplace(v)
            if (review) void check(v)
          }}
          label={t('Replace what the file lists')}
          description={t('Also remove items from a list when they are not in the file.')}
        />
      </div>

      {review && (
        <div className="cfg-review" aria-live="polite">
          {review.problems.length > 0 ? (
            <div className="cfg-problems" role="alert">
              <strong className="cfg-nowrap">{t("This file can't be applied")}</strong>
              <ul>
                {review.problems.map((p) => (
                  <li key={p}>{st(p)}</li>
                ))}
              </ul>
            </div>
          ) : (
            <>
              <div className="cfg-chips">
                {STATUS_ORDER.map((s) => {
                  const n = review[s]
                  const label = s === 'added' ? t('Added') : s === 'changed' ? t('Changed') : s === 'removed' ? t('Removed') : t('Unchanged')
                  return (
                    <span key={s} className={`cfg-chip cfg-${s}${n === 0 ? ' zero' : ''}`}>
                      {label} <strong>{n}</strong>
                    </span>
                  )
                })}
              </div>
              {!review.canApply && <p className="field-hint">{t('Nothing to change: Mediarium already matches this file.')}</p>}
              <ul className="cfg-list">
                {STATUS_ORDER.flatMap((s) => review.sections.filter((x) => x.status === s)).map((sec) => (
                  <li key={sec.status + sec.name} className={`cfg-item cfg-${sec.status}`}>
                    <span className="cfg-name" title={sectionLabel(sec.name)}>
                      {sectionLabel(sec.name)}
                    </span>
                    {sec.status === 'unchanged' ? (
                      <small>{t('Already as the file says')}</small>
                    ) : (
                      (sec.changes ?? []).map((c, i) => (
                        <small key={i} title={changeLine(c)}>
                          {changeLine(c)}
                        </small>
                      ))
                    )}
                  </li>
                ))}
              </ul>
              {review.missingSecrets.length > 0 && (
                <div className="cfg-secrets">
                  <strong className="cfg-nowrap">{t('Passwords still to enter')}</strong>
                  <ul>
                    {review.missingSecrets.map((m) => (
                      <li key={m}>{m.replace(/^(\w+)\[(.*)\]\.(.*)$/, (_a, list: string, name: string, key: string) => `${kindLabel(list)}: ${name} (${key})`)}</li>
                    ))}
                  </ul>
                  <p className="field-hint">{t('Enter these on the Settings pages after applying.')}</p>
                </div>
              )}
              <div className="backup-actions" style={{ marginTop: 12 }}>
                <button className="primary btn-with-icon cfg-nowrap" onClick={() => void apply()} disabled={!review.canApply || busy !== ''}>
                  <Icon name="check" size={15} /> {busy === 'apply' ? t('Applying…') : t('Apply')}
                </button>
              </div>
            </>
          )}
        </div>
      )}

      {outcome && !outcome.applied && (
        <div className="cfg-problems" role="alert" style={{ marginTop: 12 }}>
          <strong className="cfg-nowrap">{t('Nothing was kept')}</strong>
          <p style={{ margin: '4px 0' }}>{outcome.error}</p>
          <p className="field-hint">{t('What was already changed has been put back.')}</p>
          {(outcome.notUndone ?? []).map((n) => (
            <p key={n} className="field-hint">
              {n}
            </p>
          ))}
          {outcome.copy && <p className="field-hint">{t('A copy of the database was saved as {name}.', { name: outcome.copy })}</p>}
        </div>
      )}
      {outcome?.applied && outcome.copy && <p className="field-hint">{t('A copy of the database was saved as {name}.', { name: outcome.copy })}</p>}

      {start && (
        <div style={{ marginTop: 18 }}>
          <Switch
            checked={start.enabled}
            onChange={(v) => void setAtStart(v)}
            label={t('Apply the settings file at start')}
            description={t('Applies mediarium-config.yaml from your config folder when it changed since last time. It only adds and changes.')}
            showState
          />
          <p className="field-hint" style={{ marginTop: 8 }}>
            {start.file ? t('Found {file} in {folder}.', { file: start.file, folder: start.folder }) : t('There is no settings file in {folder} yet.', { folder: start.folder })}
            {start.last && (
              <>
                {' '}
                {t('Last time ({when}): {message}', { when: timeAgo(start.last.at), message: st(start.last.message) })}
              </>
            )}
          </p>
        </div>
      )}
    </fieldset>
  )
}
