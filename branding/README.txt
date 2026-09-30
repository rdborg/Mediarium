MEDIARIUM BRAND KIT v1

Mediarium-Brand-Guide.pdf     5-page guide: logo rules, colours, UI accents, type
mediarium-mark-*.svg          the mark: colour, light, themed (follows CSS colours)
mediarium-mark-*.png          512px marks in colour, black and white
mediarium-icon.svg / -512.png the app icon tile (used for Docker and Unraid icons)
mediarium-lockup-*.svg / .png horizontal lockups: dark, light and mono
mediarium-tokens.css          drop-in CSS variables, dark by default plus [data-theme="light"]
mediarium-tokens.json         the same values for code and design tools

The favicons and the fonts (Sora, IBM Plex Sans, IBM Plex Mono, with their
OFL licences) used by the app live in web/public.

Quick start
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <link rel="icon" href="/favicon.ico" sizes="any">
  Inline mediarium-mark-themed.svg in the sidebar and set  .logo-mark { color: var(--accent) }
  Use mediarium-icon-512.png for Docker / Unraid template icons.
