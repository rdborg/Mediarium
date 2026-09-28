// Simple heuristic strength meter (PRD §5.2 asks for one on the onboarding
// admin-account step). Not trying to be zxcvbn — length plus character
// variety is enough to steer people away from "password1" without pulling
// in a scoring library for a single form field.
function score(password: string): number {
  if (!password) return 0
  let s = 0
  if (password.length >= 8) s++
  if (password.length >= 12) s++
  if (/[a-z]/.test(password) && /[A-Z]/.test(password)) s++
  if (/[0-9]/.test(password)) s++
  if (/[^a-zA-Z0-9]/.test(password)) s++
  return Math.min(s, 4)
}

const LABELS = ['Very weak', 'Weak', 'Fair', 'Good', 'Strong']
const COLORS = ['var(--danger)', 'var(--danger)', 'var(--warning)', 'var(--success)', 'var(--success)']

export default function PasswordStrength({ password }: { password: string }) {
  if (!password) return null
  const s = score(password)

  return (
    <div style={{ marginTop: '0.35rem' }}>
      <div style={{ display: 'flex', gap: 4 }}>
        {[0, 1, 2, 3].map((i) => (
          <div
            key={i}
            style={{
              height: 4,
              flex: 1,
              borderRadius: 2,
              background: i < s ? COLORS[s] : 'var(--border)',
            }}
          />
        ))}
      </div>
      <p style={{ margin: '0.25rem 0 0', fontSize: '0.8rem', color: COLORS[s] }}>{LABELS[s]}</p>
    </div>
  )
}
