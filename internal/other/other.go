// Package other keeps the "Other" media type: items that are not movies,
// shows, music or books. An item is made by name and is never matched against
// a metadata service; its files sit in a folder named after it.
package other

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Status of an item.
const (
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusDownloaded  = "downloaded"
)

// MaxNameLen is the longest name taken.
const MaxNameLen = 200

// ErrExists is returned when an item with the same name is already there.
var ErrExists = errors.New("an item with that name already exists")

// ErrName is returned for a name that is empty or too long.
var ErrName = errors.New("give the item a name of up to 200 characters")

// Item is one thing in the Other library.
type Item struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Path       string `json:"path"`
	FileCount  int    `json:"fileCount"`
	SizeBytes  int64  `json:"sizeBytes"`
	AddedAt    string `json:"addedAt"`
	AddedBy    int64  `json:"addedBy"`
	OriginKind string `json:"originKind,omitempty"`
	OriginName string `json:"originName,omitempty"`
}

// CleanName trims a name, folds its white space and checks its length.
func CleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > MaxNameLen {
		return "", ErrName
	}
	return name, nil
}

// Repo stores the items.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo over db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const columns = `id, name, status, path, file_count, size_bytes, added_at, COALESCE(added_by, 0), origin_kind, origin_name`

func scan(sc interface{ Scan(...any) error }) (Item, error) {
	var it Item
	err := sc.Scan(&it.ID, &it.Name, &it.Status, &it.Path, &it.FileCount, &it.SizeBytes, &it.AddedAt, &it.AddedBy, &it.OriginKind, &it.OriginName)
	return it, err
}

// Add stores a new item. The name is cleaned; a name already in the library
// (ignoring case) answers ErrExists.
func (r *Repo) Add(name string, addedBy int64, originKind, originName string) (Item, error) {
	name, err := CleanName(name)
	if err != nil {
		return Item{}, err
	}
	var exists int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM other_items WHERE name = ? COLLATE NOCASE`, name).Scan(&exists); err != nil {
		return Item{}, fmt.Errorf("check for an item named %q: %w", name, err)
	}
	if exists > 0 {
		return Item{}, ErrExists
	}
	var by any
	if addedBy > 0 {
		by = addedBy
	}
	res, err := r.db.Exec(`INSERT INTO other_items (name, added_by, origin_kind, origin_name) VALUES (?, ?, ?, ?)`, name, by, originKind, originName)
	if err != nil {
		return Item{}, fmt.Errorf("add item: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.Get(id)
}

// Get returns one item, or sql.ErrNoRows.
func (r *Repo) Get(id int64) (Item, error) {
	it, err := scan(r.db.QueryRow(`SELECT `+columns+` FROM other_items WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Item{}, err
		}
		return Item{}, fmt.Errorf("get item %d: %w", id, err)
	}
	return it, nil
}

// FindByName returns the item with this name (ignoring case), or sql.ErrNoRows.
func (r *Repo) FindByName(name string) (Item, error) {
	it, err := scan(r.db.QueryRow(`SELECT `+columns+` FROM other_items WHERE name = ? COLLATE NOCASE`, strings.Join(strings.Fields(name), " ")))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Item{}, fmt.Errorf("find item %q: %w", name, err)
	}
	return it, err
}

// List returns every item, A to Z.
func (r *Repo) List() ([]Item, error) {
	rows, err := r.db.Query(`SELECT ` + columns + ` FROM other_items ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("read item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Rename changes an item's name. Its files stay where they are.
func (r *Repo) Rename(id int64, name string) error {
	name, err := CleanName(name)
	if err != nil {
		return err
	}
	var other int64
	if err := r.db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM other_items WHERE name = ? COLLATE NOCASE AND id <> ?`, name, id).Scan(&other); err != nil {
		return fmt.Errorf("check for an item named %q: %w", name, err)
	}
	if other > 0 {
		return ErrExists
	}
	res, err := r.db.Exec(`UPDATE other_items SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("rename item %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetState records the item's status. With a path it also records the folder
// and what it holds; without one only the status changes, so a download that
// fails leaves the files already there in view.
func (r *Repo) SetState(id int64, status, path string, files int, size int64) error {
	var err error
	if path != "" {
		_, err = r.db.Exec(`UPDATE other_items SET status = ?, path = ?, file_count = ?, size_bytes = ? WHERE id = ?`, status, path, files, size, id)
	} else {
		_, err = r.db.Exec(`UPDATE other_items SET status = ? WHERE id = ?`, status, id)
	}
	if err != nil {
		return fmt.Errorf("save the state of item %d: %w", id, err)
	}
	return nil
}

// Delete removes an item (its downloads go with it); files on disk stay.
func (r *Repo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM other_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete item %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
