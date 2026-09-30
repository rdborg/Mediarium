import { Component, type ErrorInfo, type ReactNode } from 'react'
import BrandMark from './BrandMark'

// Catches a page that crashes while it draws, so the person sees a short
// message and a way forward instead of a blank white screen. `resetKey`
// changes when they go somewhere else, which clears the message.
export default class ErrorBoundary extends Component<{ children: ReactNode; resetKey?: string }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // The browser console is where a bug report can pick it up.
    console.error('Mediarium: a page failed to draw.', error, info.componentStack)
  }

  componentDidUpdate(prev: { resetKey?: string }) {
    if (this.state.failed && prev.resetKey !== this.props.resetKey) this.setState({ failed: false })
  }

  render() {
    if (!this.state.failed) return this.props.children
    return (
      <div className="splash" role="alert" style={{ gap: 14, padding: 24, textAlign: 'center' }}>
        <div className="splash-mark">
          <BrandMark className="splash-logo" />
        </div>
        <div className="splash-name">Something went wrong</div>
        <p className="splash-hint">This page couldn't be shown. Reloading usually fixes it. If it keeps happening, report it on the Mediarium GitHub page and say what you were doing.</p>
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', justifyContent: 'center' }}>
          <button type="button" className="primary" onClick={() => window.location.reload()}>
            Reload the page
          </button>
          <a className="btn-link" href="/">
            Go to the dashboard
          </a>
        </div>
      </div>
    )
  }
}
