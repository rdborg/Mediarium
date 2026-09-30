package library

import "fmt"

// Kinds of title a bulk change can name.
const (
	KindMovie = "movie"
	KindShow  = "tv"
)

// Ref names one title in the library: a movie or a show.
type Ref struct {
	Kind string // KindMovie or KindShow
	ID   int64
}

// BulkField is a setting that can be changed on many titles at once.
type BulkField int

const (
	BulkMonitored  BulkField = iota + 1 // value: bool
	BulkNoUpgrade                       // value: bool
	BulkProfile                         // value: int64, 0 = the default profile
	BulkSourcePref                      // value: string, "" = follow the default
)

// bulkSQL is the statement for each field on movies and on shows. The column
// is part of the statement text, never of the arguments, so nothing a caller
// sends can reach the SQL.
var bulkSQL = map[BulkField]struct{ movie, show string }{
	BulkMonitored:  {`UPDATE movies SET monitored = ? WHERE id = ?`, `UPDATE series SET monitored = ? WHERE id = ?`},
	BulkNoUpgrade:  {`UPDATE movies SET no_upgrade = ? WHERE id = ?`, `UPDATE series SET no_upgrade = ? WHERE id = ?`},
	BulkProfile:    {`UPDATE movies SET profile_id = NULLIF(?, 0) WHERE id = ?`, `UPDATE series SET profile_id = NULLIF(?, 0) WHERE id = ?`},
	BulkSourcePref: {`UPDATE movies SET source_pref = ? WHERE id = ?`, `UPDATE series SET source_pref = ? WHERE id = ?`},
}

// BulkSet changes one setting on every title in refs in a single transaction
// and returns the titles that are not in the library (any more). Those are
// skipped, not an error: the rest are still changed. A database error undoes
// the whole change.
func (r *Repo) BulkSet(field BulkField, value any, refs []Ref) (missing []Ref, err error) {
	stmts, ok := bulkSQL[field]
	if !ok {
		return nil, fmt.Errorf("bulk change: unknown setting %d", field)
	}
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin bulk change: %w", err)
	}
	defer tx.Rollback()
	for _, ref := range refs {
		var query string
		switch ref.Kind {
		case KindMovie:
			query = stmts.movie
		case KindShow:
			query = stmts.show
		default:
			return nil, fmt.Errorf("bulk change: unknown kind %q", ref.Kind)
		}
		res, err := tx.Exec(query, value, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("bulk change of %s %d: %w", ref.Kind, ref.ID, err)
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			missing = append(missing, ref)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bulk change: %w", err)
	}
	return missing, nil
}
