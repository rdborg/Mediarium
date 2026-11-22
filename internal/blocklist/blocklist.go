// Package blocklist remembers releases that failed for a reason that is the
// release's fault, so automation does not grab the same dud again.
package blocklist

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// What an entry is blocked for: every title, or the one it is tied to.
const (
	ScopeAll  = "all"
	ScopeItem = "item"
)

// Entry is one blocklisted release.
type Entry struct {
	ID           int64
	ReleaseTitle string
	Protocol     string
	Reason       string
	MovieID      int64 // 0 if not tied to a movie
	SeriesID     int64 // 0 if not tied to a series
	AlbumID      int64 // 0 if not tied to an album
	BookID       int64 // 0 if not tied to a book
	// ReleaseID says which upload of the release this is (see ReleaseID). With
	// one, only that upload is blocked and another with the same name is not.
	// Entries without one (all those from before, and those made where the
	// upload is not known) block every release with that name.
	ReleaseID string
	// A ReleaseID made with PackIDs also carries the size-based id of the same
	// upload (see FallbackID); Add keeps it next to the guid.
	// ItemOnly makes the entry count for the title named by the ids above and
	// for no other. Without it the release is blocked everywhere, as before.
	ItemOnly  bool
	CreatedAt time.Time
}

// Scope names the one title (a movie, a series, an album or a book) a search
// is for, so entries made for that title alone apply to it. The zero Scope is
// a search that is for no particular title: only entries for everywhere apply.
type Scope struct {
	MovieID, SeriesID, AlbumID, BookID int64
}

// tags lists the scope's targets in the form used inside keys.
func (sc Scope) tags() []string {
	var out []string
	if sc.MovieID != 0 {
		out = append(out, fmt.Sprintf("m%d", sc.MovieID))
	}
	if sc.SeriesID != 0 {
		out = append(out, fmt.Sprintf("s%d", sc.SeriesID))
	}
	if sc.AlbumID != 0 {
		out = append(out, fmt.Sprintf("a%d", sc.AlbumID))
	}
	if sc.BookID != 0 {
		out = append(out, fmt.Sprintf("b%d", sc.BookID))
	}
	return out
}

// Scope is the title an entry is tied to (zero when it is not tied to one).
func (e Entry) Scope() Scope {
	return Scope{MovieID: e.MovieID, SeriesID: e.SeriesID, AlbumID: e.AlbumID, BookID: e.BookID}
}

// relSep separates a release's name from its ReleaseID inside a key. The NUL
// bytes keep it from ever being part of a real release title.
const relSep = "\x00rel\x00"

// releaseKey is the key of a release blocklisted everywhere: its name, and
// when the upload is known, which upload.
func releaseKey(releaseTitle, releaseID string) string {
	if releaseID == "" {
		return Key(releaseTitle)
	}
	return Key(releaseTitle) + relSep + releaseID
}

// scopedKey is the key of a release blocklisted for one title only. The NUL
// bytes keep it from ever matching a real release title.
func scopedKey(tag, releaseTitle, releaseID string) string {
	return "\x00item\x00" + tag + "\x00" + releaseKey(releaseTitle, releaseID)
}

// idOfKey is the ReleaseID inside a stored key ("" when it has none).
func idOfKey(titleKey string) string {
	if i := strings.LastIndex(titleKey, relSep); i >= 0 {
		return titleKey[i+len(relSep):]
	}
	return ""
}

var magnetHashRe = regexp.MustCompile(`(?i)[?&]xt=urn:btih:([0-9a-z]{32,40})`)

// ReleaseID names one upload of a release, from what is known of it on both
// sides, when it fails and when it turns up in a search: the torrent's info
// hash when the link is a magnet link, else the indexer and the size. It is ""
// when there is too little to tell uploads apart, and then a blocklist entry
// blocks every release with that name, as it always did.
func ReleaseID(indexerID, size int64, downloadURL string) string {
	if m := magnetHashRe.FindStringSubmatch(downloadURL); m != nil {
		return "h:" + strings.ToLower(m[1])
	}
	if indexerID > 0 && size > 0 {
		return fmt.Sprintf("i%d:%d", indexerID, size)
	}
	return ""
}

// GUIDID names one upload by the id its indexer gave it (the guid of its entry
// in the feed). Unlike ReleaseID it tells apart two uploads of the same size
// from the same indexer. It is "" when the indexer gave no guid.
func GUIDID(indexerID int64, guid string) string {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return ""
	}
	return fmt.Sprintf("g%d:%s", indexerID, guid)
}

// ReleaseIDs lists every way an upload is known by, the most exact first: its
// guid (when the indexer gave one), then what ReleaseID makes of it. A search
// result is looked up under all of them, so entries made before guids were
// kept (by size or info hash) still match. When it has no guid, BlockedAny and
// ReasonOfAny also look it up under FallbackID, which finds an entry that was
// saved under the guid of the same upload (same indexer, same size).
func ReleaseIDs(indexerID int64, guid string, size int64, downloadURL string) []string {
	var out []string
	if id := GUIDID(indexerID, guid); id != "" {
		out = append(out, id)
	}
	if id := ReleaseID(indexerID, size, downloadURL); id != "" {
		out = append(out, id)
	}
	return out
}

// withFallbacks adds FallbackID of each size-based id to the ids of an upload
// that has no guid (none of them is a guid id), so BlockedAny and ReasonOfAny
// also find an entry saved under the guid of the same upload.
func withFallbacks(ids []string) []string {
	for _, id := range ids {
		if strings.HasPrefix(id, "g") {
			return ids
		}
	}
	out := append([]string(nil), ids...)
	for _, id := range ids {
		if f := FallbackID(id); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// weakPrefix marks the id a release without a guid is also looked up under:
// the size-based id (ReleaseID) of the upload, as a fallback for an entry that
// was saved under a guid. It is never the id of an entry itself.
const weakPrefix = "~"

// fbSep joins the two ids in what PackIDs returns. The NUL bytes keep it from
// ever being part of a real guid.
const fbSep = "\x00fb\x00"

// FallbackID is the size-based id (ReleaseID) of an upload with the mark that
// says it is only the fallback for a guid. A release the indexer gave no guid
// is looked up under it as well as under its plain id, so an entry made for
// the same upload while its guid was known still matches (the indexer and the
// size are the same). A release that has a guid never uses it: another guid is
// another upload.
func FallbackID(releaseID string) string {
	if releaseID == "" {
		return ""
	}
	return weakPrefix + releaseID
}

// PackIDs is the id a blocklist entry is saved under, from ReleaseIDs: the
// first one, and when that is a guid, the size-based id too (kept in the
// entry's fallback). Pass the result as Entry.ReleaseID.
func PackIDs(ids []string) string {
	switch {
	case len(ids) == 0:
		return ""
	case len(ids) > 1 && strings.HasPrefix(ids[0], "g") && !strings.HasPrefix(ids[1], weakPrefix):
		return ids[0] + fbSep + ids[1]
	}
	return ids[0]
}

// unpackID splits an Entry.ReleaseID into the id the entry is saved under and
// its fallback id (both may be empty).
func unpackID(packed string) (id, fallback string) {
	id, fallback, _ = strings.Cut(packed, fbSep)
	return id, fallback
}

// Blocked reports whether the release is blocklisted for a search for sc: on
// the blocklist for everywhere, or for that title alone. set is from Keys.
// It only finds entries that block every release of that name; BlockedRelease
// also finds those that block one upload.
func Blocked(set map[string]bool, releaseTitle string, sc Scope) bool {
	return BlockedRelease(set, releaseTitle, "", sc)
}

// BlockedRelease is Blocked for a release whose upload is known (ReleaseID):
// it is blocked by an entry for its name alone, or by one for this upload.
func BlockedRelease(set map[string]bool, releaseTitle, releaseID string, sc Scope) bool {
	if releaseID == "" {
		return BlockedAny(set, releaseTitle, nil, sc)
	}
	return BlockedAny(set, releaseTitle, []string{releaseID}, sc)
}

// BlockedAny is BlockedRelease for an upload known in several ways (see
// ReleaseIDs): it is blocked by an entry for the name alone, or by one for
// any of the ids. An entry for another upload with the same name does not
// block it.
func BlockedAny(set map[string]bool, releaseTitle string, releaseIDs []string, sc Scope) bool {
	ids := append([]string{""}, withFallbacks(releaseIDs)...)
	for _, id := range ids {
		if set[releaseKey(releaseTitle, id)] {
			return true
		}
		for _, tag := range sc.tags() {
			if set[scopedKey(tag, releaseTitle, id)] {
				return true
			}
		}
	}
	return false
}

// Key is how titles are compared: case and surrounding whitespace ignored.
func Key(releaseTitle string) string {
	return strings.ToLower(strings.TrimSpace(releaseTitle))
}

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func nullIfZero(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// Add blocklists a release; adding one that is already blocklisted just
// refreshes its reason and timestamp.
//
// With ItemOnly and a title to tie it to, the release is blocklisted for that
// title alone; an entry for everywhere that the same title made for the same
// release is dropped, or it would still block the release for every title.
// ItemOnly without a title is blocklisted everywhere, as there is nothing to
// limit it to.
func (r *Repo) Add(e Entry) error {
	tags := e.Scope().tags()
	id, fallback := unpackID(e.ReleaseID)
	e.ReleaseID = id
	key, scope := releaseKey(e.ReleaseTitle, e.ReleaseID), ScopeAll
	if e.ItemOnly && len(tags) > 0 {
		key, scope = scopedKey(tags[0], e.ReleaseTitle, e.ReleaseID), ScopeItem
	}
	_, err := r.db.Exec(
		`INSERT INTO blocklist (release_title, title_key, protocol, reason, movie_id, series_id, album_id, book_id, scope, fallback_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (title_key) DO UPDATE SET reason = excluded.reason, fallback_id = excluded.fallback_id, created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		e.ReleaseTitle, key, e.Protocol, e.Reason, nullIfZero(e.MovieID), nullIfZero(e.SeriesID), nullIfZero(e.AlbumID), nullIfZero(e.BookID), scope, fallback,
	)
	if err != nil {
		return fmt.Errorf("blocklist %q: %w", e.ReleaseTitle, err)
	}
	if scope == ScopeItem {
		// Only the entry this same title made: another title's stays, and so
		// does one tied to no title at all, which a person made on purpose
		// for a download that was for no title. (Older album and book
		// entries saved without their ids are tied to them on upgrade.)
		if _, err := r.db.Exec(
			`DELETE FROM blocklist WHERE title_key = ? AND scope = ? AND
			   COALESCE(movie_id, 0) = ? AND COALESCE(series_id, 0) = ? AND COALESCE(album_id, 0) = ? AND COALESCE(book_id, 0) = ?`,
			releaseKey(e.ReleaseTitle, e.ReleaseID), ScopeAll, e.MovieID, e.SeriesID, e.AlbumID, e.BookID,
		); err != nil {
			return fmt.Errorf("narrow blocklist entry %q: %w", e.ReleaseTitle, err)
		}
	}
	return nil
}

// ErrNoTitle is what SetScope answers when an entry is not tied to a title, so
// there is no title to limit it to.
var ErrNoTitle = errors.New("blocklist entry is not tied to a title")

// SetScope switches an entry between everywhere and its own title only. An
// entry of the same release and scope that is already there is replaced by
// this one.
func (r *Repo) SetScope(id int64, itemOnly bool) error {
	var (
		e                          Entry
		scope, titleKey            string
		movie, series, album, book sql.NullInt64
	)
	err := r.db.QueryRow(`SELECT release_title, title_key, scope, movie_id, series_id, album_id, book_id FROM blocklist WHERE id = ?`, id).
		Scan(&e.ReleaseTitle, &titleKey, &scope, &movie, &series, &album, &book)
	if errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil {
		return fmt.Errorf("read blocklist entry %d: %w", id, err)
	}
	e.MovieID, e.SeriesID, e.AlbumID, e.BookID = movie.Int64, series.Int64, album.Int64, book.Int64
	e.ReleaseID = idOfKey(titleKey)
	key, want := releaseKey(e.ReleaseTitle, e.ReleaseID), ScopeAll
	if itemOnly {
		tags := e.Scope().tags()
		if len(tags) == 0 {
			return ErrNoTitle
		}
		key, want = scopedKey(tags[0], e.ReleaseTitle, e.ReleaseID), ScopeItem
	}
	if scope == want {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("switch blocklist entry %d: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM blocklist WHERE title_key = ? AND id != ?`, key, id); err != nil {
		return fmt.Errorf("switch blocklist entry %d: %w", id, err)
	}
	if _, err := tx.Exec(`UPDATE blocklist SET title_key = ?, scope = ? WHERE id = ?`, key, want, id); err != nil {
		return fmt.Errorf("switch blocklist entry %d: %w", id, err)
	}
	return tx.Commit()
}

// Reason returns the reason a release is blocklisted for everywhere, and false
// when it is not on the blocklist like that.
func (r *Repo) Reason(releaseTitle string) (string, bool, error) {
	return r.ReasonOf(releaseTitle, "")
}

// ReasonOf is Reason for one upload of the release: the reason of the entry
// for that upload, or else of the one for the name alone.
func (r *Repo) ReasonOf(releaseTitle, releaseID string) (string, bool, error) {
	if releaseID == "" {
		return r.ReasonOfAny(releaseTitle, nil)
	}
	return r.ReasonOfAny(releaseTitle, []string{releaseID})
}

// ReasonOfAny is ReasonOf for an upload known in several ways (see
// ReleaseIDs): the reason of the entry for one of them (the first listed
// first), or else of the one for the name alone.
func (r *Repo) ReasonOfAny(releaseTitle string, releaseIDs []string) (string, bool, error) {
	keys := make([]any, 0, len(releaseIDs)+1)
	var weak []string
	for _, id := range withFallbacks(releaseIDs) {
		switch {
		case id == "":
		case strings.HasPrefix(id, weakPrefix):
			weak = append(weak, strings.TrimPrefix(id, weakPrefix))
		default:
			keys = append(keys, releaseKey(releaseTitle, id))
		}
	}
	keys = append(keys, Key(releaseTitle))
	var reason string
	// An entry saved under the guid of this upload (same indexer and size, no
	// guid known now) is found by its fallback id.
	for _, fb := range weak {
		rows, err := r.db.Query(`SELECT release_title, reason FROM blocklist WHERE fallback_id = ? AND scope = ?`, fb, ScopeAll)
		if err != nil {
			return "", false, fmt.Errorf("look up blocklist reason of %q: %w", releaseTitle, err)
		}
		found := false
		for rows.Next() {
			var title, why string
			if err := rows.Scan(&title, &why); err != nil {
				rows.Close()
				return "", false, fmt.Errorf("look up blocklist reason of %q: %w", releaseTitle, err)
			}
			if Key(title) == Key(releaseTitle) {
				reason, found = why, true
				break
			}
		}
		rows.Close()
		if found {
			return reason, true, nil
		}
	}
	for _, k := range keys {
		err := r.db.QueryRow(`SELECT reason FROM blocklist WHERE title_key = ? LIMIT 1`, k).Scan(&reason)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return "", false, fmt.Errorf("look up blocklist reason of %q: %w", releaseTitle, err)
		}
		return reason, true, nil
	}
	return "", false, nil
}

// Keys returns the set of blocklisted title keys, for filtering search
// results. A release blocked for one title alone is in it under a key of its
// own, so look releases up with Blocked and not by Key alone.
func (r *Repo) Keys() (map[string]bool, error) {
	rows, err := r.db.Query(`SELECT title_key, release_title, scope, fallback_id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(album_id, 0), COALESCE(book_id, 0) FROM blocklist`)
	if err != nil {
		return nil, fmt.Errorf("list blocklist keys: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var (
			k, title, scope, fallback string
			e                         Entry
		)
		if err := rows.Scan(&k, &title, &scope, &fallback, &e.MovieID, &e.SeriesID, &e.AlbumID, &e.BookID); err != nil {
			return nil, err
		}
		out[k] = true
		if fallback == "" {
			continue
		}
		// The same upload, for a release that comes without a guid.
		if tags := e.Scope().tags(); scope == ScopeItem && len(tags) > 0 {
			out[scopedKey(tags[0], title, FallbackID(fallback))] = true
		} else if scope != ScopeItem {
			out[releaseKey(title, FallbackID(fallback))] = true
		}
	}
	return out, rows.Err()
}

const entryColumns = `id, release_title, title_key, protocol, reason, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(album_id, 0), COALESCE(book_id, 0), scope, created_at`

func scanEntry(rows *sql.Rows) (Entry, error) {
	var (
		e       Entry
		scope   string
		created string
		tkey    string
	)
	if err := rows.Scan(&e.ID, &e.ReleaseTitle, &tkey, &e.Protocol, &e.Reason, &e.MovieID, &e.SeriesID, &e.AlbumID, &e.BookID, &scope, &created); err != nil {
		return e, fmt.Errorf("scan blocklist entry: %w", err)
	}
	e.ItemOnly = scope == ScopeItem
	e.ReleaseID = idOfKey(tkey)
	e.CreatedAt, _ = time.Parse("2006-01-02T15:04:05.000Z", created)
	return e, nil
}

func (r *Repo) List() ([]Entry, error) {
	rows, err := r.db.Query(`SELECT ` + entryColumns + ` FROM blocklist ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list blocklist: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Page returns one page of the blocklist, newest first, and how many entries
// there are in all.
func (r *Repo) Page(limit, offset int) ([]Entry, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM blocklist`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count blocklist: %w", err)
	}
	rows, err := r.db.Query(`SELECT `+entryColumns+` FROM blocklist ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list blocklist: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (r *Repo) Remove(id int64) error {
	res, err := r.db.Exec(`DELETE FROM blocklist WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove blocklist entry %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ReleaseOf is the release title and reason of an entry.
func (r *Repo) ReleaseOf(id int64) (releaseTitle, reason string, err error) {
	err = r.db.QueryRow(`SELECT release_title, reason FROM blocklist WHERE id = ?`, id).Scan(&releaseTitle, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if err != nil {
		return "", "", fmt.Errorf("read blocklist entry %d: %w", id, err)
	}
	return releaseTitle, reason, nil
}

// KeepShort remembers that a person chose to keep this release although its
// file is far shorter than expected, and drops its blocklist entries (made for
// that reason) so it can be grabbed again. It reports how many were dropped.
func (r *Repo) KeepShort(releaseTitle, reason string) (int, error) {
	key := Key(releaseTitle)
	if key == "" {
		return 0, errors.New("keep a release: no release title")
	}
	if _, err := r.db.Exec(`INSERT INTO kept_short_releases (title_key) VALUES (?) ON CONFLICT (title_key) DO NOTHING`, key); err != nil {
		return 0, fmt.Errorf("keep release %q: %w", releaseTitle, err)
	}
	// The titles are compared here: SQLite's lower() only folds ASCII, so a
	// title with a capital like "É" would never match the key.
	rows, err := r.db.Query(`SELECT id, release_title FROM blocklist WHERE reason = ?`, reason)
	if err != nil {
		return 0, fmt.Errorf("unblock release %q: %w", releaseTitle, err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			rows.Close()
			return 0, fmt.Errorf("unblock release %q: %w", releaseTitle, err)
		}
		if Key(title) == key {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("unblock release %q: %w", releaseTitle, err)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := r.db.Exec(`DELETE FROM blocklist WHERE id = ?`, id); err != nil {
			return 0, fmt.Errorf("unblock release %q: %w", releaseTitle, err)
		}
	}
	return len(ids), nil
}

// KeptShort says whether a person chose to keep this release despite its length.
// A database error counts as no: the normal check then applies.
func (r *Repo) KeptShort(releaseTitle string) bool {
	var one int
	err := r.db.QueryRow(`SELECT 1 FROM kept_short_releases WHERE title_key = ?`, Key(releaseTitle)).Scan(&one)
	return err == nil
}

func (r *Repo) Clear() error {
	if _, err := r.db.Exec(`DELETE FROM blocklist`); err != nil {
		return fmt.Errorf("clear blocklist: %w", err)
	}
	return nil
}

// CountRecent counts entries for a movie (movieID) or series (seriesID)
// added since the given time — the guard that stops automatic retries from
// chewing through an endless list of bad releases.
func (r *Repo) CountRecent(movieID, seriesID int64, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM blocklist WHERE created_at >= ? AND ((? != 0 AND movie_id = ?) OR (? != 0 AND series_id = ?))`,
		since.UTC().Format("2006-01-02T15:04:05.000Z"), movieID, movieID, seriesID, seriesID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count recent blocklist entries: %w", err)
	}
	return n, nil
}
