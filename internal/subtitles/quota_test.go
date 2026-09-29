package subtitles_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/store"
	"github.com/ryanborg/mediarium/internal/subtitles"
)

func TestComputeQuota(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	future := now.Add(5 * time.Hour)

	tests := []struct {
		name       string
		hasAccount bool
		report     *subtitles.QuotaReport
		downloads  []time.Time
		want       subtitles.QuotaState
		wantReset  *time.Time
	}{
		{
			name: "no account, nothing used",
			want: subtitles.QuotaState{Limit: 5, Used: 0, Remaining: 5, Source: "estimated"},
		},
		{
			name: "account raises the estimate", hasAccount: true, downloads: []time.Time{ago(time.Hour), ago(2 * time.Hour)},
			want:      subtitles.QuotaState{Limit: 20, Used: 2, Remaining: 18, Source: "estimated"},
			wantReset: ptr(ago(2 * time.Hour).Add(24 * time.Hour)),
		},
		{
			name: "estimate used up", downloads: []time.Time{ago(time.Hour), ago(2 * time.Hour), ago(3 * time.Hour), ago(4 * time.Hour), ago(5 * time.Hour), ago(6 * time.Hour)},
			want:      subtitles.QuotaState{Limit: 5, Used: 6, Remaining: 0, Source: "estimated", Exceeded: true},
			wantReset: ptr(ago(6 * time.Hour).Add(24 * time.Hour)),
		},
		{
			name:   "report with used count gives the real limit",
			report: &subtitles.QuotaReport{Remaining: 97, Requests: 3, HasRequests: true, ResetAt: future, ObservedAt: ago(time.Minute)},
			want:   subtitles.QuotaState{Limit: 100, Used: 3, Remaining: 97, Source: "reported"}, wantReset: &future,
		},
		{
			name:      "downloads after the report are subtracted",
			report:    &subtitles.QuotaReport{Remaining: 4, Requests: 1, HasRequests: true, ResetAt: future, ObservedAt: ago(time.Hour)},
			downloads: []time.Time{ago(time.Hour), ago(30 * time.Minute), ago(10 * time.Minute)},
			want:      subtitles.QuotaState{Limit: 5, Used: 3, Remaining: 2, Source: "reported"}, wantReset: &future,
		},
		{
			name:      "the download that produced the report is not counted twice",
			report:    &subtitles.QuotaReport{Remaining: 4, Requests: 1, HasRequests: true, ResetAt: future, ObservedAt: ago(time.Hour)},
			downloads: []time.Time{ago(time.Hour)},
			want:      subtitles.QuotaState{Limit: 5, Used: 1, Remaining: 4, Source: "reported"}, wantReset: &future,
		},
		{
			name:   "a refused download means nothing left until the reset",
			report: &subtitles.QuotaReport{Remaining: 0, ResetAt: future, ObservedAt: ago(time.Minute)},
			want:   subtitles.QuotaState{Limit: 5, Used: 5, Remaining: 0, Source: "reported", Exceeded: true}, wantReset: &future,
		},
		{
			name:      "no reset given: the report holds for 24 hours",
			report:    &subtitles.QuotaReport{Remaining: 0, ObservedAt: ago(2 * time.Hour)},
			want:      subtitles.QuotaState{Limit: 5, Used: 5, Remaining: 0, Source: "reported", Exceeded: true},
			wantReset: ptr(ago(2 * time.Hour).Add(24 * time.Hour)),
		},
		{
			name:      "an expired report is ignored",
			report:    &subtitles.QuotaReport{Remaining: 0, Requests: 5, HasRequests: true, ResetAt: ago(time.Minute), ObservedAt: ago(20 * time.Hour)},
			downloads: []time.Time{ago(time.Hour)},
			want:      subtitles.QuotaState{Limit: 5, Used: 1, Remaining: 4, Source: "estimated"},
			wantReset: ptr(ago(time.Hour).Add(24 * time.Hour)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := subtitles.ComputeQuota(now, tc.hasAccount, tc.report, tc.downloads)
			gotReset := got.ResetsAt
			got.ResetsAt = nil
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			switch {
			case (gotReset == nil) != (tc.wantReset == nil):
				t.Fatalf("resetsAt = %v, want %v", gotReset, tc.wantReset)
			case gotReset != nil && !gotReset.Equal(*tc.wantReset):
				t.Fatalf("resetsAt = %v, want %v", gotReset, tc.wantReset)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestQuotaAndDismissRepos(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	q := subtitles.NewQuotaRepo(db)
	if rep, err := q.LoadReport(); err != nil || rep != nil {
		t.Fatalf("expected no report yet, got %+v, %v", rep, err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := q.RecordDownload(now.Add(-3 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := q.RecordDownload(now); err != nil {
		t.Fatal(err)
	}
	// Old downloads fall out of the window (and get pruned).
	if err := q.RecordDownload(now.Add(-30 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := q.DownloadsSince(now.Add(-24 * time.Hour))
	if err != nil || len(got) != 2 || !got[1].Equal(now) {
		t.Fatalf("downloads = %v, %v", got, err)
	}

	want := subtitles.QuotaReport{Remaining: 17, Requests: 3, HasRequests: true, ResetAt: now.Add(5 * time.Hour), ObservedAt: now}
	if err := q.SaveReport(want); err != nil {
		t.Fatal(err)
	}
	rep, err := q.LoadReport()
	if err != nil || rep == nil || rep.Remaining != 17 || !rep.HasRequests || rep.Requests != 3 || !rep.ResetAt.Equal(want.ResetAt) || !rep.ObservedAt.Equal(now) {
		t.Fatalf("report = %+v, %v", rep, err)
	}
	// A later report without the optional figures replaces the earlier one.
	if err := q.SaveReport(subtitles.QuotaReport{Remaining: 0, ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	rep, _ = q.LoadReport()
	if rep.Remaining != 0 || rep.HasRequests || !rep.ResetAt.IsZero() {
		t.Fatalf("report after update = %+v", rep)
	}

	d := subtitles.NewDismissRepo(db)
	items := []subtitles.Item{{Kind: "movie", ID: 1}, {Kind: "episode", ID: 1}}
	if err := d.Dismiss(items); err != nil {
		t.Fatal(err)
	}
	if err := d.Dismiss(items[:1]); err != nil { // dismissing twice is fine
		t.Fatal(err)
	}
	all, err := d.All()
	if err != nil || len(all) != 2 || !all[items[0]] || !all[items[1]] {
		t.Fatalf("dismissed = %v, %v", all, err)
	}
	if err := d.Restore(items[:1]); err != nil {
		t.Fatal(err)
	}
	if all, _ = d.All(); len(all) != 1 || !all[items[1]] {
		t.Fatalf("after restore = %v", all)
	}
}

func TestRequestDownloadInfoReportsQuota(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     map[string]any
		wantErr  bool
		want     subtitles.DownloadInfo // compared field by field except ResetAt
		resetIn  time.Duration          // expected reset relative to the request, when ResetAt is derived
		resetUTC string                 // expected absolute reset
	}{
		{
			name: "full report with an absolute reset",
			body: map[string]any{"link": "L", "requests": 3, "remaining": 17, "message": "ok", "reset_time": "23 hours and 59 minutes", "reset_time_utc": "2030-01-02T03:04:05.000Z"},
			want: subtitles.DownloadInfo{Link: "L", Requests: 3, HasRequests: true, Remaining: 17, HasRemaining: true, Message: "ok"}, resetUTC: "2030-01-02T03:04:05Z",
		},
		{
			name: "only a human reset time",
			body: map[string]any{"link": "L", "requests": 1, "remaining": 4, "reset_time": "23 hours and 59 minutes"},
			want: subtitles.DownloadInfo{Link: "L", Requests: 1, HasRequests: true, Remaining: 4, HasRemaining: true}, resetIn: 23*time.Hour + 59*time.Minute,
		},
		{
			name: "minutes only",
			body: map[string]any{"link": "L", "remaining": 4, "reset_time": "12 minutes"},
			want: subtitles.DownloadInfo{Link: "L", Remaining: 4, HasRemaining: true}, resetIn: 12 * time.Minute,
		},
		{
			name: "nothing reported",
			body: map[string]any{"link": "L"},
			want: subtitles.DownloadInfo{Link: "L"},
		},
		{
			name: "quota reached", status: http.StatusNotAcceptable, wantErr: true,
			body: map[string]any{"requests": 5, "remaining": 0, "message": "limit", "reset_time_utc": "2030-01-02T03:04:05.000Z"},
			want: subtitles.DownloadInfo{Requests: 5, HasRequests: true, Remaining: 0, HasRemaining: true, Message: "limit"}, resetUTC: "2030-01-02T03:04:05Z",
		},
		{
			name: "406 without a body still means none left", status: http.StatusNotAcceptable, wantErr: true,
			body: nil,
			want: subtitles.DownloadInfo{Remaining: 0, HasRemaining: true},
		},
		{
			name: "429 without figures reports none", status: http.StatusTooManyRequests, wantErr: true,
			body: nil,
			want: subtitles.DownloadInfo{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				if tc.body != nil {
					json.NewEncoder(w).Encode(tc.body)
				}
			}))
			defer srv.Close()

			before := time.Now()
			info, err := subtitles.NewWithBaseURL("k", srv.URL).RequestDownloadInfo(context.Background(), 1)
			after := time.Now()
			if tc.wantErr {
				var qe *subtitles.QuotaError
				if !errors.As(err, &qe) || !errors.Is(err, subtitles.ErrQuota) {
					t.Fatalf("expected a quota error, got %v", err)
				}
				info = qe.Info
			} else if err != nil {
				t.Fatal(err)
			}

			reset := info.ResetAt
			info.ResetAt = time.Time{}
			if info != tc.want {
				t.Fatalf("info = %+v, want %+v", info, tc.want)
			}
			switch {
			case tc.resetUTC != "":
				if reset.UTC().Format(time.RFC3339) != tc.resetUTC {
					t.Fatalf("resetAt = %v, want %s", reset, tc.resetUTC)
				}
			case tc.resetIn > 0:
				if reset.Before(before.Add(tc.resetIn)) || reset.After(after.Add(tc.resetIn)) {
					t.Fatalf("resetAt = %v, want about %v after the request", reset, tc.resetIn)
				}
			default:
				if !reset.IsZero() {
					t.Fatalf("resetAt = %v, want none", reset)
				}
			}
		})
	}
}
