// Package web embeds the built frontend (dist/, produced by `npm run
// build` — see README.md) so the compiled Go binary serves the whole UI
// itself with no external file dependency (single
// binary, single process).
package web

import "embed"

//go:embed dist
var DistFS embed.FS

// DistDir is the subdirectory name within DistFS that dist's own files
// live under (embed.FS keeps the "dist/" prefix from the pattern above).
const DistDir = "dist"
