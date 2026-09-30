package subtitles

import (
	"database/sql"
	"fmt"
	"time"
)

// QuotaWindow is how long OpenSubtitles counts downloads for: the limit is per
// rolling 24 hours.
const QuotaWindow = 24 * time.Hour

// Download limits OpenSubtitles documents when it has not told us the real
// figure: about 5 per day per IP address without a personal account, about 20
// with a free one (VIP accounts get more, which the download response reports).
const (
	AnonymousDailyLimit = 5
	AccountDailyLimit   = 20
)

// QuotaReport is the quota OpenSubtitles last reported in a download response.
type QuotaReport struct {
	Remaining   int
	Requests    int // downloads used in the window; meaningful when HasRequests
	HasRequests bool
	ResetAt     time.Time // zero when OpenSubtitles did not say
	ObservedAt  time.Time
}

// QuotaState is Mediarium's best knowledge of today's download allowance.
type QuotaState struct {
	Limit     int
	Used      int
	Remaining int
	ResetsAt  *time.Time
	Source    string // "reported" (OpenSubtitles said so) or "estimated"
	Exceeded  bool
}

// Quota sources.
const (
	QuotaReported  = "reported"
	QuotaEstimated = "estimated"
)

// ComputeQuota works out the allowance now. downloads are the times of the
// downloads Mediarium made in the last QuotaWindow; report is the last figure
// OpenSubtitles gave (nil when never). A report still describes the current
// window while its reset time (or, when none was given, 24 hours after it was
// observed) lies ahead; downloads made after it are subtracted. Otherwise the
// allowance is estimated from the documented per-day limits.
func ComputeQuota(now time.Time, hasAccount bool, report *QuotaReport, downloads []time.Time) QuotaState {
	estimated := AnonymousDailyLimit
	if hasAccount {
		estimated = AccountDailyLimit
	}

	if report != nil {
		resetAt := report.ResetAt
		if resetAt.IsZero() {
			resetAt = report.ObservedAt.Add(QuotaWindow)
		}
		if now.Before(resetAt) {
			after := 0
			for _, d := range downloads {
				if d.After(report.ObservedAt) {
					after++
				}
			}
			remaining := max(0, report.Remaining-after)
			st := QuotaState{Remaining: remaining, ResetsAt: &resetAt, Source: QuotaReported, Exceeded: remaining == 0}
			if report.HasRequests {
				st.Used = report.Requests + after
				st.Limit = report.Requests + report.Remaining
			} else {
				st.Limit = max(estimated, remaining)
				st.Used = st.Limit - remaining
			}
			return st
		}
	}

	st := QuotaState{Limit: estimated, Used: len(downloads), Source: QuotaEstimated}
	st.Remaining = max(0, st.Limit-st.Used)
	st.Exceeded = st.Remaining == 0
	if len(downloads) > 0 {
		earliest := downloads[0]
		for _, d := range downloads {
			if d.Before(earliest) {
				earliest = d
			}
		}
		reset := earliest.Add(QuotaWindow)
		st.ResetsAt = &reset
	}
	return st
}

// QuotaRepo remembers the downloads Mediarium made and the last quota report.
type QuotaRepo struct {
	db *sql.DB
}

func NewQuotaRepo(db *sql.DB) *QuotaRepo { return &QuotaRepo{db: db} }

func formatQuotaTime(t time.Time) string { return t.UTC().Format(attemptTimeLayout) }

func parseQuotaTime(s string) (time.Time, error) {
	t, err := time.Parse(attemptTimeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", s, err)
	}
	return t, nil
}

// RecordDownload notes one download at the given time and forgets downloads
// too old to matter.
func (r *QuotaRepo) RecordDownload(at time.Time) error {
	if _, err := r.db.Exec(`INSERT INTO subtitle_downloads (downloaded_at) VALUES (?)`, formatQuotaTime(at)); err != nil {
		return fmt.Errorf("record subtitle download: %w", err)
	}
	if _, err := r.db.Exec(`DELETE FROM subtitle_downloads WHERE downloaded_at < ?`, formatQuotaTime(at.Add(-2*QuotaWindow))); err != nil {
		return fmt.Errorf("prune subtitle downloads: %w", err)
	}
	return nil
}

// DownloadsSince lists the downloads made at or after since.
func (r *QuotaRepo) DownloadsSince(since time.Time) ([]time.Time, error) {
	rows, err := r.db.Query(`SELECT downloaded_at FROM subtitle_downloads WHERE downloaded_at >= ? ORDER BY downloaded_at`, formatQuotaTime(since))
	if err != nil {
		return nil, fmt.Errorf("list subtitle downloads: %w", err)
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan subtitle download: %w", err)
		}
		t, err := parseQuotaTime(s)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveReport stores the quota OpenSubtitles just reported (replacing the last).
func (r *QuotaRepo) SaveReport(rep QuotaReport) error {
	var requests, resetAt any
	if rep.HasRequests {
		requests = rep.Requests
	}
	if !rep.ResetAt.IsZero() {
		resetAt = formatQuotaTime(rep.ResetAt)
	}
	_, err := r.db.Exec(
		`INSERT INTO subtitle_quota (id, remaining, requests, reset_at, observed_at) VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET remaining = excluded.remaining, requests = excluded.requests,
		   reset_at = excluded.reset_at, observed_at = excluded.observed_at`,
		rep.Remaining, requests, resetAt, formatQuotaTime(rep.ObservedAt),
	)
	if err != nil {
		return fmt.Errorf("save subtitle quota: %w", err)
	}
	return nil
}

// LoadReport returns the last stored report, or nil when there is none.
func (r *QuotaRepo) LoadReport() (*QuotaReport, error) {
	var (
		rep      QuotaReport
		requests sql.NullInt64
		resetAt  sql.NullString
		observed string
	)
	err := r.db.QueryRow(`SELECT remaining, requests, reset_at, observed_at FROM subtitle_quota WHERE id = 1`).
		Scan(&rep.Remaining, &requests, &resetAt, &observed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load subtitle quota: %w", err)
	}
	if requests.Valid {
		rep.Requests, rep.HasRequests = int(requests.Int64), true
	}
	if resetAt.Valid {
		if rep.ResetAt, err = parseQuotaTime(resetAt.String); err != nil {
			return nil, err
		}
	}
	if rep.ObservedAt, err = parseQuotaTime(observed); err != nil {
		return nil, err
	}
	return &rep, nil
}

// DismissRepo remembers titles the person does not want subtitles for.
type DismissRepo struct {
	db *sql.DB
}

func NewDismissRepo(db *sql.DB) *DismissRepo { return &DismissRepo{db: db} }

// Item names a movie or an episode.
type Item struct {
	Kind string // "movie" or "episode"
	ID   int64
}

// Dismiss marks items as "no subtitles wanted".
func (r *DismissRepo) Dismiss(items []Item) error {
	for _, it := range items {
		if _, err := r.db.Exec(
			`INSERT INTO subtitle_dismissed (kind, media_id, dismissed_at) VALUES (?, ?, ?)
			 ON CONFLICT (kind, media_id) DO NOTHING`,
			it.Kind, it.ID, formatQuotaTime(time.Now()),
		); err != nil {
			return fmt.Errorf("dismiss subtitles: %w", err)
		}
	}
	return nil
}

// Restore undoes Dismiss.
func (r *DismissRepo) Restore(items []Item) error {
	for _, it := range items {
		if _, err := r.db.Exec(`DELETE FROM subtitle_dismissed WHERE kind = ? AND media_id = ?`, it.Kind, it.ID); err != nil {
			return fmt.Errorf("restore subtitles: %w", err)
		}
	}
	return nil
}

// All returns every dismissed item.
func (r *DismissRepo) All() (map[Item]bool, error) {
	rows, err := r.db.Query(`SELECT kind, media_id FROM subtitle_dismissed`)
	if err != nil {
		return nil, fmt.Errorf("list dismissed subtitles: %w", err)
	}
	defer rows.Close()
	out := map[Item]bool{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.Kind, &it.ID); err != nil {
			return nil, fmt.Errorf("scan dismissed subtitle: %w", err)
		}
		out[it] = true
	}
	return out, rows.Err()
}
