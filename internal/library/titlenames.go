package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

// A movie's or show's other names (its title where a release was made, such as
// "Der Herr der Ringe") are kept in title_names, keyed by TMDB id like the
// artwork, so a second copy of a title shares them. The library only stores
// them: the api package decides which are worth keeping and fetches them.
// Movie.AltTitles and Series.AltTitles carry them wherever a title is loaded.

// TitleNames is what is known of a title's other names.
type TitleNames struct {
	Names []string
	// Languages is the audio language list the names were picked for.
	Languages string
	CheckedAt time.Time
}

func decodeTitleNames(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	// Names saved before the umlaut spellings were added get them here, so a
	// title already in the library is searched and matched as "Glueck" too.
	return metadata.WithUmlautSpellings(out)
}

// TitleNames returns the saved names of a movie (kind "movie") or show
// (kind "tv"); ok is false for a title nobody has asked TMDB about yet.
func (r *Repo) TitleNames(kind string, tmdbID int) (TitleNames, bool, error) {
	var raw, languages, checked string
	err := r.db.QueryRow(`SELECT names, languages, checked_at FROM title_names WHERE kind = ? AND tmdb_id = ?`, kind, tmdbID).Scan(&raw, &languages, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return TitleNames{}, false, nil
	}
	if err != nil {
		return TitleNames{}, false, fmt.Errorf("read title names: %w", err)
	}
	out := TitleNames{Names: decodeTitleNames(raw), Languages: languages}
	out.CheckedAt, _ = time.Parse(time.RFC3339, checked)
	return out, true, nil
}

// SaveTitleNames records the names picked for a title (none is fine: it says
// the title was looked at), the audio languages they were picked for, and when.
func (r *Repo) SaveTitleNames(kind string, tmdbID int, names []string, languages string, now time.Time) error {
	raw := ""
	if len(names) > 0 {
		b, err := json.Marshal(names)
		if err != nil {
			return fmt.Errorf("save title names: %w", err)
		}
		raw = string(b)
	}
	_, err := r.db.Exec(`INSERT INTO title_names (kind, tmdb_id, names, languages, checked_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (kind, tmdb_id) DO UPDATE SET names = excluded.names, languages = excluded.languages, checked_at = excluded.checked_at`,
		kind, tmdbID, raw, languages, now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("save title names: %w", err)
	}
	return nil
}

// ClearTitleNames forgets every saved name, for when the audio languages that
// chose them change. The names are fetched again as titles are searched for.
func (r *Repo) ClearTitleNames() error {
	if _, err := r.db.Exec(`DELETE FROM title_names`); err != nil {
		return fmt.Errorf("clear title names: %w", err)
	}
	return nil
}
