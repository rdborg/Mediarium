MEDIARIUM BRAND KIT v1

Mediarium-Brand-Guide.pdf   5-page guide: logo rules, colours, UI accents, type
logo/      mark (colour, light, mono/currentColor, CSS-var themed), app icon tile,
           horizontal lockups (dark / light / mono, wordmark outlined), 512px PNGs
favicon/   favicon.svg, favicon.ico (16/32/48), PNGs, apple-touch-icon-180.png
colors/    mediarium-tokens.css (drop-in CSS variables, dark default + [data-theme="light"])
           mediarium-tokens.json (same values for code / design tools)
fonts/     Sora (variable), IBM Plex Sans (variable), IBM Plex Mono 400/500 + OFL licences

Quick start
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <link rel="icon" href="/favicon.ico" sizes="any">
  Inline logo/mediarium-mark.svg in the sidebar and set  .logo-mark { color: var(--accent) }
  Use logo/mediarium-icon-512.png for Docker / Unraid template icons.
