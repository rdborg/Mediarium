package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/slskd"
)

// Soulseek through the person's own slskd (V4 D19, #143), for music only.
// An album's Choose a release also asks slskd, which searches Soulseek; each
// user's folder with the album's tracks becomes one release. Grabbing one asks
// slskd to download those files into its own downloads folder. When they are
// all there, Mediarium links them into the download's folder (the same "client
// view" a download client's files get) and imports them like any album; the
// files in slskd's folder stay, so slskd keeps sharing them.
//
// It is off until slskd's address and a read-write API key are saved, with
// the folder slskd downloads into as Mediarium sees it.

const (
	soulseekSearchWait = 30 * time.Second
	// soulseekQueryWait is how long one ask to slskd may take; an empty answer
	// is asked again in a wider form, so each try gets less than the whole.
	soulseekQueryWait = 20 * time.Second
	soulseekPoll      = 5 * time.Second
	// soulseekQueuedFor is how long a download may wait in the other user's
	// queue before another release is tried. Soulseek queues can be long.
	soulseekQueuedFor = 6 * time.Hour
	// soulseekStalledFor is how long a started download may get no data.
	soulseekStalledFor = 30 * time.Minute
	soulseekMaxResults = 25
	soulseekURLPrefix  = "slsk:"
)

type soulseekConfig struct {
	URL, Key, Folder string
}

func (s *Server) soulseekConfig() soulseekConfig {
	u, _ := s.Settings.Get(settings.KeySoulseekURL)
	k, _ := s.Settings.Get(settings.KeySoulseekAPIKey)
	f, _ := s.Settings.Get(settings.KeySoulseekFolder)
	return soulseekConfig{URL: strings.TrimSpace(u), Key: strings.TrimSpace(k), Folder: strings.TrimSpace(f)}
}

func (c soulseekConfig) on() bool { return c.URL != "" && c.Key != "" && c.Folder != "" }

func (s *Server) soulseekClient() (*slskd.Client, soulseekConfig, bool) {
	cfg := s.soulseekConfig()
	if !cfg.on() || !s.musicEnabled() {
		return nil, cfg, false
	}
	return slskd.New(cfg.URL, cfg.Key), cfg, true
}

// soulseekGrab is what a Soulseek release's download address holds: whose
// files and which ones.
type soulseekGrab struct {
	User  string       `json:"u"`
	Files []slskd.Want `json:"f"`
}

func encodeSoulseek(g soulseekGrab) string {
	b, _ := json.Marshal(g)
	return soulseekURLPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func decodeSoulseek(u string) (soulseekGrab, error) {
	var g soulseekGrab
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u, soulseekURLPrefix))
	if err != nil || !strings.HasPrefix(u, soulseekURLPrefix) {
		return g, errors.New("this isn't a Soulseek release")
	}
	if err := json.Unmarshal(raw, &g); err != nil || g.User == "" || len(g.Files) == 0 {
		return g, errors.New("this isn't a Soulseek release")
	}
	return g, nil
}

// soulseekFolder is one user's folder of audio files from a search.
type soulseekFolder struct {
	user  string
	dir   string
	files []slskd.File
	free  bool
	queue int64
	speed int64
}

// soulseekFolders groups the audio files of a search by user and folder.
func soulseekFolders(resps []slskd.Response) []soulseekFolder {
	var out []soulseekFolder
	for _, r := range resps {
		byDir := map[string]*soulseekFolder{}
		var order []string
		for _, f := range r.Files {
			if f.IsLocked || !music.IsAudioFile(slskd.BaseName(f.Filename)) {
				continue
			}
			d := slskd.Dir(f.Filename)
			g := byDir[d]
			if g == nil {
				g = &soulseekFolder{user: r.Username, dir: d, free: r.HasFreeUploadSlot, queue: r.QueueLength, speed: r.UploadSpeed}
				byDir[d] = g
				order = append(order, d)
			}
			g.files = append(g.files, f)
		}
		for _, d := range order {
			out = append(out, *byDir[d])
		}
	}
	return out
}

// tag is the format of a folder, as a release name would write it.
func (f soulseekFolder) tag() string {
	exts := map[string]int{}
	deep, rate, n := false, 0, 0
	for _, x := range f.files {
		e := strings.ToLower(strings.TrimPrefix(filepath.Ext(slskd.BaseName(x.Filename)), "."))
		exts[e]++
		if x.BitDepth >= 24 {
			deep = true
		}
		if x.BitRate > 0 {
			rate += x.BitRate
			n++
		}
	}
	top, most := "", 0
	for e, c := range exts {
		if c > most || (c == most && e < top) {
			top, most = e, c
		}
	}
	switch top {
	case "flac":
		if deep {
			return "FLAC 24bit"
		}
		return "FLAC"
	case "mp3":
		switch avg := rate / max(n, 1); {
		case avg >= 310:
			return "MP3 320"
		case avg >= 220:
			return "MP3 V0"
		case avg > 0:
			return fmt.Sprintf("MP3 %d", avg)
		}
		return "MP3"
	}
	return strings.ToUpper(top)
}

func (f soulseekFolder) size() int64 {
	var n int64
	for _, x := range f.files {
		n += x.Size
	}
	return n
}

// title names a folder as a release, the way a release name reads: the
// folder's own name, with the artist in front when the folder doesn't say it
// (a folder is often just "Album (2014)" inside one named after the artist),
// then the format.
func (f soulseekFolder) title(artist string) string {
	name := slskd.BaseName(f.dir)
	if !strings.Contains(strings.ToLower(name), strings.ToLower(artist)) {
		name = artist + " - " + name
	}
	return name + " [" + f.tag() + "]"
}

// soulseekRowName is the name of Soulseek's line in the table of who a
// search asked.
const soulseekRowName = "Soulseek (slskd)"

// missing names what is still empty in the Soulseek settings, in plain words.
func (c soulseekConfig) missing() []string {
	var out []string
	if c.URL == "" {
		out = append(out, "the slskd address")
	}
	if c.Key == "" {
		out = append(out, "the API key")
	}
	if c.Folder == "" {
		out = append(out, "the downloads folder")
	}
	return out
}

// soulseekFailure puts an error from slskd in a short code, a few plain words
// and the technical text (never with the API key in it).
func soulseekFailure(err error, key string) (code, reason, detail string) {
	detail = err.Error()
	if key != "" {
		detail = strings.ReplaceAll(detail, key, "***")
	}
	if len(detail) > 200 {
		detail = detail[:200] + "..."
	}
	var netErr net.Error
	switch {
	case errors.Is(err, slskd.ErrBadKey):
		return "auth", "the API key was refused (it needs the readwrite role)", detail
	case errors.Is(err, slskd.ErrBusy):
		return "busy", "it is busy, try again in a moment", detail
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout", "it took too long", detail
	case errors.As(err, &netErr):
		return "unreachable", "it could not be reached at that address", detail
	}
	return "error", "it returned an error", detail
}

// soulseekRun is what one album search of Soulseek came to: the releases and
// the line for the table of who was asked. Show is false when Soulseek is not
// used at all (nothing of it is set up), so there is nothing to say.
type soulseekRun struct {
	Releases []musicReleasePayload
	Line     searchedIndexer
	Show     bool
}

// soulseekAlbumRun asks Soulseek for an album and always says what happened:
// in the line it returns and in the log.
func (s *Server) soulseekAlbumRun(ctx context.Context, artist music.Artist, album music.Album, profile music.Profile) soulseekRun {
	cfg := s.soulseekConfig()
	start := time.Now()
	line := searchedIndexer{Name: soulseekRowName, Kind: "soulseek"}
	skip := func(code, reason string) soulseekRun {
		line.Status, line.Code, line.Reason = searchSkipped, code, reason
		slog.Info("soulseek: album search skipped", "album", album.Title, "why", reason)
		return soulseekRun{Line: line, Show: true}
	}
	if cfg == (soulseekConfig{}) {
		slog.Debug("soulseek: album search skipped, Soulseek is not set up", "album", album.Title)
		return soulseekRun{}
	}
	if !s.musicEnabled() {
		return skip("music_off", "Music is switched off.")
	}
	if m := cfg.missing(); len(m) > 0 {
		return skip("not_set_up", fmt.Sprintf("Soulseek is not set up yet: %s is missing.", strings.Join(m, " and ")))
	}
	c := slskd.New(cfg.URL, cfg.Key)
	ctx, cancel := context.WithTimeout(ctx, 3*soulseekQueryWait+15*time.Second)
	defer cancel()
	fail := func(code, reason, detail string) soulseekRun {
		line.Ms = time.Since(start).Milliseconds()
		line.Status, line.Code, line.Reason, line.Detail = searchFailed, code, reason, detail
		slog.Info("soulseek: album search failed", "album", album.Title, "why", reason, "err", detail, "took", time.Since(start).Round(time.Millisecond))
		return soulseekRun{Line: line, Show: true}
	}
	// slskd can answer its web API without being logged in to Soulseek; a
	// search then finds nothing. Older slskd without this call is just searched.
	state := ""
	if st, err := c.Server(ctx); err == nil {
		state = st.State
		if !st.Online() {
			if state == "" {
				state = "not connected"
			}
			return fail("not_connected", fmt.Sprintf("slskd is not connected to the Soulseek network (state: %s).", state), "")
		}
	} else if errors.Is(err, slskd.ErrBadKey) {
		code, reason, detail := soulseekFailure(err, cfg.Key)
		return fail(code, reason, detail)
	}
	// Soulseek matches whole words, so the exact title with its punctuation or
	// an "(Original Motion Picture Soundtrack)" tail finds nothing. The first
	// try is the cleaned artist and title; a search that comes back empty is
	// tried again with fewer words, up to three asks (#143, balkie31).
	var (
		query   string
		resps   []slskd.Response
		out     []musicReleasePayload
		folders int
	)
	for i, q := range soulseekQueries(artist, album) {
		query = q
		var err error
		resps, err = c.Search(ctx, q, soulseekQueryWait)
		if err != nil {
			code, reason, detail := soulseekFailure(err, cfg.Key)
			return fail(code, reason, detail)
		}
		out, folders = s.soulseekReleases(resps, artist, album, profile)
		if len(out) > 0 || i >= 2 {
			break
		}
	}
	line.Ms = time.Since(start).Milliseconds()
	line.Count = len(out)
	line.Status = searchEmpty
	if len(out) > 0 {
		line.Status = searchFound
	}
	slog.Info("soulseek: album search done", "album", album.Title, "query", query, "responses", len(resps), "folders", folders, "releases", len(out), "state", state, "took", time.Since(start).Round(time.Millisecond))
	return soulseekRun{Releases: out, Line: line, Show: true}
}

// extra is the run's line for the table, when there is one to show.
func (r soulseekRun) extra() []searchedIndexer {
	if !r.Show {
		return nil
	}
	return []searchedIndexer{r.Line}
}

// soulseekAlbumReleases is soulseekAlbumRun with an error for a search that
// failed, as the releases alone.
func (s *Server) soulseekAlbumReleases(ctx context.Context, artist music.Artist, album music.Album, profile music.Profile) ([]musicReleasePayload, error) {
	run := s.soulseekAlbumRun(ctx, artist, album, profile)
	if run.Line.Status == searchFailed {
		return nil, errors.New(run.Line.Reason)
	}
	return run.Releases, nil
}

// soulseekReleases turns the answers to a search into releases, one for each
// folder with the album's tracks. It also says how many folders had audio.
func (s *Server) soulseekReleases(resps []slskd.Response, artist music.Artist, album music.Album, profile music.Profile) ([]musicReleasePayload, int) {
	want := 0
	if tracks, err := s.MusicRepo.ListTracks(album.ID); err == nil {
		want = len(tracks)
	}
	folders := soulseekFolders(resps)
	audio := len(folders)
	complete := func(f soulseekFolder) bool { return want == 0 || len(f.files) >= want }
	sort.SliceStable(folders, func(i, j int) bool {
		a, b := folders[i], folders[j]
		if complete(a) != complete(b) {
			return complete(a)
		}
		if a.free != b.free {
			return a.free
		}
		if a.queue != b.queue {
			return a.queue < b.queue
		}
		return a.speed > b.speed
	})
	if len(folders) > soulseekMaxResults {
		folders = folders[:soulseekMaxResults]
	}
	out := make([]musicReleasePayload, 0, len(folders))
	for _, f := range folders {
		grab := soulseekGrab{User: f.user}
		for _, x := range f.files {
			grab.Files = append(grab.Files, slskd.Want{Filename: x.Filename, Size: x.Size})
		}
		title := f.title(artist.Name)
		rel := music.ParseRelease(title)
		p := musicReleasePayload{
			Title: title, IndexerName: "Soulseek · " + f.user, Protocol: string(indexers.ProtocolSoulseek), DownloadURL: s.offered.ref(encodeSoulseek(grab)),
			SizeBytes: f.size(), Artist: rel.Artist, Album: rel.Album, Year: rel.Year, Format: string(rel.Format), Bitrate: rel.Bitrate, BitDepth: rel.BitDepth,
			Quality: string(rel.Tier()), AcceptedBy: musicAcceptedBy(profile, rel.Tier()),
		}
		if !complete(f) {
			p.Rejections = append(p.Rejections, fmt.Sprintf("It has %d of the album's %d tracks.", len(f.files), want))
		}
		if !f.free {
			p.Rejections = append(p.Rejections, fmt.Sprintf("The user's upload slots are busy, %d files are waiting before yours.", f.queue))
		}
		p.Rejections = append(p.Rejections, profile.Rejections(rel.Tier(), albumCurrentTier(album))...)
		out = append(out, p)
	}
	return out, audio
}

// downloadSoulseek asks slskd for a release's files, waits for them, and
// links them into the download's folder for the import. It answers the
// folder.
func (s *Server) downloadSoulseek(ctx context.Context, queueID int64, downloadURL string) (string, error) {
	c, cfg, ok := s.soulseekClient()
	if !ok {
		return "", errors.New("Soulseek isn't set up any more. Save slskd's address, key and folder under Settings > Downloading > Soulseek")
	}
	g, err := decodeSoulseek(downloadURL)
	if err != nil {
		return "", err
	}
	wanted := map[string]int64{}
	for _, f := range g.Files {
		wanted[f.Filename] = f.Size
	}
	// A download that is carried on (after a pause or a restart) doesn't ask twice.
	have, err := c.Downloads(ctx, g.User)
	if err != nil {
		return "", fmt.Errorf("slskd: %w", err)
	}
	queued := map[string]bool{}
	for _, t := range have {
		if _, ok := wanted[t.Filename]; ok && !(t.State.Done() && !t.State.Succeeded()) {
			queued[t.Filename] = true
		}
	}
	var ask []slskd.Want
	for _, f := range g.Files {
		if !queued[f.Filename] {
			ask = append(ask, f)
		}
	}
	if len(ask) > 0 {
		if err := c.Enqueue(ctx, g.User, ask); err != nil {
			if errors.Is(err, slskd.ErrUserOffline) {
				return "", badRelease(fmt.Errorf("the Soulseek user %s is offline", g.User))
			}
			return "", fmt.Errorf("slskd couldn't queue the files: %w", err)
		}
		s.queueEvent(queueID, "download", queue.LevelInfo, fmt.Sprintf("Asked slskd for %d files from %s", len(ask), g.User))
	}

	var total int64
	for _, sz := range wanted {
		total += sz
	}
	started := time.Now()
	lastMove, lastBytes := time.Now(), int64(-1)
	for {
		list, err := c.Downloads(ctx, g.User)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			slog.Warn("soulseek: check the downloads", "queueId", queueID, "err", err)
		} else {
			var got int64
			done, failed, moving := 0, 0, false
			seen := map[string]slskd.Transfer{}
			for _, t := range list {
				if _, ok := wanted[t.Filename]; ok {
					seen[t.Filename] = t // the newest try of a file wins
				}
			}
			for _, t := range seen {
				got += min(t.BytesTransferred, t.Size)
				switch {
				case t.State.Succeeded():
					done++
				case t.State.Done():
					failed++
				case t.State&slskd.StateInProgress != 0:
					moving = true
				}
			}
			_ = s.QueueRepo.SetProgress(queueID, float64(got)*100/float64(max(total, 1)))
			if done+failed == len(wanted) {
				if failed > 0 {
					return "", badRelease(fmt.Errorf("%s couldn't send %d of the %d files on Soulseek", g.User, failed, len(wanted)))
				}
				return s.soulseekView(queueID, cfg.Folder, g.Files)
			}
			if got != lastBytes {
				lastBytes, lastMove = got, time.Now()
			}
			switch {
			case moving && time.Since(lastMove) > soulseekStalledFor:
				return "", badRelease(fmt.Errorf("nothing came from %s on Soulseek for %s", g.User, soulseekStalledFor))
			case !moving && got == 0 && time.Since(started) > soulseekQueuedFor:
				return "", badRelease(fmt.Errorf("the files waited in %s's queue on Soulseek for %s without starting", g.User, soulseekQueuedFor))
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err() // paused or stopped: slskd keeps the files it has
		case <-time.After(soulseekPoll):
		}
	}
}

// soulseekView finds the finished files in slskd's downloads folder (by name
// and size: where slskd puts them depends on its own settings) and links them
// into the download's folder, marked as a client view so the import leaves
// slskd's files where they are.
func (s *Server) soulseekView(queueID int64, folder string, files []slskd.Want) (string, error) {
	want := map[string]int64{}
	for _, f := range files {
		want[strings.ToLower(slskd.BaseName(f.Filename))] = f.Size
	}
	found := map[string]string{}
	root := filepath.Clean(folder)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if d.IsDir() {
			if rel, _ := filepath.Rel(root, p); strings.Count(rel, string(filepath.Separator)) >= 4 {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		size, ok := want[name]
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() != size {
			return nil
		}
		if old, dup := found[name]; dup {
			if oi, err := os.Stat(old); err == nil && !info.ModTime().After(oi.ModTime()) {
				return nil // an older copy from another download: keep the newest
			}
		}
		found[name] = p
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read slskd's downloads folder %s: %w", folder, err)
	}
	if len(found) < len(want) {
		return "", fmt.Errorf("slskd says the files are downloaded, but %d of them aren't in %s. Check the Soulseek folder under Settings > Downloading > Soulseek", len(want)-len(found), folder)
	}
	dir := s.defaultWorkDirFor(queueID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create download folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, clientContentFile), []byte(root+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", clientContentFile, err)
	}
	for _, p := range found {
		if err := viewLink(p, filepath.Join(dir, filepath.Base(p))); err != nil {
			return "", fmt.Errorf("link %s: %w", filepath.Base(p), err)
		}
	}
	return dir, nil
}

// ---- Settings > Downloading > Soulseek ----

type soulseekPayload struct {
	URL     string `json:"url"`
	HasKey  bool   `json:"hasKey"`
	Folder  string `json:"folder"`
	Version string `json:"version,omitempty"` // slskd's, after a save that reached it
	// Warning is set when the save worked but Soulseek will still be skipped:
	// the downloads folder is empty, or slskd is not logged in to Soulseek.
	Warning string `json:"warning,omitempty"`
}

func (s *Server) soulseekState() soulseekPayload {
	cfg := s.soulseekConfig()
	return soulseekPayload{URL: cfg.URL, HasKey: cfg.Key != "", Folder: cfg.Folder}
}

func (s *Server) handleGetSoulseek(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.soulseekState())
}

// handlePutSoulseek saves slskd's address, key and folder after checking that
// slskd answers with the key and that the folder is there; remove takes it all away.
func (s *Server) handlePutSoulseek(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"`
		Key    string `json:"apiKey"`
		Folder string `json:"folder"`
		Remove bool   `json:"remove"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Remove {
		tx := &settingsSave{}
		_ = tx.Set(settings.KeySoulseekURL, "", false)
		_ = tx.Set(settings.KeySoulseekAPIKey, "", true)
		_ = tx.Set(settings.KeySoulseekFolder, "", false)
		if err := s.commitSettings(tx); err != nil {
			writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
			return
		}
		writeJSON(w, http.StatusOK, s.soulseekState())
		return
	}
	cfg := s.soulseekConfig()
	addr, key, folder := strings.TrimSpace(req.URL), strings.TrimSpace(req.Key), strings.TrimSpace(req.Folder)
	if key == "" {
		key = cfg.Key
	}
	if rejectBad(w, checkIndexerAddress(addr), checkAPIKey(key)) {
		return
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "Add slskd's API key (a read-write key from slskd's settings).")
		return
	}
	if folder != "" && !filepath.IsAbs(folder) {
		writeError(w, http.StatusBadRequest, "Add the folder slskd downloads into, as a full path as Mediarium sees it (for example /data/slskd/downloads).")
		return
	}
	if st, err := os.Stat(folder); folder != "" && (err != nil || !st.IsDir()) {
		writeError(w, http.StatusBadRequest, "Mediarium can't see that folder. Give Mediarium's container the same folder slskd downloads into, then enter the path it has inside Mediarium.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	version, err := slskd.New(addr, key).Version(ctx)
	if err != nil {
		if errors.Is(err, slskd.ErrBadKey) {
			writeError(w, http.StatusBadRequest, "slskd didn't accept that API key. Use a key with the readwrite role from slskd's settings.")
			return
		}
		writeUpstreamError(w, "reach slskd", err)
		return
	}
	tx := &settingsSave{}
	_ = tx.Set(settings.KeySoulseekURL, addr, false)
	_ = tx.Set(settings.KeySoulseekAPIKey, key, true)
	if folder != "" {
		folder = filepath.Clean(folder)
	}
	_ = tx.Set(settings.KeySoulseekFolder, folder, false)
	if err := s.commitSettings(tx); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	out := s.soulseekState()
	out.Version = version
	switch status, err := slskd.New(addr, key).Server(ctx); {
	case err == nil && !status.Online():
		state := status.State
		if state == "" {
			state = "not connected"
		}
		out.Warning = fmt.Sprintf("slskd is not connected to the Soulseek network (state: %s). Soulseek finds nothing until it is.", state)
	case folder == "":
		out.Warning = "slskd answers, but Soulseek is skipped on albums until the slskd downloads folder is filled in."
	}
	writeJSON(w, http.StatusOK, out)
}
