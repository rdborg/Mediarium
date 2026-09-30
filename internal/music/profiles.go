package music

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Editing music profiles follows the rules of the movie and TV quality
// profiles: a name, at least one allowed tier, a cutoff among them, an
// optional fallback chain; a profile that is the default or is used by an
// artist cannot be deleted.

var (
	// ErrProfileInvalid wraps validation failures so the API can report them
	// as user errors.
	ErrProfileInvalid = errors.New("invalid music profile")
	// ErrProfileInUse is returned when deleting a profile that is the default
	// or is assigned to artists.
	ErrProfileInUse = errors.New("music profile is in use")
)

const (
	maxProfileNameLen = 60
	maxFallbacks      = 20
)

// ValidateProfile checks a profile is coherent and returns it cleaned: the
// name trimmed, the allowed tiers de-duplicated and ordered worst to best,
// the fallback list without zero, self and repeated ids.
func ValidateProfile(p Profile) (Profile, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > maxProfileNameLen {
		return p, fmt.Errorf("%w: name must be 1-%d characters", ErrProfileInvalid, maxProfileNameLen)
	}
	seen := map[Tier]bool{}
	for _, t := range p.Allowed {
		if !ValidTier(string(t)) {
			return p, fmt.Errorf("%w: %q is not a quality tier", ErrProfileInvalid, t)
		}
		seen[t] = true
	}
	if len(seen) == 0 {
		return p, fmt.Errorf("%w: pick at least one allowed quality", ErrProfileInvalid)
	}
	if !seen[p.Cutoff] {
		return p, fmt.Errorf("%w: the cutoff must be one of the allowed qualities", ErrProfileInvalid)
	}
	p.Allowed = nil
	for _, t := range AllTiers() {
		if seen[t] {
			p.Allowed = append(p.Allowed, t)
		}
	}
	p.Fallback = cleanFallbackIDs(p.ID, p.Fallback)
	return p, nil
}

func cleanFallbackIDs(self int64, ids []int64) []int64 {
	out := []int64{}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || id == self || seen[id] || len(out) >= maxFallbacks {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func encodeFallbackIDs(ids []int64) string {
	if len(ids) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// existingProfileIDs keeps only the ids of profiles that exist.
func (r *Repo) existingProfileIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	all, err := r.ListProfiles()
	if err != nil {
		return nil, err
	}
	exists := map[int64]bool{}
	for _, p := range all {
		exists[p.ID] = true
	}
	out := []int64{}
	for _, id := range ids {
		if exists[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// GetProfile returns one profile (its fallback ids, not resolved); the
// error wraps sql.ErrNoRows when there is none.
func (r *Repo) GetProfile(id int64) (Profile, error) {
	all, err := r.ListProfiles()
	if err != nil {
		return Profile{}, err
	}
	for _, p := range all {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("get music profile %d: %w", id, sql.ErrNoRows)
}

// CreateProfile validates and stores a new profile.
func (r *Repo) CreateProfile(p Profile) (Profile, error) {
	p.ID = 0
	p, err := ValidateProfile(p)
	if err != nil {
		return Profile{}, err
	}
	if p.Fallback, err = r.existingProfileIDs(p.Fallback); err != nil {
		return Profile{}, err
	}
	res, err := r.db.Exec(`INSERT INTO music_profiles (name, allowed_tiers, cutoff, upgrade_allowed, fallback) VALUES (?, ?, ?, ?, ?)`,
		p.Name, joinTiers(p.Allowed), string(p.Cutoff), p.UpgradeAllowed, encodeFallbackIDs(p.Fallback))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Profile{}, fmt.Errorf("%w: a profile named %q already exists", ErrProfileInvalid, p.Name)
		}
		return Profile{}, fmt.Errorf("insert music profile: %w", err)
	}
	p.ID, _ = res.LastInsertId()
	return p, nil
}

// UpdateProfile validates and replaces a profile.
func (r *Repo) UpdateProfile(p Profile) (Profile, error) {
	p, err := ValidateProfile(p)
	if err != nil {
		return Profile{}, err
	}
	if p.Fallback, err = r.existingProfileIDs(p.Fallback); err != nil {
		return Profile{}, err
	}
	res, err := r.db.Exec(`UPDATE music_profiles SET name = ?, allowed_tiers = ?, cutoff = ?, upgrade_allowed = ?, fallback = ? WHERE id = ?`,
		p.Name, joinTiers(p.Allowed), string(p.Cutoff), p.UpgradeAllowed, encodeFallbackIDs(p.Fallback), p.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Profile{}, fmt.Errorf("%w: a profile named %q already exists", ErrProfileInvalid, p.Name)
		}
		return Profile{}, fmt.Errorf("update music profile %d: %w", p.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Profile{}, fmt.Errorf("update music profile %d: %w", p.ID, sql.ErrNoRows)
	}
	return p, nil
}

// ProfileUsage returns how many artists are explicitly assigned each
// profile (artists on the default profile are not counted).
func (r *Repo) ProfileUsage() (map[int64]int, error) {
	rows, err := r.db.Query(`SELECT profile_id, COUNT(*) FROM artists WHERE profile_id IS NOT NULL GROUP BY profile_id`)
	if err != nil {
		return nil, fmt.Errorf("count music profile usage: %w", err)
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan music profile usage: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// DeleteProfile removes a profile nothing references. defaultID is the
// profile that is currently the default, which cannot be deleted. The
// profile is also taken out of every other profile's fallback list.
func (r *Repo) DeleteProfile(id, defaultID int64) error {
	if id == defaultID {
		return fmt.Errorf("%w: this is the default profile. Make another profile the default first", ErrProfileInUse)
	}
	usage, err := r.ProfileUsage()
	if err != nil {
		return err
	}
	if n := usage[id]; n > 0 {
		return fmt.Errorf("%w: %d artist(s) use it. Move them to another profile first", ErrProfileInUse, n)
	}
	res, err := r.db.Exec(`DELETE FROM music_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete music profile %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete music profile %d: %w", id, sql.ErrNoRows)
	}
	all, err := r.ListProfiles()
	if err != nil {
		return err
	}
	for _, p := range all {
		kept := []int64{}
		for _, f := range p.Fallback {
			if f != id {
				kept = append(kept, f)
			}
		}
		if len(kept) == len(p.Fallback) {
			continue
		}
		if _, err := r.db.Exec(`UPDATE music_profiles SET fallback = ? WHERE id = ?`, encodeFallbackIDs(kept), p.ID); err != nil {
			return fmt.Errorf("remove profile %d from the fallback of %q: %w", id, p.Name, err)
		}
	}
	return nil
}

// SetArtistProfile assigns a profile to an artist (0 = the default profile).
func (r *Repo) SetArtistProfile(artistID, profileID int64) error {
	res, err := r.db.Exec(`UPDATE artists SET profile_id = ? WHERE id = ?`, nullID(profileID), artistID)
	if err != nil {
		return fmt.Errorf("set profile of artist %d: %w", artistID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set profile of artist %d: %w", artistID, sql.ErrNoRows)
	}
	return nil
}
