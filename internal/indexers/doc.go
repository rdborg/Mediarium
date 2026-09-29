// Package indexers implements the indexer engine.
//
// Two kinds of indexer are supported:
//
//   - Newznab/Torznab APIs (newznab.go): the API most Usenet indexers, and
//     Prowlarr/Jackett in front of torrent sites, already speak.
//   - Definition-based ("Cardigann") sites (cardigann*.go): torrent sites
//     without such an API, driven by the community-maintained YAML
//     definitions from github.com/Prowlarr/Indexers. The definitions are not
//     bundled; DefinitionStore downloads them on demand. The engine logs in,
//     searches, scrapes result rows with CSS selectors (or JSON paths) and
//     resolves download links, optionally passing Cloudflare checks through
//     a user-run FlareSolverr.
//
// Both produce the same Result type, so search, parsing, quality ranking
// and grabbing work the same for every indexer.
package indexers
