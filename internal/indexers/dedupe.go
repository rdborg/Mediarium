package indexers

import "strings"

// releaseKeys are the ways one indexer names one release: the id it gave it
// (guid), the torrent's info hash and the download link. Two results of the same
// indexer that share any of them are the same release. The title is not part
// of it: the same release is not a different one because a second search
// spelled its name another way.
func releaseKeys(r Result) []string {
	var keys []string
	if g := strings.TrimSpace(r.GUID); g != "" {
		keys = append(keys, r.IndexerName+"\x00guid\x00"+g)
	}
	if h := strings.ToLower(strings.TrimSpace(r.InfoHash)); h != "" {
		keys = append(keys, r.IndexerName+"\x00hash\x00"+h)
	}
	if u := strings.TrimSpace(r.DownloadURL); u != "" {
		keys = append(keys, r.IndexerName+"\x00link\x00"+u)
	}
	return keys
}

// seenRelease reports whether a release was already seen (by any of its keys)
// and remembers it. A result with no guid, hash or link has nothing to compare
// and is never a duplicate.
func seenRelease(seen map[string]bool, r Result) bool {
	keys := releaseKeys(r)
	dup := false
	for _, k := range keys {
		if seen[k] {
			dup = true
		}
	}
	for _, k := range keys {
		seen[k] = true
	}
	return dup
}

// dedupeResults keeps the first of each release in rs, and tells the number of
// releases that were not in seen before (seen is filled in as it goes, so it
// can be passed on to the next page or query).
func dedupeResults(seen map[string]bool, rs []Result) []Result {
	out := rs[:0:0]
	for _, r := range rs {
		if !seenRelease(seen, r) {
			out = append(out, r)
		}
	}
	return out
}
