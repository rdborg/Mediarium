package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
)

// Searching for albums on the indexers and choosing a release: interactive
// (every result with the reasons automation would skip it), "search now"
// for one album, and the scheduled hunt for every wanted album.

// musicCategories are the Newznab/Torznab audio categories: audio, MP3,
// lossless and other audio.
var musicCategories = []int{3000, 3010, 3040, 3050}

// maxMusicSearchesPerHunt caps how many albums one scheduled hunt searches
// for; the next hunt carries on where this one stopped, so a big wanted
// list is worked through over several runs without flooding the indexers.
const maxMusicSearchesPerHunt = 25

// albumQuery is what is searched for: "Artist Album".
func albumQuery(artist music.Artist, album music.Album) string {
	return artist.Name + " " + album.Title
}

// soulseekWords turns a name into the words Soulseek matches: letters and
// digits only (so "AC/DC" is "AC DC" and "Don't" is "Don t"), no bracketed tail
// such as "(Deluxe Edition)" or "[Remastered]".
func soulseekWords(name string) string {
	var b strings.Builder
	depth := 0
	for _, r := range name {
		switch {
		case r == '(' || r == '[':
			depth++
			b.WriteByte(' ')
		case r == ')' || r == ']':
			if depth > 0 {
				depth--
			}
			b.WriteByte(' ')
		case depth > 0:
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// soulseekQueries lists what to ask Soulseek for an album, from the most
// exact to the widest: artist and title, then the title alone (a soundtrack
// or a "Various Artists" album is shared under many artist names), then the
// artist and the first words of a long title. Repeats are dropped.
func soulseekQueries(artist music.Artist, album music.Album) []string {
	name := soulseekWords(artist.Name)
	if strings.EqualFold(name, "various artists") || strings.EqualFold(name, "various") {
		name = ""
	}
	title := soulseekWords(album.Title)
	if title == "" {
		title = soulseekWords(strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ").Replace(album.Title))
	}
	var out []string
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" {
			return
		}
		for _, have := range out {
			if strings.EqualFold(have, q) {
				return
			}
		}
		out = append(out, q)
	}
	add(name + " " + title)
	add(title)
	if w := strings.Fields(title); len(w) > 3 {
		add(name + " " + strings.Join(w[:3], " "))
	}
	if len(out) == 0 {
		add(albumQuery(artist, album))
	}
	return out
}

// musicReleasePayload is one indexer result for an album.
type musicReleasePayload struct {
	Title       string `json:"title"`
	IndexerName string `json:"indexerName"`
	Protocol    string `json:"protocol"`
	DownloadURL string `json:"downloadUrl"` // an opaque reference to send back to grab
	SizeBytes   int64  `json:"sizeBytes"`
	PublishDate string `json:"publishDate,omitempty"`
	Seeders     int    `json:"seeders,omitempty"`
	Peers       int    `json:"peers,omitempty"`
	// Soulseek only: the user's upload speed (bytes a second), how many files wait
	// in their queue, and whether a slot is free now.
	PeerSpeed int64 `json:"peerSpeed,omitempty"`
	PeerQueue int64 `json:"peerQueue,omitempty"`
	PeerFree  bool  `json:"peerFree,omitempty"`
	// BelowMinSeeders marks a torrent with fewer seeders than its indexer's minimum
	// (MinSeeders): automatic searches skip it, a grab by hand is allowed.
	BelowMinSeeders bool `json:"belowMinSeeders,omitempty"`
	MinSeeders      int  `json:"minSeeders,omitempty"`
	// What the release name says.
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Year        int    `json:"year,omitempty"`
	Format      string `json:"format,omitempty"`
	Bitrate     string `json:"bitrate,omitempty"`
	BitDepth    int    `json:"bitDepth,omitempty"`
	Source      string `json:"source,omitempty"`
	Discography bool   `json:"discography,omitempty"`
	Quality     string `json:"quality"` // the tier: FLAC, MP3-320/V0, ...
	// Blocklisted releases failed before; automation skips them, a manual
	// grab is still allowed.
	Blocklisted bool `json:"blocklisted,omitempty"`
	// Rejections explain why automation would not take this release; a
	// manual grab is still allowed.
	Rejections []string           `json:"rejections,omitempty"`
	AcceptedBy *acceptedByPayload `json:"acceptedBy,omitempty"`
}

// albumCurrentTier is the tier on disk for a downloaded album, "" otherwise.
func albumCurrentTier(album music.Album) music.Tier {
	if album.Status != music.StatusDownloaded {
		return ""
	}
	if album.Quality == "" {
		return music.TierUnknown
	}
	return music.Tier(album.Quality)
}

// musicRejections lists every reason automation would not take a release
// for an album: another artist or album, a discography, torrents while the
// defaults say Usenet only (or the reverse), quality outside the profile.
func (s *Server) musicRejections(res indexers.Result, rel music.Release, artist music.Artist, album music.Album, profile music.Profile, sources string) []string {
	var out []string
	if ok, why := music.ReleaseMatch(rel, artist.Name, album.Title, album.Year()); !ok {
		out = append(out, why)
	}
	if sources != sourcesBoth && sources != "" && res.Protocol != indexers.Protocol(sources) {
		out = append(out, sourceReason(sources))
	}
	if why := s.retentionRejection(res); why != "" {
		out = append(out, why)
	}
	return append(out, profile.Rejections(rel.Tier(), albumCurrentTier(album))...)
}

func musicAcceptedBy(profile music.Profile, t music.Tier) *acceptedByPayload {
	by, fallback, ok := profile.AcceptedBy(t)
	if !ok {
		return nil
	}
	return &acceptedByPayload{ProfileID: by.ID, ProfileName: by.Name, Fallback: fallback}
}

// albumProfile is the profile an artist's albums are judged by.
func (s *Server) albumProfile(artist music.Artist) (music.Profile, error) {
	profiles, err := s.MusicRepo.ListProfiles()
	if err != nil {
		return music.Profile{}, err
	}
	p, ok := s.resolveMusicProfile(profiles, artist.ProfileID)
	if !ok {
		return music.Profile{}, fmt.Errorf("no music quality profiles")
	}
	return p, nil
}

// handleAlbumSearch is the interactive search for one album: every release
// the indexers have for "Artist Album" in the audio categories, with what
// its name says and the reasons automation would skip it.
func (s *Server) handleAlbumSearch(w http.ResponseWriter, r *http.Request) {
	album, artist, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []musicReleasePayload{}
	profile, err := s.albumProfile(artist)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Soulseek (slskd), when it is set up, is asked at the same time as the indexers.
	soulseek := make(chan soulseekRun, 1)
	go func() { soulseek <- s.soulseekAlbumRun(r.Context(), artist, album, profile) }()
	if len(instances) == 0 {
		run := <-soulseek
		writeSearchWith(w, r, append(out, run.Releases...), 0, nil, nil, nil, run.extra())
		return
	}
	blocked := s.searchSkipKeys()
	sources := s.sourcesFor("")
	// A site that lists no audio category is not asked for an album.
	asked, left := indexers.SplitByCategories(r.Context(), instances, musicCategories)
	var notAsked []string
	for _, inst := range left {
		if inst.Enabled {
			notAsked = append(notAsked, inst.Name)
		}
	}
	outcomes := indexers.SearchAll(indexers.AsManual(r.Context()), asked, albumQuery(artist, album), musicCategories)
	for _, res := range indexers.MergeResults(outcomes) {
		rel := music.ParseRelease(res.Title)
		p := musicReleasePayload{
			Title: res.Title, IndexerName: res.IndexerName, Protocol: string(res.Protocol), DownloadURL: s.offered.ref(res.DownloadURL),
			SizeBytes: res.SizeBytes, Seeders: res.Seeders, Peers: res.Peers,
			Artist: rel.Artist, Album: rel.Album, Year: rel.Year, Format: string(rel.Format), Bitrate: rel.Bitrate, BitDepth: rel.BitDepth,
			Source: rel.Source, Discography: rel.Discography, Quality: string(rel.Tier()),
			Blocklisted: blocked[blocklist.Key(res.Title)],
			Rejections:  s.musicRejections(res, rel, artist, album, profile, sources),
			AcceptedBy:  musicAcceptedBy(profile, rel.Tier()),
		}
		if !res.PublishDate.IsZero() {
			p.PublishDate = res.PublishDate.Format(time.RFC3339)
		}
		if belowMinSeeders(res) {
			p.BelowMinSeeders, p.MinSeeders = true, res.MinSeeders
		}
		out = append(out, p)
	}
	run := <-soulseek
	out = append(out, run.Releases...)
	writeSearchWith(w, r, out, len(instances), outcomes, notAsked, instances, run.extra())
}

// handleAlbumGrab grabs one release for an album (from the interactive
// search: {releaseTitle, downloadUrl, sizeBytes, protocol}) through the
// same download queue as movies and shows.
func (s *Server) handleAlbumGrab(w http.ResponseWriter, r *http.Request) {
	album, artist, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	var req grabRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.ReleaseTitle == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "releaseTitle and downloadUrl are required")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}
	queueID, err := s.grabAlbum(artist, album, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.musicProtocol(), grabPicked)
	if err != nil {
		writeGrabError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": queueID})
}

// handleAlbumSearchNow runs the automatic search for one album right now,
// even when it is not monitored, and grabs the best acceptable release.
func (s *Server) handleAlbumSearchNow(w http.ResponseWriter, r *http.Request) {
	album, artist, ok := s.loadAlbum(w, r)
	if !ok {
		return
	}
	if album.Status == music.StatusDownloading {
		writeError(w, http.StatusConflict, "This album is already downloading.")
		return
	}
	instances, ok := s.hasIndexers(w)
	if !ok {
		return
	}
	profile, err := s.albumProfile(artist)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.huntAlbum(r.Context(), artist, album, profile, instances, s.searchSkipKeys(), true) {
		writeJSON(w, http.StatusOK, searchNowResponse{Grabbed: 1, Message: "Started a download. Follow it in Activity."})
		return
	}
	writeJSON(w, http.StatusOK, searchNowResponse{Message: "Nothing suitable was found right now."})
}

// huntAlbum searches for one album and grabs the best release automation
// may take: for a missing album the best the profile accepts (or, failing
// that, the first fallback profile that accepts something), for a
// downloaded one only a real upgrade. Unless force is set, the album must
// be monitored and wanted. It records the search in the album's activity
// and reports whether something was grabbed.
func (s *Server) huntAlbum(ctx context.Context, artist music.Artist, album music.Album, profile music.Profile, instances []indexers.Instance, blocked map[string]bool, force bool) bool {
	current := albumCurrentTier(album)
	switch album.Status {
	case music.StatusDownloading:
		return false
	case music.StatusDownloaded:
		if force && s.albumIncomplete(album.ID) {
			// A search by hand for an album with tracks missing: any
			// release the profile accepts is better than what is there,
			// so it is judged like a missing album, not as an upgrade.
			current = ""
		} else if !profile.WantsUpgrade(current) {
			return false
		}
	}
	if !force && !album.Monitored {
		return false
	}
	asked, _ := indexers.SplitByCategories(ctx, instances, musicCategories)
	outcomes := indexers.SearchAll(ctx, asked, albumQuery(artist, album), musicCategories)
	results, expired := s.freshResults(indexers.MergeResults(outcomes))
	results, thin := s.keepSeeded(ctx, results)
	sources := s.sourcesFor("")
	best, verdict := pickMusicRelease(results, blocked, sources, artist, album, profile, current)
	verdict.addExpired(expired, s.retentionDays())
	verdict.addThin(thin)
	verdict.failedIndexers = failedIndexers(outcomes)
	what := "Searched"
	if current != "" {
		what = "Searched for an upgrade over " + string(current)
	}
	picked := ""
	if best != nil {
		picked = fmt.Sprintf("picked %q", best.Title)
		if best.IndexerName != "" {
			picked += " from " + best.IndexerName
		}
		if by, fallback, ok := profile.AcceptedBy(music.ParseRelease(best.Title).Tier()); ok && fallback {
			picked += fmt.Sprintf(" with the fallback profile %q", by.Name)
		} else {
			picked += fmt.Sprintf(" with the %q profile", profile.Name)
		}
	}
	msg, level := verdict.message(what, picked)
	s.albumEvent(album.ID, "searched", level, msg)
	if best == nil {
		return false
	}
	if _, err := s.grabAlbum(artist, album, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, searchKind(force)); err != nil {
		slog.Info("music: automatic grab refused", "album", album.Title, "artist", artist.Name, "release", best.Title, "err", err)
		return false
	}
	return true
}

// pickMusicRelease judges every result for an album, in the order
// automation applies its checks (blocklist, downloader choice, is it this
// album, quality), and returns the best one it may take with the tally for
// the album's activity log. current is the tier on disk ("" when missing):
// then only an upgrade under the album's own profile counts.
func pickMusicRelease(results []indexers.Result, blocked map[string]bool, sources string, artist music.Artist, album music.Album, profile music.Profile, current music.Tier) (*indexers.Result, searchVerdict) {
	v := searchVerdict{total: len(results)}
	type candidate struct {
		res   *indexers.Result
		tier  music.Tier
		chain int // index in the profile chain that accepts it
	}
	var cands []candidate
	for i := range results {
		res := &results[i]
		rel := music.ParseRelease(res.Title)
		tier := rel.Tier()
		switch {
		case blocked[blocklist.Key(res.Title)]:
			v.reject("blocklisted")
			continue
		case sources != sourcesBoth && sources != "" && res.Protocol != indexers.Protocol(sources):
			v.reject(sourceReason(sources))
			continue
		}
		if ok, why := music.ReleaseMatch(rel, artist.Name, album.Title, album.Year()); !ok {
			v.reject(why)
			continue
		}
		if current != "" {
			if !profile.IsUpgrade(current, tier) {
				if profile.Accepts(tier) {
					v.reject("not an upgrade over " + string(current))
				} else {
					v.reject(musicQualityReason(tier))
				}
				continue
			}
			v.acceptable++
			cands = append(cands, candidate{res, tier, 0})
			continue
		}
		chain := -1
		for ci, p := range profile.Chain() {
			if p.Accepts(tier) {
				chain = ci
				break
			}
		}
		if chain < 0 {
			v.reject(musicQualityReason(tier))
			continue
		}
		v.acceptable++
		if chain > 0 {
			v.fallbackOnly++
		}
		cands = append(cands, candidate{res, tier, chain})
	}
	var best *candidate
	for i := range cands {
		c := &cands[i]
		switch {
		case best == nil,
			c.chain < best.chain,
			c.chain == best.chain && music.Rank(c.tier) > music.Rank(best.tier),
			c.chain == best.chain && c.tier == best.tier && preferUsenet(best.res, c.res):
			best = c
		}
	}
	if best == nil {
		return nil, v
	}
	return best.res, v
}

func musicQualityReason(t music.Tier) string {
	if t == music.TierUnknown {
		return "unknown quality"
	}
	return string(t)
}

// albumEvent records an item-only event in an album's activity log. A
// failure to record it is logged: an event must never stop the work.
func (s *Server) albumEvent(albumID int64, kind string, level queue.Level, message string) {
	if err := s.QueueRepo.LogItemEvent(queue.ItemEvent{AlbumID: albumID, Kind: kind, Level: level, Message: message, ItemOnly: true}); err != nil {
		slog.Warn("api: record album event", "albumId", albumID, "kind", kind, "err", err)
	}
}

// huntMusic is the music part of the scheduled hunt: it searches for the
// wanted albums (missing first, then upgrades), at most
// maxMusicSearchesPerHunt per run, carrying on where the last run stopped.
// Nothing happens while the music module is off.
func (s *Server) huntMusic(ctx context.Context, instances []indexers.Instance) {
	if !s.musicEnabled() || len(instances) == 0 {
		return
	}
	missing, err := s.wantedAlbums(false)
	if err != nil {
		slog.Warn("music: hunt: list wanted albums", "err", err)
		return
	}
	upgrades, err := s.wantedAlbums(true)
	if err != nil {
		slog.Warn("music: hunt: list albums to upgrade", "err", err)
		return
	}
	wanted := append(missing, upgrades...)
	if len(wanted) == 0 {
		return
	}
	s.music.mu.Lock()
	start := s.music.huntOffset % len(wanted)
	n := min(len(wanted), maxMusicSearchesPerHunt)
	s.music.huntOffset = start + n
	s.music.mu.Unlock()

	blocked := s.searchSkipKeys()
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			return
		}
		wa := wanted[(start+i)%len(wanted)]
		if busy, err := s.QueueRepo.HasActiveForAlbum(wa.album.ID); err != nil || busy {
			continue
		}
		if s.huntAlbum(ctx, wa.artist, wa.album, wa.profile, instances, blocked, false) && !s.autoGrabRoom() {
			holdBack("hunt")
			return
		}
	}
}

// searchArtistAlbums searches right away for every wanted album of one
// artist (after adding it with "search now").
func (s *Server) searchArtistAlbums(artistID int64) {
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	wanted, err := s.wantedAlbums(false)
	if err != nil {
		return
	}
	blocked := s.searchSkipKeys()
	searched := 0
	for _, wa := range wanted {
		if wa.artist.ID != artistID {
			continue
		}
		if searched++; searched > maxMusicSearchesPerHunt {
			return // the scheduled hunt takes care of the rest
		}
		s.huntAlbum(context.Background(), wa.artist, wa.album, wa.profile, instances, blocked, false)
	}
}

// recentAlbumBlocklists counts the releases blocklisted for an album in the
// last retry window, from its activity log (blocklist entries themselves
// are not tied to albums).
func (s *Server) recentAlbumBlocklists(albumID int64) int {
	events, err := s.QueueRepo.AlbumEvents(albumID, maxItemEvents)
	if err != nil {
		return 0
	}
	since := time.Now().Add(-retryWindow).UTC().Format("2006-01-02T15:04:05.000Z")
	n := 0
	for _, e := range events {
		if e.Kind == "blocklisted" && e.At >= since {
			n++
		}
	}
	return n
}

// retryAlbum tries the next-best release after a bad one was blocklisted,
// when automation is on and the album is still wanted.
func (s *Server) retryAlbum(albumID int64) {
	if !s.automationEnabled() || !s.musicEnabled() {
		return
	}
	album, err := s.MusicRepo.GetAlbum(albumID)
	if err != nil || album.Status != music.StatusMissing {
		return
	}
	artist, err := s.MusicRepo.GetArtist(album.ArtistID)
	if err != nil {
		return
	}
	if n := s.recentAlbumBlocklists(albumID); n > maxAutoRetries {
		s.albumEvent(albumID, "retry", queue.LevelWarn, retryPausedMessage(n))
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profile, err := s.albumProfile(artist)
	if err != nil {
		return
	}
	if !s.huntAlbum(context.Background(), artist, album, profile, instances, s.searchSkipKeys(), false) {
		s.albumEvent(albumID, "retry", queue.LevelWarn, noOtherRelease)
	}
}

// albumIncomplete reports whether a downloaded album has tracks without a
// file (its tracklist is known and not every wanted track has one).
func (s *Server) albumIncomplete(albumID int64) bool {
	tracks, err := s.MusicRepo.ListTracks(albumID)
	if err != nil || len(tracks) == 0 {
		return false
	}
	for _, t := range tracks {
		if t.FilePath == "" && t.Monitored {
			return true
		}
	}
	return false
}
