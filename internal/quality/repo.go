package quality

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	if p.MaxSizeGB < 0 || p.MaxSizeGB > 1000 {
		return p, fmt.Errorf("%w: the largest size must be 0 (no limit) to 1000 GB", ErrInvalid)
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
	p.Fallback = cleanFallback(p.ID, p.Fallback)
	return p, nil
}

// maxFallbacks bounds a profile's fallback list.
const maxFallbacks = 20

// cleanFallback drops ids that cannot be a fallback (zero or negative, the
// profile itself, repeats) and keeps the order. Ids of profiles that do not
// exist are dropped by the repo, which can look them up.
func cleanFallback(self int64, ids []int64) []int64 {
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

func encodeFallback(ids []int64) string {
	if len(ids) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func decodeFallback(s string) []int64 {
	var ids []int64
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return []int64{}
	}
	return cleanFallback(0, ids)
}

// existingFallback keeps only the ids of profiles that exist (a fallback may
// not point at a profile that is gone).
func (r *Repo) existingFallback(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	rows, err := r.db.Query(`SELECT id FROM quality_profiles`)
	if err != nil {
		return nil, fmt.Errorf("list quality profile ids: %w", err)
	}
	defer rows.Close()
	exists := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan quality profile id: %w", err)
		}
		exists[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list quality profile ids: %w", err)
	}
	out := []int64{}
	for _, id := range ids {
		if exists[id] {
			out = append(out, id)
		}
	}
	return out, nil
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

const selectProfile = `SELECT id, name, allowed_tiers, cutoff, upgrade_allowed, must_contain, must_not_contain, preferred, fallback, max_size_gb FROM quality_profiles`

func scanProfile(scan func(dest ...any) error) (Profile, error) {
	var (
		p                               Profile
		allowed, cutoff                 string
		mustContain, mustNot, preferred string
		fallback                        string
	)
	if err := scan(&p.ID, &p.Name, &allowed, &cutoff, &p.UpgradeAllowed, &mustContain, &mustNot, &preferred, &fallback, &p.MaxSizeGB); err != nil {
		return Profile{}, err
	}
	p.Allowed = decodeTiers(allowed)
	p.Cutoff = Tier(cutoff)
	p.MustContain = decodeLines(mustContain)
	p.MustNotContain = decodeLines(mustNot)
	p.Preferred = decodePreferred(preferred)
	p.Fallback = cleanFallback(p.ID, decodeFallback(fallback))
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
	p.ID = 0
	p, err := Validate(p)
	if err != nil {
		return Profile{}, err
	}
	if p.Fallback, err = r.existingFallback(p.Fallback); err != nil {
		return Profile{}, err
	}
	res, err := r.db.Exec(
		`INSERT INTO quality_profiles (name, allowed_tiers, cutoff, upgrade_allowed, must_contain, must_not_contain, preferred, fallback, max_size_gb) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, encodeTiers(p.Allowed), string(p.Cutoff), p.UpgradeAllowed,
		encodeLines(p.MustContain), encodeLines(p.MustNotContain), encodePreferred(p.Preferred), encodeFallback(p.Fallback), p.MaxSizeGB,
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
	if p.Fallback, err = r.existingFallback(p.Fallback); err != nil {
		return Profile{}, err
	}
	res, err := r.db.Exec(
		`UPDATE quality_profiles SET name = ?, allowed_tiers = ?, cutoff = ?, upgrade_allowed = ?, must_contain = ?, must_not_contain = ?, preferred = ?, fallback = ?, max_size_gb = ? WHERE id = ?`,
		p.Name, encodeTiers(p.Allowed), string(p.Cutoff), p.UpgradeAllowed,
		encodeLines(p.MustContain), encodeLines(p.MustNotContain), encodePreferred(p.Preferred), encodeFallback(p.Fallback), p.MaxSizeGB, p.ID,
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
		return fmt.Errorf("%w: this is the default profile. Make another profile the default first", ErrInUse)
	}
	usage, err := r.Usage()
	if err != nil {
		return err
	}
	if n := usage[id]; n > 0 {
		return fmt.Errorf("%w: %d movie(s) or show(s) use it. Move them to another profile first", ErrInUse, n)
	}
	res, err := r.db.Exec(`DELETE FROM quality_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete quality profile %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return r.dropFallback(id)
}

// dropFallback removes a deleted profile from every other profile's
// fallback list.
func (r *Repo) dropFallback(id int64) error {
	list, err := r.List()
	if err != nil {
		return err
	}
	for _, p := range list {
		kept := []int64{}
		for _, f := range p.Fallback {
			if f != id {
				kept = append(kept, f)
			}
		}
		if len(kept) == len(p.Fallback) {
			continue
		}
		if _, err := r.db.Exec(`UPDATE quality_profiles SET fallback = ? WHERE id = ?`, encodeFallback(kept), p.ID); err != nil {
			return fmt.Errorf("remove profile %d from the fallback of %q: %w", id, p.Name, err)
		}
	}
	return nil
}

// PresetsVersion is the revision of the built-in preset set. The caller stores
// the version it last seeded (a settings key) and passes it back to
// SeedPresets, so each revision is applied exactly once:
//
//	1: "Up to 1080p", "Ultra-HD (up to 2160p)", "Any"
//	2: "Any", "720p", "1080p", "4K & over" (1080p and 4K with a fallback)
//	3: the same four, each resolution preset strict to its own resolution
//	4: adds "Cinema recordings" (CAM/TeleSync only)
const PresetsVersion = 4

// SeedPresets brings the built-in presets up to PresetsVersion and returns
// all profiles. seeded is the version applied last time (0 if never).
//
//   - Up to date: nothing changes.
//   - Empty table (first start): the current presets are created.
//   - Older install: each old built-in preset that is still exactly as it was
//     shipped is redefined in place as its new equivalent ("Up to 1080p"
//     becomes "1080p", "Ultra-HD (up to 2160p)" becomes "4K & over", "Any"
//     keeps its name with the new definition; revision 2's "1080p" and
//     "4K & over" lose their lower-resolution fallback). Keeping the row keeps its id,
//     so items assigned to it, and the default profile setting, follow it.
//     A preset the user edited is left alone. Then every current preset
//     newer than the seeded version that is still missing by name is created
//     (a preset the install already had is not recreated).
//
// Presets are ordinary rows afterwards: users can edit or delete them like any
// other profile, and a deleted preset is not brought back on a later start.
func (r *Repo) SeedPresets(seeded int) ([]Profile, error) {
	existing, err := r.List()
	if err != nil {
		return nil, err
	}
	if seeded >= PresetsVersion {
		return existing, nil
	}

	presets := Presets()
	byName := map[string]Profile{}
	for _, p := range existing {
		byName[p.Name] = p
	}
	for _, lp := range legacyPresets() {
		cur, ok := byName[lp.Old.Name]
		if !ok || !sameDefinition(cur, lp.Old) {
			continue // gone, or customised by the user: theirs now
		}
		next := presets[lp.NewTo]
		if other, taken := byName[next.Name]; taken && other.ID != cur.ID {
			continue // the user already has a profile with the new name
		}
		next.ID = cur.ID
		// Presets now start with upgrades off, but an install that already has
		// this preset keeps the choice stored with it.
		next.UpgradeAllowed = cur.UpgradeAllowed
		updated, err := r.Update(next)
		if err != nil {
			return nil, fmt.Errorf("upgrade preset %q: %w", lp.Old.Name, err)
		}
		delete(byName, cur.Name)
		byName[updated.Name] = updated
	}
	for _, k := range PresetKeys() {
		p := presets[k]
		if _, ok := byName[p.Name]; ok || presetSince(k) <= seeded {
			continue
		}
		created, err := r.Create(p)
		if err != nil {
			return nil, fmt.Errorf("seed preset %s: %w", k, err)
		}
		byName[created.Name] = created
	}
	return r.List()
}

// PresetName returns the display name of the preset a key stands for: a
// current key ("1080p") or one from the old single global setting
// ("any-1080p", "ultra-hd"), which map to their current equivalent.
func PresetName(key string) (string, bool) {
	if p, ok := Presets()[key]; ok {
		return p.Name, true
	}
	for _, lp := range legacyPresets() {
		if lp.Key == key {
			return Presets()[lp.NewTo].Name, true
		}
	}
	return "", false
}
