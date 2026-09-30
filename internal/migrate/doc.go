// Package migrate moves an existing Radarr, Sonarr, Prowlarr, SABnzbd
// (and NZBGet, Jackett, NZBHydra2, Overseerr/Jellyseerr, Ombi, Bazarr, Medusa
// and SickChill) setup into Mediarium without redoing it and without moving a single file.
//
// It only ever reads from those apps: every request it sends them is a GET
// (see read in client.go), except NZBGet's JSON-RPC, which is POST by protocol
// and only ever calls the read-only methods in nzbgetReadOnly (rpc in
// client.go), so trying
// Mediarium side by side changes nothing in the setup already running.
//
// What it reads:
//   - Radarr and Sonarr: the library (TMDB/TVDB ids, monitored flags, the
//     folder each title lives in), quality profiles and root folders.
//   - Prowlarr: indexers (Newznab/Torznab address and API key, or the
//     community definition a site uses).
//   - SABnzbd: the Usenet server accounts.
//
// Paths are as the other apps' containers see them, so each title's folder
// is translated through a path map (from -> to) to where Mediarium sees
// it, and checked to exist before anything is linked. Existing files are
// registered where they are, with their quality read by Mediarium's own
// folder scanner (package libimport).
//
// The package depends on the feature packages it writes to (library,
// indexers, download, quality, metadata, libimport) and never on the api
// package; things only the api package can do (testing a site-based
// indexer, writing the activity feed) are passed in through Deps.
package migrate
