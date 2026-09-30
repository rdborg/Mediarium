// The Mediarium mark (branding/logo/mediarium-mark.svg), inlined so its
// fill="currentColor" picks up whatever color the caller sets — paired
// with .brand-mark { color: var(--accent) } by default. Inlining (rather
// than an <img src>) is what the brand kit's own README recommends, since
// an <img> can't inherit currentColor.
export default function BrandMark({ className }: { className?: string }) {
  return (
    <svg className={className ?? 'brand-mark'} viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <path d="M5 28V15a11 11 0 0 1 22 0v13h-6l-5-11-5 11z" fill="currentColor" />
    </svg>
  )
}
