package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/rdborg/mediarium/internal/bencsafe"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/other"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/queue"
)

// Adding a download by hand: an NZB or .torrent file, an address that points
// to one, or a magnet link, attached to a movie, an episode, an album or a
// book (one format of it) the person picks. It goes into the download line the
// same way a release picked from a search does (grabRelease, grabTVRelease,
// grabAlbum, grabBook), so everything after it is the usual pipeline.

const (
	// maxManualNZB is the biggest NZB file taken. Real ones are a few MB; a
	// 100 GB release is well under this.
	maxManualNZB = 32 << 20
	// maxManualTorrent is the biggest .torrent file taken. A torrent with a
	// hundred thousand files is about this size.
	maxManualTorrent = 16 << 20
	// maxManualAddress is the longest address or magnet link taken.
	maxManualAddress = 4096
	// maxManualName is the longest release name taken.
	maxManualName = 200
	// manualFetchTimeout is how long fetching an address may take.
	manualFetchTimeout = 45 * time.Second

	// uploadRefPrefix starts the reference a download keeps to a file that was
	// uploaded (or fetched once, when its address was pasted). The file lives
	// in the uploads folder next to the database, not in the queue.
	uploadRefPrefix = "upload:"
	uploadsDirName  = "uploads"
	// uploadKeepFor is how long an uploaded file nobody refers to is kept.
	uploadKeepFor = 24 * time.Hour
)

var uploadNameRe = regexp.MustCompile(`^[0-9a-f]{32}\.(nzb|torrent)$`)

// --- the uploaded files ---

func isUploadRef(ref string) bool { return strings.HasPrefix(ref, uploadRefPrefix) }

func (s *Server) uploadsDir() string { return filepath.Join(s.cfg.ConfigDir, uploadsDirName) }

// saveUpload stores a release file and returns the reference to put in the
// queue. ext is "nzb" or "torrent".
func (s *Server) saveUpload(data []byte, ext string) (string, error) {
	dir := s.uploadsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("couldn't create the folder for uploaded files: %w", err)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("couldn't name the uploaded file: %w", err)
	}
	name := hex.EncodeToString(b) + "." + ext
	tmp, err := os.CreateTemp(dir, ".incoming-*")
	if err != nil {
		return "", fmt.Errorf("couldn't save the uploaded file: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("couldn't save the uploaded file: %w", errors.Join(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("couldn't save the uploaded file: %w", err)
	}
	return uploadRefPrefix + name, nil
}

// uploadPath turns a reference into the file it names. Only names this code
// made are accepted, so a reference can never point outside the folder.
func (s *Server) uploadPath(ref string) (string, error) {
	name := strings.TrimPrefix(ref, uploadRefPrefix)
	if !uploadNameRe.MatchString(name) {
		return "", errors.New("the uploaded file's name is not valid")
	}
	return filepath.Join(s.uploadsDir(), name), nil
}

// readUpload returns the bytes of an uploaded file.
func (s *Server) readUpload(ref string) ([]byte, error) {
	path, err := s.uploadPath(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("the uploaded NZB or torrent file is gone. Add the download again")
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't read the uploaded file: %w", err)
	}
	return data, nil
}

func (s *Server) removeUpload(ref string) {
	if path, err := s.uploadPath(ref); err == nil {
		_ = os.Remove(path)
	}
}

// pruneUploads deletes uploaded files that no download refers to any more (the
// entry was removed or pruned from the history) and are over a day old, plus
// half-written leftovers. It returns how many it removed.
func (s *Server) pruneUploads(now time.Time) int {
	entries, err := os.ReadDir(s.uploadsDir())
	if err != nil {
		return 0
	}
	items, err := s.QueueRepo.List()
	if err != nil {
		return 0
	}
	used := map[string]bool{}
	for _, it := range items {
		if isUploadRef(it.NZBURL) {
			used[strings.TrimPrefix(it.NZBURL, uploadRefPrefix)] = true
		}
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || used[e.Name()] {
			continue
		}
		if !uploadNameRe.MatchString(e.Name()) && !strings.HasPrefix(e.Name(), ".incoming-") {
			continue // not ours
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < uploadKeepFor {
			continue
		}
		if os.Remove(filepath.Join(s.uploadsDir(), e.Name())) == nil {
			removed++
		}
	}
	return removed
}

// --- reading a release file ---

// releaseKind is what a file turned out to be.
type releaseKind string

const (
	kindNZB     releaseKind = "nzb"
	kindTorrent releaseKind = "torrent"
)

// releaseInfo is what is known about a release file once it has been read.
type releaseInfo struct {
	kind  releaseKind
	name  string   // the release's own name (a torrent's name), "" for an NZB
	size  int64    // total size in bytes, 0 when unknown
	files []string // the names of the files inside
	// inside is a release name read from inside the file: the name in an NZB's
	// head or the name of its biggest file, or the name of a torrent's biggest
	// file. "" when it says nothing.
	inside string
}

func (k releaseKind) protocol() indexers.Protocol {
	if k == kindTorrent {
		return indexers.ProtocolTorrent
	}
	return indexers.ProtocolUsenet
}

// errNotARelease is returned for a file that is neither an NZB nor a torrent.
var errNotARelease = errors.New("That file isn't an NZB or a .torrent file.")

// inspectRelease reads a release file and says what it is. The content decides,
// not the file name: an NZB is XML, a .torrent starts with a bencoded
// dictionary. A file that cannot be read all the way through is refused.
func inspectRelease(data []byte) (releaseInfo, error) {
	trimmed := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	trimmed = bytes.TrimLeft(trimmed, " \t\r\n")
	if len(trimmed) == 0 {
		return releaseInfo{}, errors.New("That file is empty.")
	}
	switch trimmed[0] {
	case '<':
		nzb, err := download.ParseNZB(data)
		if err != nil {
			if strings.Contains(err.Error(), "contains no files") {
				return releaseInfo{}, errors.New("That NZB doesn't list any files.")
			}
			return releaseInfo{}, errors.New("That isn't a working NZB file. Check that you chose the right file.")
		}
		return releaseInfo{kind: kindNZB, size: nzb.TotalBytes(), files: nzb.FileNames(), inside: nzb.ReleaseName()}, nil
	case 'd':
		// An uploaded file is untrusted: the library's decoder has no limit on
		// nesting, so a crafted one is turned away before it gets there.
		if err := bencsafe.Check(data); err != nil {
			return releaseInfo{}, errors.New("That isn't a working .torrent file. Check that you chose the right file.")
		}
		mi, err := metainfo.Load(bytes.NewReader(data))
		if err != nil {
			return releaseInfo{}, errors.New("That isn't a working .torrent file. Check that you chose the right file.")
		}
		info, err := mi.UnmarshalInfo()
		if err != nil {
			return releaseInfo{}, errors.New("That isn't a working .torrent file. Check that you chose the right file.")
		}
		var names []string
		var biggest string
		var biggestSize int64 = -1
		for _, f := range info.UpvertedFiles() {
			names = append(names, strings.Join(f.BestPath(), "/"))
			if path := f.BestPath(); f.Length > biggestSize && len(path) > 0 {
				biggest, biggestSize = path[len(path)-1], f.Length
			}
		}
		if len(names) == 0 || info.TotalLength() <= 0 {
			return releaseInfo{}, errors.New("That torrent has no files in it.")
		}
		// A single-file torrent lists no path of its own: the file is the name.
		for i, n := range names {
			if n == "" {
				names[i] = info.BestName()
			}
		}
		return releaseInfo{kind: kindTorrent, name: info.BestName(), size: info.TotalLength(), files: names, inside: withoutMediaExt(biggest)}, nil
	}
	return releaseInfo{}, errNotARelease
}

// withoutMediaExt is a file name without the ending of a video, an audio file or a
// book, so a release name is not read with ".mkv" on the end.
func withoutMediaExt(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".mkv", ".mp4", ".avi", ".m4v", ".ts", ".m2ts", ".wmv", ".mov", ".mpg", ".mpeg", ".iso", ".flac", ".mp3", ".m4a", ".m4b", ".ogg", ".opus", ".epub", ".mobi", ".azw3", ".pdf", ".cbz", ".cbr":
		return strings.TrimSuffix(file, filepath.Ext(file))
	}
	return file
}

// magnetInfo checks a magnet link and returns the name it carries.
func magnetInfo(link string) (name string, err error) {
	m, err := metainfo.ParseMagnetUri(link)
	if err != nil || m.InfoHash == (metainfo.Hash{}) {
		return "", errors.New("That magnet link isn't valid. It needs to contain the torrent's hash (xt=urn:btih:...).")
	}
	return strings.TrimSpace(m.DisplayName), nil
}

// cleanReleaseName makes a name safe to show and to store: control characters
// and surrounding spaces go, and it is cut to a sensible length.
func cleanReleaseName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if len([]rune(name)) > maxManualName {
		name = string([]rune(name)[:maxManualName])
	}
	return name
}

// nameFromFile is a release name from a file name: the name without .nzb or
// .torrent.
func nameFromFile(file string) string {
	file = file[strings.LastIndexAny(file, `/\`)+1:]
	lower := strings.ToLower(file)
	for _, ext := range []string{".nzb", ".torrent"} {
		if strings.HasSuffix(lower, ext) {
			file = file[:len(file)-len(ext)]
			break
		}
	}
	return cleanReleaseName(file)
}

// nameFromAddress is a release name from the last part of an address, "" when
// that part says nothing (an API call, a script).
func nameFromAddress(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	last := u.Path[strings.LastIndex(u.Path, "/")+1:]
	name := nameFromFile(last)
	switch strings.ToLower(name) {
	case "", "api", "download", "getnzb", "get", "nzb", "torrent", "index", "index.php", "download.php":
		return ""
	}
	if !strings.ContainsAny(name, " .-_") {
		return "" // a bare id or hash, not a release name
	}
	return name
}

// chooseReleaseName picks the name the download goes by: the one the person
// typed, else the best of the names found in the file or address, else a plain
// placeholder. The names are given in the order to prefer when they are equally
// good.
func chooseReleaseName(typed string, candidates ...string) string {
	if n := cleanReleaseName(typed); n != "" {
		return n
	}
	best, bestScore := "", -1
	for _, c := range candidates {
		n := cleanReleaseName(c)
		if n == "" {
			continue
		}
		if score := releaseNameScore(n); score > bestScore {
			best, bestScore = n, score
		}
	}
	if best != "" {
		return best
	}
	return "Download added by hand"
}

// releaseNameScore says how much a name tells about a release: the quality, the
// source, the year, the season and episode and the group each count. A file
// called "m2" scores nothing, a real release name scores a lot, so the name
// read from inside an NZB beats a file name like that.
func releaseNameScore(name string) int {
	r := parser.Parse(name)
	score := 0
	for _, has := range []bool{
		r.Resolution != "", r.Source != "", r.Codec != "", r.Year > 0,
		r.Season > 0 || len(r.Episodes) > 0 || r.AirDate != "", r.Group != "", r.AudioCodec != "",
	} {
		if has {
			score++
		}
	}
	return score
}

// --- the request ---

// manualTarget is what the download is for, as the person picked it.
type manualTarget struct {
	movieID    int64
	seriesID   int64
	season     int // 0 is the specials
	episode    int // 0 is the whole season
	albumID    int64
	bookID     int64
	bookFormat books.Format
	// otherID is an item of the Other library; newOther asks for a new one,
	// named otherName (or after the release when that is empty).
	otherID   int64
	newOther  bool
	otherName string
}

type manualDownloadResponse struct {
	QueueID      int64  `json:"queueId"`
	ReleaseTitle string `json:"releaseTitle"`
	Protocol     string `json:"protocol"`
	SizeBytes    int64  `json:"sizeBytes"`
}

// handleManualDownload adds an NZB, a torrent or a magnet link to the download
// line for a chosen movie, episode, album or book. The request is a multipart form:
//
//	file    an .nzb or .torrent file (or url, not both)
//	url     an address of an NZB or .torrent file, or a magnet link
//	name    the release name (optional; the file or address gives one)
//	movieId, or seriesId with season and episode (episode 0 = the whole season),
//	or albumId, or bookId with format ("ebook" or "audiobook"),
//	or otherId (an Other item), or other=new with an optional otherName
func (s *Server) handleManualDownload(w http.ResponseWriter, r *http.Request) {
	allowLongUpload(w)
	r.Body = http.MaxBytesReader(w, r.Body, maxManualNZB+(1<<20))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("That file is too large. NZB files can be up to %d MB and .torrent files up to %d MB.", maxManualNZB>>20, maxManualTorrent>>20))
			return
		}
		writeError(w, http.StatusBadRequest, "Send a file or an address, with the title it is for.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	target, problem := parseManualTarget(r)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	address := strings.TrimSpace(r.FormValue("url"))
	var fileHeader *multipart.FileHeader
	if r.MultipartForm != nil {
		if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
			fileHeader = fh[0]
		}
	}
	switch {
	case fileHeader != nil && address != "":
		writeError(w, http.StatusBadRequest, "Choose a file or paste an address, not both.")
		return
	case fileHeader == nil && address == "":
		writeError(w, http.StatusBadRequest, "Choose an NZB or .torrent file, or paste an address or magnet link.")
		return
	case len(address) > maxManualAddress:
		writeError(w, http.StatusBadRequest, "That address is too long.")
		return
	}

	// The title comes first: nothing is stored for one that isn't there.
	var movie library.Movie
	var series library.Series
	var album music.Album
	var artist music.Artist
	var book books.Book
	var otherItem other.Item
	switch {
	case target.albumID > 0:
		if !s.requireModule(w, moduleMusic) {
			return
		}
		a, err := s.MusicRepo.GetAlbum(target.albumID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That album isn't in your library.")
			return
		}
		if err == nil {
			artist, err = s.MusicRepo.GetArtist(a.ArtistID)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		album = a
	case target.otherID > 0 || target.newOther:
		if !s.requireModule(w, moduleOther) {
			return
		}
		if target.otherID > 0 {
			o, err := s.OtherRepo.Get(target.otherID)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "That item isn't in your library.")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			otherItem = o
		}
	case target.bookID > 0:
		module := moduleEbooks
		if target.bookFormat == books.Audiobook {
			module = moduleAudiobooks
		}
		if !s.requireModule(w, module) {
			return
		}
		b, err := s.BookRepo.Get(target.bookID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That book isn't in your library.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !b.Wants(target.bookFormat) {
			writeError(w, http.StatusConflict, "This book isn't kept in that format. Turn that format on for the book first.")
			return
		}
		book = b
	case target.movieID > 0:
		if !s.requireModule(w, moduleMovies) {
			return
		}
		m, err := s.MovieRepo.Get(target.movieID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That movie isn't in your library.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		movie = m
	default:
		if !s.requireModule(w, moduleTV) {
			return
		}
		sr, err := s.MovieRepo.GetSeries(target.seriesID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That show isn't in your library.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		series = sr
		if target.episode > 0 {
			if _, err := s.MovieRepo.GetEpisode(series.ID, target.season, target.episode); err != nil {
				writeError(w, http.StatusNotFound, "That episode isn't listed for this show. Refresh the show first if it is new.")
				return
			}
		}
	}

	src, status, err := s.readManualSource(r.Context(), fileHeader, address, r.FormValue("name"))
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	if target.seriesID > 0 {
		if msg := tvTargetMismatch(src.title, target); msg != "" {
			s.removeUpload(src.ref)
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}

	createdOther := false
	if target.newOther {
		// A new item is named by the person, else after the release.
		name := target.otherName
		if name == "" {
			name = otherNameFromRelease(src.title)
		}
		userID, _ := requester(r)
		o := addOrigin(r)
		made, aerr := s.OtherRepo.Add(name, userID, o.Kind, o.Name)
		switch {
		case errors.Is(aerr, other.ErrExists):
			// The same name is the same item: the download goes into it.
			made, aerr = s.OtherRepo.FindByName(name)
		default:
			createdOther = aerr == nil
		}
		if aerr != nil {
			s.removeUpload(src.ref)
			if !writeOtherNameError(w, aerr) {
				writeError(w, http.StatusInternalServerError, aerr.Error())
			}
			return
		}
		otherItem = made
	}

	var queueID int64
	switch {
	case otherItem.ID > 0:
		queueID, err = s.grabOther(otherItem, src.title, src.ref, src.size, src.protocol, userOrigin(r, origin.Manual))
	case target.albumID > 0:
		queueID, err = s.grabAlbum(artist, album, src.title, src.ref, src.size, src.protocol, grabPicked, userOrigin(r, origin.Manual))
	case target.bookID > 0:
		queueID, err = s.grabBook(book, target.bookFormat, src.title, src.ref, src.size, src.protocol, queue.PriorityManual, userOrigin(r, origin.Manual))
	case target.movieID > 0:
		queueID, err = s.grabRelease(movie, src.title, src.ref, src.size, src.protocol)
	default:
		queueID, err = s.grabTVRelease(series, target.season, target.episode, src.title, src.ref, src.size, src.protocol)
	}
	if err != nil {
		s.removeUpload(src.ref)
		if createdOther {
			_ = s.OtherRepo.Delete(otherItem.ID) // nothing was added, so no empty item is left behind
		}
		failed := http.StatusInternalServerError
		if target.bookID > 0 || otherItem.ID > 0 {
			failed = http.StatusConflict // a book's grab only fails because a download is already there
		}
		writeGrabError(w, err, failed)
		return
	}
	writeJSON(w, http.StatusAccepted, manualDownloadResponse{
		QueueID: queueID, ReleaseTitle: src.title, Protocol: string(src.protocol), SizeBytes: src.size,
	})
}

func parseManualTarget(r *http.Request) (manualTarget, string) {
	num := func(key string) (int, bool) {
		v := strings.TrimSpace(r.FormValue(key))
		if v == "" {
			return 0, false
		}
		n, err := strconv.Atoi(v)
		return n, err == nil && n >= 0 && n < 100000
	}
	movieID, hasMovie := num("movieId")
	seriesID, hasSeries := num("seriesId")
	albumID, hasAlbum := num("albumId")
	bookID, hasBook := num("bookId")
	otherID, hasOther := num("otherId")
	newOther := strings.TrimSpace(r.FormValue("other")) == "new"
	chosen := 0
	for _, has := range []bool{hasMovie, hasSeries, hasAlbum, hasBook, hasOther || newOther} {
		if has {
			chosen++
		}
	}
	switch {
	case chosen > 1:
		return manualTarget{}, "Choose one title for this download, not both."
	case hasOther && otherID > 0:
		return manualTarget{otherID: int64(otherID)}, ""
	case newOther:
		return manualTarget{newOther: true, otherName: cleanReleaseName(r.FormValue("otherName"))}, ""
	case hasAlbum && albumID > 0:
		return manualTarget{albumID: int64(albumID)}, ""

	case hasBook && bookID > 0:
		f := books.Format(strings.TrimSpace(r.FormValue("format")))
		if !f.Valid() {
			return manualTarget{}, `Choose the format: "ebook" or "audiobook".`
		}
		return manualTarget{bookID: int64(bookID), bookFormat: f}, ""
	case hasMovie && movieID > 0:
		return manualTarget{movieID: int64(movieID)}, ""
	case hasSeries && seriesID > 0:
		season, ok := num("season")
		if !ok {
			return manualTarget{}, "Choose the season this is for."
		}
		episode, _ := num("episode") // left out = the whole season
		if episode == 0 && season == 0 {
			return manualTarget{}, "Choose an episode of the specials, or a season from 1."
		}
		return manualTarget{seriesID: int64(seriesID), season: season, episode: episode}, ""
	}
	return manualTarget{}, "Choose the movie, show, album, book or item this download is for."
}

// tvTargetMismatch says so when the release name names another season or
// episode than the one the person picked. A name with no season or episode
// in it (random names, and most names from an NZB's file) fits any choice.
func tvTargetMismatch(title string, t manualTarget) string {
	season, episodes := resolveTVTarget(title, -1, 0)
	if season == -1 {
		return "" // the name says nothing about the season
	}
	if lo, hi := releaseSeasonRange(title, season); hi > lo {
		// A pack of several seasons, or of the whole series.
		switch {
		case t.episode > 0:
			return fmt.Sprintf("The name says this holds several seasons, but you chose episode %d.", t.episode)
		case t.season < lo || t.season > hi:
			return fmt.Sprintf("The name says seasons %d to %d, but you chose season %d.", lo, min(hi, 99), t.season)
		}
		return ""
	}
	if season != t.season {
		return fmt.Sprintf("The name says season %d, but you chose season %d.", season, t.season)
	}
	switch {
	case t.episode == 0 && len(episodes) > 0:
		return fmt.Sprintf("The name says this is an episode, but you chose the whole of season %d.", t.season)
	case t.episode > 0 && len(episodes) == 0:
		return fmt.Sprintf("The name says this is a whole season, but you chose episode %d.", t.episode)
	case t.episode > 0 && !containsInt(episodes, t.episode):
		return fmt.Sprintf("The name says episode %d, but you chose episode %d.", episodes[0], t.episode)
	}
	return ""
}

func containsInt(list []int, n int) bool {
	for _, v := range list {
		if v == n {
			return true
		}
	}
	return false
}

// manualSource is a checked release, ready to go into the queue.
type manualSource struct {
	protocol indexers.Protocol
	ref      string // what the queue keeps: an upload reference or a magnet link
	title    string
	size     int64
}

// readManualSource reads the file or the address the person gave, checks it and
// stores what has to be stored. The returned status goes with an error.
func (s *Server) readManualSource(ctx context.Context, fh *multipart.FileHeader, address, typedName string) (manualSource, int, error) {
	switch {
	case fh != nil:
		f, err := fh.Open()
		if err != nil {
			return manualSource{}, http.StatusBadRequest, errors.New("The upload couldn't be read. Try again.")
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxManualNZB+1))
		if err != nil {
			return manualSource{}, http.StatusBadRequest, errors.New("The upload couldn't be read. Try again.")
		}
		if len(data) > maxManualNZB {
			return manualSource{}, http.StatusRequestEntityTooLarge, fmt.Errorf("That file is too large. NZB files can be up to %d MB and .torrent files up to %d MB.", maxManualNZB>>20, maxManualTorrent>>20)
		}
		return s.acceptFile(data, nameFromFile(fh.Filename), typedName)
	case isMagnetURI(address):
		name, err := magnetInfo(address)
		if err != nil {
			return manualSource{}, http.StatusBadRequest, err
		}
		if !s.torrentReleasesOn() {
			return manualSource{}, http.StatusConflict, errTorrentsDisabled
		}
		return manualSource{protocol: indexers.ProtocolTorrent, ref: address, title: chooseReleaseName(typedName, name, "Magnet link added by hand")}, 0, nil
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return manualSource{}, http.StatusBadRequest, errors.New("Paste a web address that starts with http:// or https://, or a magnet link.")
	}
	fetchCtx, cancel := context.WithTimeout(ctx, manualFetchTimeout)
	defer cancel()
	data, err := fetchURLLimit(fetchCtx, address, maxManualNZB)
	if err != nil {
		return manualSource{}, http.StatusBadGateway, fmt.Errorf("Couldn't get a file from that address: %s", err.Error())
	}
	return s.acceptFile(data, nameFromAddress(address), typedName)
}

// acceptFile checks a release file and stores it.
func (s *Server) acceptFile(data []byte, fileName, typedName string) (manualSource, int, error) {
	info, err := inspectRelease(data)
	if err != nil {
		return manualSource{}, http.StatusBadRequest, err
	}
	if info.kind == kindTorrent && int64(len(data)) > maxManualTorrent {
		return manualSource{}, http.StatusRequestEntityTooLarge, fmt.Errorf("That .torrent file is too large (the limit is %d MB).", maxManualTorrent>>20)
	}
	switch info.kind {
	case kindTorrent:
		if !s.torrentReleasesOn() {
			return manualSource{}, http.StatusConflict, errTorrentsDisabled
		}
	default:
		if servers, err := s.ClientRepo.List(); err == nil && len(servers) == 0 && !s.usenetClientOn() {
			return manualSource{}, http.StatusPreconditionFailed, errors.New("Add your Usenet provider first (Settings > Downloading > Usenet).")
		}
	}
	// The same check the download makes, said now instead of after it was queued.
	if s.rejectExecutables() {
		if err := download.UnsafeInNames(info.files); err != nil {
			return manualSource{}, http.StatusUnprocessableEntity, errors.New(capitalize(err.Error()) + ". You can allow such releases in Settings > Downloading.")
		}
	}
	fallback := ""
	if info.kind == kindNZB && len(info.files) > 0 {
		fallback = stemOf(info.files[0])
	}
	// A torrent's own name comes first; an NZB's file name and what is inside it compete,
	// so "m2.nzb" does not hide the release it holds.
	title := chooseReleaseName(typedName, info.name, fileName, info.inside, fallback)
	ref, err := s.saveUpload(data, string(info.kind))
	if err != nil {
		slog.Error("api: save an uploaded release", "err", err)
		return manualSource{}, http.StatusInternalServerError, errors.New("Couldn't save that file. Check that the settings folder has free space.")
	}
	return manualSource{protocol: info.kind.protocol(), ref: ref, title: title, size: info.size}, 0, nil
}

func stemOf(file string) string {
	file = file[strings.LastIndexAny(file, `/\`)+1:]
	return strings.TrimSuffix(file, filepath.Ext(file))
}
