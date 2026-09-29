package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// Level is how an item event is shown: info, warn or error.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// levelFor is the level of a feed event by its type, for events logged
// without an explicit one.
func levelFor(eventType string) Level {
	switch eventType {
	case "failed":
		return LevelError
	case "blocklisted", "conflict":
		return LevelWarn
	}
	return LevelInfo
}

// maxItemOnlyEvents is how many detailed (item-only) events are kept per
// movie or show; older ones are dropped as new ones arrive.
const maxItemOnlyEvents = 300

// repeatWindow is how many of an item's latest events are checked for one an
// item-only event repeats word for word.
const repeatWindow = 20

// ItemEvent is one event in a movie's or show's own activity log.
type ItemEvent struct {
	MovieID  int64 // one of MovieID/SeriesID is set
	SeriesID int64
	Kind     string // searched, grabbed, download, postprocess, imported, failed, blocklisted, retried, removed...
	Message  string
	Level    Level
	// ItemOnly events are detail for the item's own log (a search, a
	// download step) and are left out of the global activity feed.
	ItemOnly bool
}

// Event is a stored item event.
type Event struct {
	At      string
	Kind    string
	Message string
	Level   Level
}

// LogSeriesActivity is LogActivity for a show's event on the global feed.
func (r *Repo) LogSeriesActivity(seriesID int64, eventType, message string) error {
	return r.LogItemEvent(ItemEvent{SeriesID: seriesID, Kind: eventType, Message: message, Level: levelFor(eventType)})
}

// LogItemActivity is LogActivity for a movie (movieID) or a show
// (seriesID) on the global feed.
func (r *Repo) LogItemActivity(movieID, seriesID int64, eventType, message string) error {
	return r.LogItemEvent(ItemEvent{MovieID: movieID, SeriesID: seriesID, Kind: eventType, Message: message, Level: levelFor(eventType)})
}

// LogItemEvent records an event for a movie or show. An item-only event
// that repeats one of the item's recent events word for word (the same
// search outcome every half hour) only moves that event's time forward, and
// each item keeps at most maxItemOnlyEvents item-only events.
func (r *Repo) LogItemEvent(e ItemEvent) error {
	if e.Level == "" {
		e.Level = LevelInfo
	}
	col, id := "movie_id", e.MovieID
	if id <= 0 {
		col, id = "series_id", e.SeriesID
	}
	if id <= 0 {
		return r.LogActivity(0, e.Kind, e.Message)
	}
	if e.ItemOnly {
		// The same outcome again (the next scheduled search of a title, or of
		// one episode among several) moves the earlier event forward instead
		// of repeating it, as long as it is among the item's recent events.
		var lastID int64
		err := r.db.QueryRow(`SELECT id FROM (SELECT id, event_type, message, item_only FROM activity WHERE `+col+` = ? ORDER BY id DESC LIMIT ?)
			WHERE item_only = 1 AND event_type = ? AND message = ? LIMIT 1`, id, repeatWindow, e.Kind, e.Message).Scan(&lastID)
		switch {
		case err == nil:
			if _, err := r.db.Exec(`UPDATE activity SET created_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, lastID); err != nil {
				return fmt.Errorf("log item event: %w", err)
			}
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("log item event: %w", err)
		}
	}
	var movie, series any
	if e.MovieID > 0 {
		movie = e.MovieID
	}
	if e.SeriesID > 0 {
		series = e.SeriesID
	}
	if _, err := r.db.Exec(`INSERT INTO activity (movie_id, series_id, event_type, message, level, item_only) VALUES (?, ?, ?, ?, ?, ?)`,
		movie, series, e.Kind, e.Message, string(e.Level), e.ItemOnly); err != nil {
		return fmt.Errorf("log item event: %w", err)
	}
	if e.ItemOnly {
		if _, err := r.db.Exec(`DELETE FROM activity WHERE `+col+` = ? AND item_only = 1 AND id NOT IN
			(SELECT id FROM activity WHERE `+col+` = ? AND item_only = 1 ORDER BY id DESC LIMIT ?)`, id, id, maxItemOnlyEvents); err != nil {
			return fmt.Errorf("trim item events: %w", err)
		}
	}
	return nil
}

// MovieEvents returns a movie's events, newest first, at most limit.
func (r *Repo) MovieEvents(movieID int64, limit int) ([]Event, error) {
	return r.itemEvents("movie_id", movieID, limit)
}

// SeriesEvents returns a show's events, newest first, at most limit.
func (r *Repo) SeriesEvents(seriesID int64, limit int) ([]Event, error) {
	return r.itemEvents("series_id", seriesID, limit)
}

func (r *Repo) itemEvents(col string, id int64, limit int) ([]Event, error) {
	rows, err := r.db.Query(`SELECT created_at, event_type, message, level FROM activity WHERE `+col+` = ? ORDER BY created_at DESC, id DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, fmt.Errorf("list item events: %w", err)
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var level string
		if err := rows.Scan(&e.At, &e.Kind, &e.Message, &level); err != nil {
			return nil, fmt.Errorf("scan item event: %w", err)
		}
		e.Level = Level(level)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReleaseEvent records an item-only event for the movie or show a queue
// item belongs to, naming its release: "<prefix>: <release title>".
func (r *Repo) ReleaseEvent(queueID int64, kind string, level Level, prefix string) error {
	var (
		movie, series sql.NullInt64
		release       string
	)
	err := r.db.QueryRow(`SELECT movie_id, series_id, release_title FROM download_queue WHERE id = ?`, queueID).Scan(&movie, &series, &release)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // removed meanwhile
	}
	if err != nil {
		return fmt.Errorf("look up queue item %d: %w", queueID, err)
	}
	return r.LogItemEvent(ItemEvent{MovieID: movie.Int64, SeriesID: series.Int64, Kind: kind, Level: level,
		Message: fmt.Sprintf("%s: %s", prefix, release), ItemOnly: true})
}

// statusEvent records the download steps of a queue item that the global
// feed does not show: the download starting and finishing. A failure to
// record one is logged, never passed on: it must not fail the download.
func (r *Repo) statusEvent(id int64, status Status) {
	var err error
	switch status {
	case StatusDownloading:
		err = r.ReleaseEvent(id, "download", LevelInfo, "Download started")
	case StatusImporting:
		err = r.ReleaseEvent(id, "download", LevelInfo, "Download finished, post-processing")
	}
	if err != nil {
		slog.Warn("queue: record download step", "queueId", id, "status", string(status), "err", err)
	}
}
