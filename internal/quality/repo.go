package quality

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrInUse is returned when deleting a profile that is still the default or
// assigned to library items.
var ErrInUse = errors.New("quality profile is in use")

// ErrInvalid wraps validation failures (bad name, unknown tier, cutoff not in
// the allowed list) so the API can report them as user errors.
var ErrInvalid = errors.New("invalid quality profile")

const (
	maxNameLen  = 60
	maxTerms    = 50
	maxTermLen  = 60
	maxAbsScore = 10000
)

// Repo stores user-editable profiles.
type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// Validate checks a profile is coherent: named, at least one allowed tier,
// every tier real ("Unknown" is valid: it means releases whose names carry
// no recognisable quality), and the cutoff one of the allowed tiers. It returns the profile with tiers de-duplicated and ordered
// worst-to-best.
func Validate(p Profile) (Profile, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > maxNameLen {
		return p, fmt.Errorf("%w: name must be 1-%d characters", ErrInvalid, maxNameLen)
	}
	valid := map[Tier]bool{}
	for _, t := range AllTiers() {
		valid[t] = true
	}
	seen := map[Tier]bool{}
	for _, t := range p.Allowed {
		if !valid[t] {
			return p, fmt.Errorf("%w: %q is not a quality tier", ErrInvalid, t)
		}
		seen[t] = true
	}
	if len(seen) == 0 {
		return p, fmt.Errorf("%w: pick at least one allowed quality", ErrInvalid)
	}
	if !seen[p.Cutoff] {
		return p, fmt.Errorf("%w: the cutoff must be one of the allowed qualities", ErrInvalid)
	}
	p.Allowed = nil
	for _, t := range AllTiers() {
		if seen[t] {
			p.Allowed = append(p.Allowed, t)
		}
	}
	var err error
	if p.MustContain, err = cleanTerms("required", p.MustContain); err != nil {
		return p, err
	}
	if p.MustNotContain, err = cleanTerms("excluded", p.MustNotContain); err != nil {
		return p, err
	}
	terms := make([]string, len(p.Preferred))
	for i, pr := range p.Preferred {
		terms[i] = pr.Term
		if pr.Score < -maxAbsScore || pr.Score > maxAbsScore {
			return p, fmt.Errorf("%w: the score for %q must be between -%d and %d", ErrInvalid, pr.Term, maxAbsScore, maxAbsScore)
		}
	}
	if _, err := cleanTerms("preferred", terms); err != nil {
		return p, err
	}
	kept := p.Preferred[:0:0]
	seenPref := map[string]bool{}
	for _, pr := range p.Preferred {
		pr.Term = strings.TrimSpace(pr.Term)
		if pr.Term == "" || seenPref[strings.ToLower(pr.Term)] {
			continue
		}
		seenPref[strings.ToLower(pr.Term)] = true
		kept = append(kept, pr)
	}
	p.Preferred = kept
	return p, nil
}

// cleanTerms trims, de-duplicates (case-insensitively) and bounds a list of
// release-title terms. Terms may not contain "|" or line breaks, which the
// storage format uses as separators.
func cleanTerms(what string, terms []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		if len(t) > maxTermLen || strings.ContainsAny(t, "|\n\r") {
			return nil, fmt.Errorf("%w: a %s term must be under %d characters and contain no '|' or line breaks", ErrInvalid, what, maxTermLen)
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	if len(out) > maxTerms {
		return nil, fmt.Errorf("%w: at most %d %s terms", ErrInvalid, maxTerms, what)
	}
	return out, nil
}

func encodeLines(lines []string) string { return strings.Join(lines, "\n") }

func decodeLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func encodePreferred(prefs []Preferred) string {
	lines := make([]string, len(prefs))
	for i, p := range prefs {
		lines[i] = fmt.Sprintf("%s|%d", p.Term, p.Score)
	}
	return encodeLines(lines)
}

func decodePreferred(s string) []Preferred {
	var out []Preferred
	for _, line := range decodeLines(s) {
		term, score, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(score)
		out = append(out, Preferred{Term: term, Score: n})
	}
	return out
}

func encodeTiers(tiers []Tier) string {
	parts := make([]string, len(tiers))
	for i, t := range tiers {
		parts[i] = string(t)
	}
	return strings.Join(parts, ",")
}

func decodeTiers(s string) []Tier {
	if s == "" {
		return nil
	}
	var out []Tier
	for _, part := range strings.Split(s, ",") {
		out = append(out, Tier(part))
	}
	return out
}

const selectProfile = `SELECT id, name, allowed_tiers, cutoff, upgrade_allowed, must_contain, must_not_contain, preferred FROM quality_profiles`

func scanProfile(scan func(dest ...any) error) (Profile, error) {
	var (
		p                               Profile
		allowed, cutoff                 string
		mustContain, mustNot, preferred string
	)
	if err := scan(&p.ID, &p.Name, &allowed, &cutoff, &p.UpgradeAllowed, &mustContain, &mustNot, &preferred); err != nil {
		return Profile{}, err
	}
	p.Allowed = decodeTiers(allowed)
	p.Cutoff = Tier(cutoff)
	p.MustContain = decodeLines(mustContain)
	p.MustNotContain = decodeLines(mustNot)
	p.Preferred = decodePreferred(preferred)
	return p, nil
}

func (r *Repo) List() ([]Profile, error) {
	rows, err := r.db.Query(selectProfile + ` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list quality profiles: %w", err)
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		p, err := scanProfile(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan quality profile: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) Get(id int64) (Profile, error) {
	p, err := scanProfile(r.db.QueryRow(selectProfile+` WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, err
	}
	if err != nil {
		return Profile{}, fmt.Errorf("get quality profile %d: %w", id, err)
	}
	return p, nil
}

func (r *Repo) Create(p Profile) (Profile, error) {
	p, err := Validate(p)
	if err != nil {
		return Profile{}, err
	}
	res, err := r.db.Exec(
		`INSERT INTO quality_profiles (name, allowed_tiers, cutoff, upgrade_allowed, must_contain, must_not_contain, preferred) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Name, encodeTiers(p.Allowed), string(p.Cutoff), p.UpgradeAllowed,
		encodeLines(p.MustContain), encodeLines(p.MustNotContain), encodePreferred(p.Preferred),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Profile{}, fmt.Errorf("%w: a profile named %q already exists", ErrInvalid, p.Name)
		}
		return Profile{}, fmt.Errorf("insert quality profile: %w", err)
	}
	p.ID, _ = res.LastInsertId()
	return p, nil
}

func (r *Repo) Update(p Profile) (Profile, error) {
	p, err := Validate(p)
	if err != nil {
		return Profile{}, err
	}
	res, err := r.db.Exec(
		`UPDATE quality_profiles SET name = ?, allowed_tiers = ?, cutoff = ?, upgrade_allowed = ?, must_contain = ?, must_not_contain = ?, preferred = ? WHERE id = ?`,
		p.Name, encodeTiers(p.Allowed), string(p.Cutoff), p.UpgradeAllowed,
		encodeLines(p.MustContain), encodeLines(p.MustNotContain), encodePreferred(p.Preferred), p.ID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Profile{}, fmt.Errorf("%w: a profile named %q already exists", ErrInvalid, p.Name)
		}
		return Profile{}, fmt.Errorf("update quality profile %d: %w", p.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Profile{}, sql.ErrNoRows
	}
	return p, nil
}

// Usage returns how many movies and series are explicitly assigned each
// profile (items on the default profile are not counted).
func (r *Repo) Usage() (map[int64]int, error) {
	out := map[int64]int{}
	for _, table := range []string{"movies", "series"} {
		rows, err := r.db.Query(`SELECT profile_id, COUNT(*) FROM ` + table + ` WHERE profile_id IS NOT NULL GROUP BY profile_id`)
		if err != nil {
			return nil, fmt.Errorf("count profile usage in %s: %w", table, err)
		}
		for rows.Next() {
			var id int64
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] += n
		}
		rows.Close()
	}
	return out, nil
}

// Delete removes a profile that nothing references. defaultID is the
// currently-default profile, which cannot be deleted.
func (r *Repo) Delete(id, defaultID int64) error {
	if id == defaultID {
		return fmt.Errorf("%w: it is the default profile - make another profile the default first", ErrInUse)
	}
	usage, err := r.Usage()
	if err != nil {
		return err
	}
	if n := usage[id]; n > 0 {
		return fmt.Errorf("%w: %d movie(s)/show(s) use it - move them to another profile first", ErrInUse, n)
	}
	res, err := r.db.Exec(`DELETE FROM quality_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete quality profile %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SeedPresets inserts the built-in presets when the table is empty (first
// start) and returns all profiles. Presets are ordinary rows afterwards:
// users can edit or delete them like any other profile.
func (r *Repo) SeedPresets() ([]Profile, error) {
	existing, err := r.List()
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return existing, nil
	}
	presets := Presets()
	keys := make([]string, 0, len(presets))
	for k := range presets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return presetOrder(keys[i]) < presetOrder(keys[j]) })
	for _, k := range keys {
		if _, err := r.Create(presets[k]); err != nil {
			return nil, fmt.Errorf("seed preset %s: %w", k, err)
		}
	}
	return r.List()
}

func presetOrder(key string) int {
	switch key {
	case "any-1080p":
		return 0
	case "ultra-hd":
		return 1
	}
	return 2
}

// PresetName returns the display name of a legacy preset key ("any-1080p"),
// used to carry the old single global setting over to a stored profile.
func PresetName(key string) (string, bool) {
	p, ok := Presets()[key]
	return p.Name, ok
}
