import BrandMark from './BrandMark'

// Full-screen loading screen: the mark, the name, and an animated "Loading".
// The same markup is inlined in index.html so it shows before the app's code
// has even arrived; keep the two in step.
export default function Splash({ label = 'Loading' }: { label?: string }) {
  return (
    <div className="splash" role="status" aria-live="polite">
      <div className="splash-mark">
        <BrandMark className="splash-logo" />
      </div>
      <div className="splash-name">Mediarium</div>
      <div className="splash-label">
        {label}
        <span className="splash-dots" aria-hidden="true">
          <i>.</i>
          <i>.</i>
          <i>.</i>
        </span>
      </div>
    </div>
  )
}
