import { cloneElement, isValidElement, useCallback, useEffect, useState, type ReactElement, type ReactNode } from 'react'
import { api, type LdapReport, type LdapSettings, type OidcSettings, type OidcTestResult, type SignInSettings } from '../api'
import { DOCS_URL } from '../docs'
import { t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'
import TipLabel from './TipLabel'

const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e))

function Field({ label, note, tip, children }: { label: string; note?: ReactNode; tip?: string; children: ReactNode }) {
  // The text field gets the label as its name too, so a screen reader never says just "edit".
  const named =
    isValidElement(children) && children.type === 'input' && !(children.props as { 'aria-label'?: string })['aria-label']
      ? cloneElement(children as ReactElement<{ 'aria-label'?: string }>, { 'aria-label': label })
      : children
  return (
    <label>
      {tip ? <TipLabel text={tip}>{label}</TipLabel> : label}
      {named}
      {note && <small style={{ color: 'var(--text-dim)' }}>{note}</small>}
    </label>
  )
}

function Check({ checked, onChange, children, disabled, tip }: { checked: boolean; onChange: (v: boolean) => void; children: ReactNode; disabled?: boolean; tip?: string }) {
  return (
    <label className="check-row">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} disabled={disabled} />
      <span>{tip ? <TipLabel text={tip}>{children}</TipLabel> : children}</span>
    </label>
  )
}

// Settings > People and access > All accounts: the ways to sign in besides the password
// of an account made here. Everything in this box is off until it is switched
// on, and none of it takes the password away unless an administrator who has
// already signed in another way says so.
export default function SignInMethods() {
  const [state, setState] = useState<SignInSettings | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    api
      .getSignIn()
      .then((s) => {
        setState(s)
        setError('')
      })
      .catch((e) => setError(errorText(e)))
  }, [])
  useEffect(load, [load])

  return (
    <>
      <GeneralCard state={state} error={error} onChange={setState} reload={load} />
      <OidcCard publicUrl={state?.publicUrl ?? ''} ssoDisabledByEnv={!!state?.ssoDisabledByEnv} onSaved={load} />
      <LdapCard ssoDisabledByEnv={!!state?.ssoDisabledByEnv} onSaved={load} />
    </>
  )
}

// Password sign-in and passkeys: the two switches.
function GeneralCard({ state, error, onChange, reload }: { state: SignInSettings | null; error: string; onChange: (s: SignInSettings) => void; reload: () => void }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState('')

  async function save(change: { localSignIn?: boolean; passkeys?: boolean; autoSignIn?: '' | 'oidc' }, done: string) {
    setBusy(true)
    setProblem('')
    try {
      onChange(await api.putSignIn(change))
      toast.success(done)
    } catch (e) {
      setProblem(errorText(e))
      reload()
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group">
      <legend>
        <Icon name="shield" size={14} /> {t('Ways to sign in')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Besides the username and password of an account made here, people can sign in with a single sign-on provider, with a directory account, or with a passkey. All of them are off until you switch them on below.')}{' '}
        <a href={`${DOCS_URL}/accounts.md#ways-to-sign-in`} target="_blank" rel="noreferrer">
          {t('How they work')}
        </a>
      </p>
      {error && <p className="error-text">{error}</p>}
      {state?.ssoDisabledByEnv && (
        <p className="hint" style={{ color: 'var(--warning)' }}>
          {t('MEDIARIUM_DISABLE_SSO is set on the container, so single sign-on and directory sign-in are off and password sign-in is on, whatever is saved here. Take the variable away and restart to use them again.')}
        </p>
      )}
      {state && (
        <div className="form-cols" style={{ gap: 18 }}>
          <div>
            <strong><TipLabel text={t('Switching it off leaves only the other ways to sign in. It only works after an admin has signed in another way, so you cannot lock yourself out.')} learnMore="accounts#turning-password-sign-in-off">{t('Password sign-in')}</TipLabel></strong>
            <p style={{ margin: '4px 0 8px', color: 'var(--text-dim)' }}>
              {state.localEffective
                ? state.localSignIn
                  ? t('On. Everyone with a password here can use the sign-in form.')
                  : t('Saved as off, but it is on now because no other way to sign in is working.')
                : t('Off. Only the other ways to sign in work. A password session that is already open keeps working.')}
            </p>
            {state.localSignIn ? (
              <>
                <button disabled={busy || !state.canTurnOffLocal} onClick={() => void save({ localSignIn: false }, t('Password sign-in is off.'))}>
                  {t('Turn off password sign-in')}
                </button>
                {!state.canTurnOffLocal && (
                  <small style={{ display: 'block', marginTop: 6, color: 'var(--text-dim)' }}>
                    {state.externalOn
                      ? t("To turn it off, sign in with single sign-on, the directory or a passkey first, as an admin. That way you know the other way works and can't lock yourself out.")
                      : t('Switch on another way to sign in first.')}
                  </small>
                )}
              </>
            ) : (
              <button className="primary" disabled={busy} onClick={() => void save({ localSignIn: true }, t('Password sign-in is on.'))}>
                {t('Turn on password sign-in')}
              </button>
            )}
          </div>
          <div>
            <strong><TipLabel text={t('A passkey signs you in with a fingerprint, face or device PIN instead of a password. It needs the address of Mediarium to start with https and use a name.')} learnMore="accounts#passkeys">{t('Passkeys')}</TipLabel></strong>
            <p style={{ margin: '4px 0 8px', color: 'var(--text-dim)' }}>
              {state.passkeys
                ? state.passkeysAvailable
                  ? t('On. People add passkeys on their profile page and sign in with them from the sign-in page.')
                  : t('Switched on, but waiting for the address you open Mediarium at.')
                : t('Off. A passkey is a fingerprint, face or device PIN that signs you in, with no password to type.')}
            </p>
            {!state.passkeysAvailable && (
              <p className="hint" style={{ margin: '0 0 8px' }}>
                {t('Passkeys need the address you open Mediarium at (Settings > Basics > General > Address of Mediarium), starting with https:// and with a name, not an IP address. http://localhost works too.')}
                {state.publicUrl && ' ' + t('Now it is {url}.', { url: state.publicUrl })}
              </p>
            )}
            {state.passkeys ? (
              <button disabled={busy} onClick={() => void save({ passkeys: false }, t('Passkeys are off.'))}>
                {t('Turn off passkeys')}
              </button>
            ) : (
              <button className="primary" disabled={busy || !state.passkeysAvailable} onClick={() => void save({ passkeys: true }, t('Passkeys are on.'))}>
                {t('Turn on passkeys')}
              </button>
            )}
          </div>
        </div>
      )}
      {state && (
        <div style={{ marginTop: 18 }}>
          <strong>
            <TipLabel text={t('Visitors who are not signed in are sent straight to the provider, and come back to the page they asked for. Add ?no-redirect=1 to the address to see the normal sign-in page. Password sign-in is not changed.')} learnMore="accounts#signing-in-automatically">
              {t('Sign in automatically with')}
            </TipLabel>
          </strong>
          <div style={{ margin: '8px 0 4px' }}>
            <select
              aria-label={t('Sign in automatically with')}
              value={state.autoSignIn}
              disabled={busy || (state.autoSignInChoices.length === 0 && state.autoSignIn === '')}
              onChange={(e) => void save({ autoSignIn: e.target.value as '' | 'oidc' }, e.target.value ? t('Visitors are now sent to {name} to sign in.', { name: state.autoSignInChoices.find((c) => c.id === e.target.value)?.name ?? '' }) : t('Visitors see the normal sign-in page.'))}
              style={{ width: 'auto', maxWidth: '100%' }}
            >
              <option value="">{t('Off')}</option>
              {state.autoSignInChoices.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
          <small style={{ color: 'var(--text-dim)' }}>
            {state.autoSignInChoices.length === 0
              ? t('Switch on single sign-on below to pick it here.')
              : t('To use the password form anyway, open the address with ?no-redirect=1.')}
          </small>
        </div>
      )}
      {problem && <p className="error-text">{problem}</p>}
    </fieldset>
  )
}

// ---- Single sign-on (OpenID Connect) ----

function OidcCard({ publicUrl, ssoDisabledByEnv, onSaved }: { publicUrl: string; ssoDisabledByEnv: boolean; onSaved: () => void }) {
  const toast = useToast()
  const [cfg, setCfg] = useState<OidcSettings | null>(null)
  const [secret, setSecret] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [test, setTest] = useState<OidcTestResult | null>(null)

  const load = useCallback(() => {
    api
      .getOidc()
      .then(setCfg)
      .catch((e) => setError(errorText(e)))
  }, [])
  useEffect(load, [load, publicUrl])

  if (!cfg) return error ? <p className="error-text">{error}</p> : null
  const set = <K extends keyof OidcSettings>(k: K, v: OidcSettings[K]) => setCfg({ ...cfg, [k]: v })
  const body = () => ({ ...cfg, clientSecret: secret })

  async function save() {
    setBusy(true)
    setError('')
    try {
      const saved = await api.putOidc(body())
      setCfg(saved)
      setSecret('')
      toast.success(saved.enabled ? t('Single sign-on is on.') : t('Saved. Single sign-on is off.'))
      onSaved()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function runTest() {
    setBusy(true)
    setError('')
    setTest(null)
    try {
      setTest(await api.testOidc(body()))
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group">
      <legend>
        <Icon name="key" size={14} /> {t('Single sign-on (OpenID Connect)')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Let people sign in with Authelia, Authentik, Keycloak, Pocket ID, Google or another OpenID Connect provider. Add Mediarium to the provider as a client first, with the redirect address shown below.')}{' '}
        <a href={`${DOCS_URL}/accounts.md#single-sign-on`} target="_blank" rel="noreferrer">
          {t('How to set it up')}
        </a>
      </p>
      {ssoDisabledByEnv && <p className="hint">{t('Switched off by MEDIARIUM_DISABLE_SSO on the container.')}</p>}
      {!cfg.publicUrlSet && <p className="hint">{t('Set the address you open Mediarium at (Settings > Basics > General > Address of Mediarium) first, for example https://mediarium.example.com. The provider needs it to send people back.')}</p>}
      <div className="grid-form" style={{ gap: 14 }}>
        <Check checked={cfg.enabled} onChange={(v) => set('enabled', v)}>
          {t('Turn on single sign-on')}
        </Check>
        <div className="form-cols">
          <Field label={t('Name on the button')} note={t('"Sign in with Authentik"')}>
            <input value={cfg.name} onChange={(e) => set('name', e.target.value)} placeholder="Authentik" maxLength={40} />
          </Field>
          <Field label={t("Provider's address (issuer)")} note={t('For example https://auth.example.com. Mediarium reads the rest from it.')}>
            <input value={cfg.issuer} onChange={(e) => set('issuer', e.target.value)} placeholder="https://auth.example.com" spellCheck={false} />
          </Field>
          <Field label={t('Client ID')} tip={t('From your provider, after you add Mediarium to it as a client. The client secret next to it is the matching password.')}>
            <input value={cfg.clientId} onChange={(e) => set('clientId', e.target.value)} spellCheck={false} autoComplete="off" />
          </Field>
          <Field label={t('Client secret')} note={cfg.hasClientSecret && !secret ? t('Saved. Leave empty to keep it. Type one if you change the provider address.') : undefined}>
            <input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} placeholder={cfg.hasClientSecret ? '••••••••' : ''} autoComplete="new-password" />
          </Field>
        </div>
        <div>
          <span className="field-title">{t('Redirect address to enter at the provider')}</span>
          <code className="forgot-cmd" style={{ display: 'block', marginTop: 4 }}>
            {cfg.redirectUrl || t('(set the address you open Mediarium at first)')}
          </code>
        </div>
        <Field label={t('Scopes')} note={t('Add "groups" if you use a group below.')}>
          <input value={cfg.scopes} onChange={(e) => set('scopes', e.target.value)} placeholder="openid profile email groups" spellCheck={false} />
        </Field>

        <strong>{t('Accounts')}</strong>
        <Check checked={cfg.autoCreate} onChange={(v) => set('autoCreate', v)} tip={t('Off: only people who already have an account here can sign in. On: anyone your provider or directory lets in gets an account.')}>
          {t('Make an account the first time someone signs in')}
        </Check>
        <div className="form-cols">
          <Field label={t('Role for new accounts')} tip={t('The role a new account gets. A Viewer can look and play but never change anything.')}>
            <select value={cfg.defaultRole} onChange={(e) => set('defaultRole', e.target.value as 'member' | 'viewer')}>
              <option value="member">{t('Basic user')}</option>
              <option value="viewer">{t('Viewer (can only look)')}</option>
            </select>
          </Field>
          <Field label={t('Administrator group')} note={t('People in this group are admins, checked at every sign-in. Empty: nobody becomes an admin this way.')}>
            <input value={cfg.adminGroup} onChange={(e) => set('adminGroup', e.target.value)} placeholder="mediarium-admins" spellCheck={false} />
          </Field>
          <Field label={t('Viewer group')} note={t('People in this group are viewers (they can only look), checked at every sign-in. Someone in the administrator group is an admin instead. Empty: nobody becomes a viewer this way.')}>
            <input value={cfg.viewerGroup} onChange={(e) => set('viewerGroup', e.target.value)} placeholder="mediarium-viewers" spellCheck={false} />
          </Field>
          <Field label={t('Only this group may sign in')} note={t('Optional. Empty: anyone the provider signs in.')}>
            <input value={cfg.allowedGroup} onChange={(e) => set('allowedGroup', e.target.value)} spellCheck={false} />
          </Field>
          <Field label={t('Only these email domains')} note={t('Optional, separated by commas, for example example.com')}>
            <input value={cfg.allowedDomains} onChange={(e) => set('allowedDomains', e.target.value)} spellCheck={false} />
          </Field>
        </div>
        <Check checked={cfg.linkByEmail} onChange={(v) => set('linkByEmail', v)}>
          {t("Link a first sign-in to an existing account with the same email address, when the provider says the address is verified")}
          <small style={{ display: 'block', color: 'var(--text-dim)' }}>
            {t("Only turn this on if the provider checks email addresses. Admin accounts are never linked this way. Each person can also link their own account from their profile.")}
          </small>
        </Check>

        <details>
          <summary>{t('Claim names (only if your provider uses other ones)')}</summary>
          <div className="form-cols" style={{ marginTop: 10 }}>
            <Field label={t('User name')}>
              <input value={cfg.usernameClaim} onChange={(e) => set('usernameClaim', e.target.value)} placeholder="preferred_username" spellCheck={false} />
            </Field>
            <Field label={t('Email')}>
              <input value={cfg.emailClaim} onChange={(e) => set('emailClaim', e.target.value)} placeholder="email" spellCheck={false} />
            </Field>
            <Field label={t('Full name')}>
              <input value={cfg.nameClaim} onChange={(e) => set('nameClaim', e.target.value)} placeholder="name" spellCheck={false} />
            </Field>
            <Field label={t('Groups')}>
              <input value={cfg.groupsClaim} onChange={(e) => set('groupsClaim', e.target.value)} placeholder="groups" spellCheck={false} />
            </Field>
          </div>
        </details>

        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
          <button className="primary" onClick={() => void save()} disabled={busy}>
            {t('Save')}
          </button>
          <button onClick={() => void runTest()} disabled={busy || !cfg.issuer.trim()}>
            {t('Test the provider')}
          </button>
        </div>
      </div>
      {error && <p className="error-text">{error}</p>}
      {test && (
        <div className="hint" role="status">
          <p style={{ margin: 0 }}>
            <strong>{test.summary}</strong>
          </p>
          <ul style={{ margin: '6px 0 0' }}>
            <li>
              {t('Sign-in page')}: <code>{test.authorizationEndpoint}</code>
            </li>
            <li>
              {t('Token address')}: <code>{test.tokenEndpoint}</code>
            </li>
            {test.notes.map((n) => (
              <li key={n} style={{ color: 'var(--warning)' }}>
                {n}
              </li>
            ))}
          </ul>
        </div>
      )}
    </fieldset>
  )
}

// ---- Directory (LDAP / Active Directory) ----

function LdapCard({ ssoDisabledByEnv, onSaved }: { ssoDisabledByEnv: boolean; onSaved: () => void }) {
  const toast = useToast()
  const [cfg, setCfg] = useState<LdapSettings | null>(null)
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [testUser, setTestUser] = useState('')
  const [testPassword, setTestPassword] = useState('')
  const [report, setReport] = useState<LdapReport | null>(null)

  useEffect(() => {
    api
      .getLdap()
      .then(setCfg)
      .catch((e) => setError(errorText(e)))
  }, [])

  if (!cfg) return error ? <p className="error-text">{error}</p> : null
  const set = <K extends keyof LdapSettings>(k: K, v: LdapSettings[K]) => setCfg({ ...cfg, [k]: v })
  const body = () => ({ ...cfg, bindPassword: password })
  const plain = cfg.url.trim().toLowerCase().startsWith('ldap://')

  async function save() {
    setBusy(true)
    setError('')
    try {
      const saved = await api.putLdap(body())
      setCfg(saved)
      setPassword('')
      toast.success(saved.enabled ? t('Directory sign-in is on.') : t('Saved. Directory sign-in is off.'))
      onSaved()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function runTest() {
    setBusy(true)
    setError('')
    setReport(null)
    try {
      setReport(await api.testLdap({ ...body(), testUsername: testUser.trim(), testPassword }))
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group">
      <legend>
        <Icon name="server" size={14} /> {t('Directory sign-in (LDAP or Active Directory)')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Let people sign in with the username and password of your directory (OpenLDAP, Active Directory, FreeIPA, LLDAP, GLAuth). Their password is checked by the directory each time and never stored here. They use the normal sign-in form.')}{' '}
        <a href={`${DOCS_URL}/accounts.md#directory-sign-in-ldap-and-active-directory`} target="_blank" rel="noreferrer">
          {t('How to set it up')}
        </a>
      </p>
      {ssoDisabledByEnv && <p className="hint">{t('Switched off by MEDIARIUM_DISABLE_SSO on the container.')}</p>}
      <div className="grid-form" style={{ gap: 14 }}>
        <Check checked={cfg.enabled} onChange={(v) => set('enabled', v)}>
          {t('Turn on directory sign-in')}
        </Check>
        <div className="form-cols">
          <Field label={t('Server address')} note={t('ldaps://ldap.example.com:636 is best. ldap:// needs StartTLS.')}>
            <input value={cfg.url} onChange={(e) => set('url', e.target.value)} placeholder="ldaps://ldap.example.com:636" spellCheck={false} />
          </Field>
          <Field label={t('Base DN (where people are)')} tip={t('The place in the directory tree where your people are listed, for example ou=people,dc=example,dc=com.')}>
            <input value={cfg.baseDn} onChange={(e) => set('baseDn', e.target.value)} placeholder="ou=people,dc=example,dc=com" spellCheck={false} />
          </Field>
          <Field label={t('Bind account (DN)')} note={t('The account that looks people up. Empty: search without signing in.')}>
            <input value={cfg.bindDn} onChange={(e) => set('bindDn', e.target.value)} placeholder="cn=mediarium,ou=service,dc=example,dc=com" spellCheck={false} autoComplete="off" />
          </Field>
          <Field label={t('Bind account password')} note={cfg.hasBindPassword && !password ? t('Saved. Leave empty to keep it. Type it again if you change the server address.') : undefined}>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder={cfg.hasBindPassword ? '••••••••' : ''} autoComplete="new-password" />
          </Field>
        </div>
        {plain && (
          <div>
            <Check checked={cfg.startTls} onChange={(v) => set('startTls', v)}>
              {t('Encrypt the connection with StartTLS')}
            </Check>
            {!cfg.startTls && (
              <Check checked={cfg.allowPlain} onChange={(v) => set('allowPlain', v)}>
                <span style={{ color: 'var(--warning)' }}>{t('Send passwords without encryption (not recommended)')}</span>
              </Check>
            )}
          </div>
        )}
        <Field label={t('User filter')} note={t('{username} stands for what the person typed. Add group or "enabled" conditions here to limit who may sign in.')}>
          <input value={cfg.userFilter} onChange={(e) => set('userFilter', e.target.value)} placeholder="(&(objectClass=person)(uid={username}))" spellCheck={false} />
        </Field>
        <div className="form-cols">
          <Field label={t('Administrator group filter')} note={t('Optional. {dn} stands for the person. People it finds are admins, checked at every sign-in.')}>
            <input value={cfg.adminGroupFilter} onChange={(e) => set('adminGroupFilter', e.target.value)} placeholder="(&(objectClass=groupOfNames)(cn=mediarium-admins)(member={dn}))" spellCheck={false} />
          </Field>
          <Field label={t('Viewer group filter')} note={t('Optional. Works like the administrator group filter. People it finds are viewers, checked at every sign-in.')}>
            <input value={cfg.viewerGroupFilter} onChange={(e) => set('viewerGroupFilter', e.target.value)} placeholder="(&(objectClass=groupOfNames)(cn=mediarium-viewers)(member={dn}))" spellCheck={false} />
          </Field>
          <Field label={t('Where to look for groups')} note={t('Empty: the base DN.')}>
            <input value={cfg.groupBaseDn} onChange={(e) => set('groupBaseDn', e.target.value)} spellCheck={false} />
          </Field>
        </div>
        <Check checked={cfg.autoCreate} onChange={(v) => set('autoCreate', v)} tip={t('Off: only people who already have an account here can sign in. On: anyone your provider or directory lets in gets an account.')}>
          {t('Make an account the first time someone signs in')}
        </Check>
        <Field label={t('Role for new accounts')} tip={t('The role a new account gets. A Viewer can look and play but never change anything.')}>
          <select value={cfg.defaultRole} onChange={(e) => set('defaultRole', e.target.value as 'member' | 'viewer')} style={{ maxWidth: 280 }}>
            <option value="member">{t('Basic user')}</option>
            <option value="viewer">{t('Viewer (can only look)')}</option>
          </select>
        </Field>

        <details>
          <summary>{t('Attribute names and the certificate (only if your directory needs other ones)')}</summary>
          <div className="form-cols" style={{ marginTop: 10 }}>
            <Field label={t('User name attribute')}>
              <input value={cfg.usernameAttr} onChange={(e) => set('usernameAttr', e.target.value)} placeholder="uid" spellCheck={false} />
            </Field>
            <Field label={t('Name attribute')}>
              <input value={cfg.nameAttr} onChange={(e) => set('nameAttr', e.target.value)} placeholder="cn" spellCheck={false} />
            </Field>
            <Field label={t('Email attribute')}>
              <input value={cfg.emailAttr} onChange={(e) => set('emailAttr', e.target.value)} placeholder="mail" spellCheck={false} />
            </Field>
            <Field label={t('Unchanging id attribute')} note={t('entryUUID for OpenLDAP, objectGUID for Active Directory, ipaUniqueID for FreeIPA.')}>
              <input value={cfg.idAttr} onChange={(e) => set('idAttr', e.target.value)} placeholder="entryUUID" spellCheck={false} />
            </Field>
          </div>
          <Field label={t("Extra certificate authority to trust (PEM)")} note={t("For a directory with its own certificate. Paste the CA certificate here. The server's certificate is always checked.")}>
            <textarea rows={4} value={cfg.caCert} onChange={(e) => set('caCert', e.target.value)} spellCheck={false} placeholder="-----BEGIN CERTIFICATE-----" />
          </Field>
          <Check checked={cfg.skipVerify} onChange={(v) => set('skipVerify', v)}>
            <span style={cfg.skipVerify ? { color: 'var(--warning)' } : undefined}>{t("Don't check the server's certificate (not recommended)")}</span>
          </Check>
        </details>

        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
          <button className="primary" onClick={() => void save()} disabled={busy}>
            {t('Save')}
          </button>
        </div>

        <div className="hint" style={{ display: 'grid', gap: 10 }}>
          <strong>{t('Try it')}</strong>
          <span>{t('Looks a person up with the details above, without signing anybody in. Add their password to check that too. Nothing is saved.')}</span>
          <div className="form-cols" style={{ alignItems: 'end' }}>
            <Field label={t('User name')}>
              <input value={testUser} onChange={(e) => setTestUser(e.target.value)} autoComplete="off" spellCheck={false} />
            </Field>
            <Field label={t('Password (optional)')}>
              <input type="password" value={testPassword} onChange={(e) => setTestPassword(e.target.value)} autoComplete="new-password" />
            </Field>
            <div>
              <button onClick={() => void runTest()} disabled={busy || !testUser.trim()}>
                {t('Find this person')}
              </button>
            </div>
          </div>
          {report && (
            <div role="status">
              {report.found ? (
                <ul style={{ margin: 0 }}>
                  <li>
                    {t('Found')}: <code>{report.dn}</code>
                  </li>
                  <li>
                    {t('User name')}: {report.username || '—'} · {t('Name')}: {report.name || '—'} · {t('Email')}: {report.email || '—'}
                  </li>
                  <li>
                    {t('Unchanging id')}: <code>{report.id}</code>
                  </li>
                  {report.idIsDn && (
                    <li style={{ color: 'var(--warning)' }}>
                      {t("The directory gave no unchanging id (or the bind account can't read it), so the DN is used. If a person is renamed or moved, they would get a new account. Check the id attribute under Attribute names.")}
                    </li>
                  )}
                  <li>{report.admin ? t('Would be an administrator.') : t('Would not be an administrator.')}</li>
                  {report.viewer && !report.admin && <li>{t('Would be a viewer.')}</li>}
                  {report.passwordChecked && <li>{report.passwordOk ? t('The password is right.') : t('The password is wrong.')}</li>}
                </ul>
              ) : (
                <span>{t('Nobody was found with that username. Check the base DN and the user filter.')}</span>
              )}
            </div>
          )}
        </div>
      </div>
      {error && <p className="error-text">{error}</p>}
    </fieldset>
  )
}
