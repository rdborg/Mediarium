package api

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/monitor"
	"github.com/rdborg/mediarium/internal/problems"
)

// The places in the API that put something in the problem log. The rest of the
// app calls problems.Record where the problem happens; these are the ones that
// need the API's own types.

// noteDownloadFailure records a download that failed. The code says why (a full
// disk, a broken archive, a login the provider refused), judged from the error.
func noteDownloadFailure(queueID int64, title, link string, err error) {
	problems.Record(problems.Problem{
		Code:       problems.CodeFor(err, problems.CodeDownloadFailed),
		Level:      problems.LevelError,
		Subject:    "download " + strconv.FormatInt(queueID, 10),
		Err:        err,
		Title:      title,
		Link:       link,
		DownloadID: queueID,
	})
}

// noteMonitorProblem records a Usenet server or indexer the connection
// check found not working. The health notification for it goes out already, so
// this row does not send a second one.
func noteMonitorProblem(ev monitor.Event) {
	if !ev.Failing {
		return
	}
	cause := errors.New(ev.Error)
	code := problems.UsenetCode(cause)
	message := fmt.Sprintf("The check of %s failed.", ev.Name)
	if ev.Kind == monitor.KindIndexer {
		code = problems.IndexerCode(cause)
	}
	problems.Record(problems.Problem{Code: code, Subject: ev.Name, Message: message, Err: cause, Quiet: true})
}

// noteNotifyFailure records a notification that could not be delivered.
func noteNotifyFailure(err error) {
	problems.Record(problems.Problem{
		Code: problems.CodeNotificationFailed, Message: "A notification could not be delivered.", Err: err,
	})
}

// noteServiceLimit records that an outside service (TMDB, OpenSubtitles) refused
// requests because of its limit. Trakt has no help entry of its own, so it is
// left out.
func noteServiceLimit(service string) {
	switch service {
	case serviceTMDB:
		problems.Record(problems.Problem{Code: problems.CodeTMDBRateLimited, Message: "TMDB says Mediarium has made too many requests for now."})
	case serviceOpenSubtitles:
		problems.Record(problems.Problem{Code: problems.CodeSubtitlesRateLimited, Message: "OpenSubtitles says a limit is reached."})
	}
}

// diagnosticsProblems is the latest 30 rows of the problem log, for the support
// report. A database that does not answer leaves the section empty.
func (s *Server) diagnosticsProblems() []diagnosticsProblem {
	out := []diagnosticsProblem{}
	var entries []problems.Entry
	var err error
	if !within(2*time.Second, func() { entries, _, err = s.Problems.List(problems.Filter{Limit: 30}) }) || err != nil {
		return out
	}
	for _, e := range entries {
		out = append(out, diagnosticsProblem{
			At: e.LastAt.UTC().Format(time.RFC3339), Level: string(e.Level), Area: problems.AreaLabel(e.Area), Code: e.Code,
			Message: e.Message, Count: e.Count, Detail: problems.Clip(e.Detail, 400),
		})
	}
	return out
}

// noteUpdateCheckFailed records a daily check for a new version that could not
// be finished.
func noteBackupFailed(err error) {
	problems.Record(problems.Problem{Code: problems.CodeBackupFailed, Err: err})
}

func noteUpdateCheckFailed(err error) {
	problems.Record(problems.Problem{Code: problems.CodeUpdateCheckFailed, Message: "Mediarium could not check for a new version.", Err: err})
}

// noteUpdateInstallFailed records an update that could not be put in place.
func noteUpdateInstallFailed(version, message string, err error) {
	problems.Record(problems.Problem{
		Code: problems.CodeUpdateInstallFailed, Level: problems.LevelError, Subject: version,
		Message: message, Detail: "version " + version, Err: err,
	})
}

// noteRestartedItself records that this run of Mediarium began after it
// restarted itself because it had stopped answering.
func noteRestartedItself(reason string, at time.Time) {
	problems.Record(problems.Problem{
		Code: problems.CodeAppRestartedItself, Subject: at.UTC().Format(time.RFC3339),
		Message: "Mediarium restarted itself because it stopped answering.", Detail: reason,
	})
}
