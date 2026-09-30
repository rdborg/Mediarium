// Package libimport scans an existing media folder for movies or TV
// episodes, works out which title each file belongs to from its filename and
// folder names, and decides how confidently a metadata search result matches
// it. It knows nothing about TMDB or the database: the api package supplies
// search results and registers what the user confirms, so this logic is
// testable against plain directory trees.
package libimport
