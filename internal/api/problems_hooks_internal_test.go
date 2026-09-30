package api

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/monitor"
	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/store"
)

func hookLog(t *testing.T) *problems.Log {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	l := problems.Open(db)
	problems.SetDefault(l)
	t.Cleanup(func() {
		l.Flush()
		problems.SetDefault(nil)
		db.Close()
	})
	return l
}

func hookRows(t *testing.T, l *problems.Log) []problems.Entry {
	t.Helper()
	l.Flush()
	rows, _, err := l.List(problems.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestAFailedDownloadIsLoggedWithTheReasonAndTheTitle(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"broken archive", errors.New("couldn't unpack the download: bad header"), problems.CodeUnpackFailed},
		{"repair", errors.New("repairing the download failed: par2 repair x: exit status 2"), problems.CodePar2Failed},
		{"full disk", errors.New("couldn't unpack the download: not enough free disk space to unpack the archive"), problems.CodeDiskFull},
		{"no permission", fmt.Errorf("couldn't move the file into your library: open /movies/x: permission denied"), problems.CodeFolderPermission},
		{"login refused", errors.New("the download failed: news.example.com: The provider refused this username and password."), problems.CodeUsenetAuthRefused},
		{"anything else", errors.New("the download failed: something odd"), problems.CodeDownloadFailed},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := hookLog(t)
			noteDownloadFailure(int64(40+i), "The Matrix", "/title/603", fmt.Errorf("%w password=hunter2", tt.err))
			rows := hookRows(t, l)
			if len(rows) != 1 {
				t.Fatalf("want one row, got %+v", rows)
			}
			r := rows[0]
			if r.Code != tt.want || r.Level != problems.LevelError || r.Title != "The Matrix" || r.Link != "/title/603" || r.DownloadID != int64(40+i) {
				t.Errorf("row = %+v", r)
			}
			if strings.Contains(r.Detail, "hunter2") {
				t.Errorf("a password was stored: %q", r.Detail)
			}
		})
	}
}

func TestTheSameDownloadFailingTwiceIsOneRowButTwoDownloadsAreTwo(t *testing.T) {
	l := hookLog(t)
	noteDownloadFailure(1, "A", "/title/1", errors.New("couldn't unpack the download: x"))
	noteDownloadFailure(1, "A", "/title/1", errors.New("couldn't unpack the download: x"))
	noteDownloadFailure(2, "B", "/title/2", errors.New("couldn't unpack the download: x"))
	rows := hookRows(t, l)
	if len(rows) != 2 {
		t.Fatalf("got %+v", rows)
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Title] = r.Count
	}
	if counts["A"] != 2 || counts["B"] != 1 {
		t.Errorf("counts = %v", counts)
	}
}

func TestMonitorFailuresAreLoggedQuietly(t *testing.T) {
	l := hookLog(t)
	var heard []problems.Entry
	l.OnNew = func(e problems.Entry) { heard = append(heard, e) }

	noteMonitorProblem(monitor.Event{Failing: false, Kind: monitor.KindUsenet, Name: "Eweka"})
	noteMonitorProblem(monitor.Event{Failing: true, Kind: monitor.KindUsenet, Name: "Eweka", Error: "dial tcp: lookup news.eweka.nl: no such host"})
	noteMonitorProblem(monitor.Event{Failing: true, Kind: monitor.KindIndexer, Name: "NZBgeek", Error: "HTTP 429 apikey=***"})
	rows := hookRows(t, l)
	if len(rows) != 2 {
		t.Fatalf("a recovery is not a problem: %+v", rows)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Subject] = r.Code
	}
	if got["Eweka"] != problems.CodeUsenetUnreachable || got["NZBgeek"] != problems.CodeIndexerRateLimited {
		t.Errorf("codes = %v", got)
	}
	if len(heard) != 0 {
		t.Errorf("the monitor sends its own notification, so these must stay quiet: %+v", heard)
	}
}

func TestNotificationFailuresAndServiceLimitsAreLogged(t *testing.T) {
	l := hookLog(t)
	noteNotifyFailure(errors.New("notification endpoint returned status 500"))
	noteServiceLimit(serviceTMDB)
	noteServiceLimit(serviceOpenSubtitles)
	noteServiceLimit(serviceTrakt) // no help entry for it: left out
	rows := hookRows(t, l)
	codes := map[string]bool{}
	for _, r := range rows {
		codes[r.Code] = true
	}
	if len(rows) != 3 || !codes[problems.CodeNotificationFailed] || !codes[problems.CodeTMDBRateLimited] || !codes[problems.CodeSubtitlesRateLimited] {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestUpdateAndRestartTroubleIsLogged(t *testing.T) {
	l := hookLog(t)
	noteUpdateCheckFailed(errors.New(`Get "https://api.github.com/x": dial tcp: no such host`))
	noteUpdateInstallFailed("1.4.0", "The download did not match its checksum.", errors.New("sha256 differs"))
	noteRestartedItself("the database answered: false", time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC))
	rows := hookRows(t, l)
	got := map[string]problems.Entry{}
	for _, r := range rows {
		got[r.Code] = r
	}
	if len(rows) != 3 || got[problems.CodeUpdateCheckFailed].Level != problems.LevelWarning ||
		got[problems.CodeUpdateInstallFailed].Level != problems.LevelError || got[problems.CodeUpdateInstallFailed].Subject != "1.4.0" ||
		got[problems.CodeAppRestartedItself].Detail != "the database answered: false" {
		t.Fatalf("rows = %+v", rows)
	}
}
