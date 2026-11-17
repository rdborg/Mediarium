package indexers

import (
	"net/url"
	"strings"
)

// secretParams are query keys that carry a login: an address with one of them is
// never handed to the browser as a release page, because the page would open
// (and be remembered by the browser and the site) with the indexer's key in it.
var secretParams = map[string]bool{
	"apikey": true, "api_key": true, "api-key": true, "key": true, "k": true,
	"passkey": true, "pass_key": true, "pass": true, "password": true, "pwd": true,
	"rsskey": true, "rss_key": true, "torrent_pass": true, "torrentpass": true,
	"authkey": true, "auth_key": true, "auth": true, "token": true, "access_token": true,
	"secret": true, "sid": true, "session": true, "sessionid": true, "phpsessid": true,
	"cookie": true, "uid": true, "user": true, "username": true, "login": true,
	"r": true, // Newznab download links end in &r=<api key>
	"i": true, // ... and &i=<user id>
}

// downloadPathParts mark an address that fetches the file itself (or talks to the
// API) instead of showing a page.
var downloadPathParts = []string{"/getnzb", "/download", ".nzb", ".torrent", "/api", "/rss", "/dl/", "/get/"}

// SafeInfoURL returns the page of a release on its tracker, for a link in the
// release list, or "" when there is no address that is safe to show. It takes
// only http and https, no address with a name and password in it, no address
// with a key, a passkey, a token or a cookie in its query, no address that
// downloads the file or calls the indexer's API, and never the download link
// itself. The result is the cleaned address (the part after # is dropped).
func SafeInfoURL(raw, downloadURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, " \t\r\n\x00") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return ""
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return ""
	}
	if u.User != nil || u.Hostname() == "" {
		return ""
	}
	// A query Go can't read cleanly is refused: u.Query() would silently drop
	// the pieces it doesn't understand (a ";" or a bad escape), and a key in
	// one of those pieces would slip through.
	if strings.Contains(u.RawQuery, ";") {
		return ""
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ""
	}
	for key, values := range query {
		k := strings.ToLower(key)
		if secretParams[k] || strings.Contains(k, "apikey") || strings.Contains(k, "passkey") || strings.Contains(k, "token") {
			return ""
		}
		// a "t=get" or "t=getnzb" asks the API for the file
		if k == "t" || k == "action" || k == "do" {
			for _, v := range values {
				if lv := strings.ToLower(v); strings.HasPrefix(lv, "get") || strings.Contains(lv, "download") {
					return ""
				}
			}
		}
	}
	lowerPath := strings.ToLower(u.EscapedPath())
	for _, part := range downloadPathParts {
		if strings.Contains(lowerPath+"/", part) {
			return ""
		}
	}
	u.Fragment, u.RawFragment = "", ""
	out := u.String()
	if dl := strings.TrimSpace(downloadURL); dl != "" {
		if out == dl || strings.TrimSuffix(out, "/") == strings.TrimSuffix(dl, "/") {
			return ""
		}
	}
	return out
}
