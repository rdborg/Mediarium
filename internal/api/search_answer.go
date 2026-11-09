package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/netproxy"
	"github.com/rdborg/mediarium/internal/problems"
)

// The interactive release lists (movie, season or episode, album) answer with
// a plain list of releases. With ?detail=1 they answer with the list and also
// say which indexers were asked and which did not answer, so the page
// can tell "nothing was found" from "an indexer is down".

type unansweredSource struct {
	Name    string `json:"name"`
	Message string `json:"message"` // plain words, for example "Could not reach Example."
}

type searchAnswer[T any] struct {
	Results    []T                `json:"results"`
	Sources    int                `json:"sources"` // indexers that were asked
	Unanswered []unansweredSource `json:"unanswered"`
	Warnings   []string           `json:"warnings"` // for example an indexer asked past its daily limit
	// Skipped names the indexers that were not asked because they don't list
	// this kind of release (a movie site, for a book).
	Skipped []string `json:"skipped,omitempty"`
	// Searched lists every indexer the search had to do with, one line each:
	// what it found, or why it found nothing.
	Searched []searchedIndexer `json:"searched"`
	// Ignored is how many releases the indexers sent back that were left out of
	// the list because their title or year is not this title's, and
	// IgnoredSamples a few of their names. They let the page tell "nothing was
	// found" from "releases came back, but none of them is this film".
	Ignored        int      `json:"ignored,omitempty"`
	IgnoredSamples []string `json:"ignoredSamples,omitempty"`
}

// ignoredReleases counts the releases a search left out because they are not the
// title asked for, and keeps the names of the first few.
type ignoredReleases struct {
	count   int
	samples []string
}

const maxIgnoredSamples = 3

func (g *ignoredReleases) add(title string) {
	g.count++
	if len(g.samples) < maxIgnoredSamples {
		g.samples = append(g.samples, title)
	}
}

// What a searched indexer did.
const (
	searchFound   = "found"   // it had releases for this title
	searchEmpty   = "empty"   // it answered, with nothing for this title
	searchFailed  = "failed"  // it did not answer
	searchSkipped = "skipped" // it was not asked
)

// searchedIndexer is one line of "who was asked, and what came of it".
type searchedIndexer struct {
	IndexerID int64  `json:"indexerId"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`   // "torrent" or "usenet"
	Status    string `json:"status"` // searchFound, searchEmpty, searchFailed or searchSkipped
	Count     int    `json:"count"`  // releases of this indexer in the list
	Ms        int64  `json:"ms"`     // how long its search took
	// Code is the short why behind a failed or skipped line: "timeout",
	// "daily_limit", "rate_limited", "auth", "unreachable", "flaresolverr",
	// "gone", "error", "vpn", "paused", "disabled" or "no_category". Reason is
	// the same in a sentence.
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Detail is the technical text behind Reason, when Reason is an
	// explanation written for the person ("flaresolverr", "gone").
	Detail string `json:"detail,omitempty"`
}

// namedIndexer is a result that knows which indexer it came from.
type namedIndexer interface{ indexerName() string }

func (p searchResultPayload) indexerName() string { return p.IndexerName }
func (p musicReleasePayload) indexerName() string { return p.IndexerName }

// indexerCounts counts the releases of each indexer in a list.
func indexerCounts[T any](results []T) map[string]int {
	out := map[string]int{}
	for _, r := range results {
		if n, ok := any(r).(namedIndexer); ok {
			out[n.indexerName()]++
		}
	}
	return out
}

// whyFailed gives the short code for an indexer that did not answer.
func whyFailed(err error) string {
	switch {
	case netproxy.WaitMessage(err) != "":
		return "vpn"
	case indexers.IsDailyLimit(err):
		return "daily_limit"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	if _, ok := indexers.FlareSolverrDown(err); ok {
		return "flaresolverr"
	}
	if _, ok := indexers.GoneStatus(err); ok {
		return "gone"
	}
	switch problems.IndexerCode(err) {
	case problems.CodeIndexerRateLimited:
		return "rate_limited"
	case problems.CodeIndexerAuthRefused:
		return "auth"
	case problems.CodeIndexerUnreachable:
		return "unreachable"
	}
	return "error"
}

// searchedLine says what became of one indexer that was asked, from its
// outcomes (one per query it was given).
func searchedLine(l searchedIndexer, outs []indexers.Outcome) searchedIndexer {
	for _, o := range outs {
		if !o.Skipped && o.Err == nil {
			// it answered at least once
			l.Status = searchEmpty
			if l.Count > 0 {
				l.Status = searchFound
			}
			return l
		}
	}
	l.Status = searchEmpty
	if l.Count > 0 {
		l.Status = searchFound
		return l
	}
	first := outs[0]
	switch {
	case first.Skipped && first.LimitNote != "":
		l.Status, l.Code, l.Reason = searchSkipped, "daily_limit", first.IndexerName+": "+first.LimitNote
	case first.Skipped:
		l.Status, l.Code = searchSkipped, "paused"
		l.Reason = indexers.PausedMessage(l.Name, first.Health, nil)
	case netproxy.WaitMessage(first.Err) != "":
		l.Status, l.Code, l.Reason = searchSkipped, "vpn", indexers.ProblemMessage(first)
	default:
		l.Status, l.Code, l.Reason = searchFailed, whyFailed(first.Err), indexers.ProblemMessage(first)
		l.Detail = indexers.ProblemDetail(first)
	}
	return l
}

// searchedFrom lines up every indexer of a search: the ones in all (in that
// order; when all is empty, the ones named by the outcomes), with what each
// found. counts is how many releases of each are in the list shown, notAsked
// the names left out because they don't list this kind of release.
func searchedFrom(all []indexers.Instance, outcomes []indexers.Outcome, counts map[string]int, notAsked []string) []searchedIndexer {
	left := map[string]bool{}
	for _, n := range notAsked {
		left[n] = true
	}
	byName := map[string][]indexers.Outcome{}
	var order []string
	for _, o := range outcomes {
		if _, ok := byName[o.IndexerName]; !ok {
			order = append(order, o.IndexerName)
		}
		byName[o.IndexerName] = append(byName[o.IndexerName], o)
	}
	out := []searchedIndexer{}
	add := func(name string, id int64, kind string, enabled bool) {
		l := searchedIndexer{IndexerID: id, Name: name, Kind: kind, Count: counts[name]}
		outs := byName[name]
		for _, o := range outs {
			l.Ms += o.Elapsed.Milliseconds()
			if l.Kind == "" {
				l.Kind = string(o.Protocol)
			}
			if l.IndexerID == 0 {
				l.IndexerID = o.IndexerID
			}
		}
		switch {
		case left[name]:
			l.Status, l.Code = searchSkipped, "no_category"
			l.Reason = fmt.Sprintf("Left out because %s does not list this kind of release.", name)
		case !enabled:
			l.Status, l.Code = searchSkipped, "disabled"
			l.Reason = fmt.Sprintf("Left out because %s is turned off.", name)
		case len(outs) == 0:
			return
		default:
			l = searchedLine(l, outs)
		}
		out = append(out, l)
	}
	seen := map[string]bool{}
	for _, inst := range all {
		if seen[inst.Name] {
			continue
		}
		seen[inst.Name] = true
		add(inst.Name, inst.ID, string(inst.Protocol), inst.Enabled)
	}
	for _, n := range order {
		if !seen[n] {
			seen[n] = true
			add(n, 0, "", true)
		}
	}
	return out
}

// unansweredFrom lists the indexers that failed, in the order asked. An indexer
// asked for several titles of one movie or show is listed once, with the first
// thing it said.
func unansweredFrom(outcomes []indexers.Outcome) []unansweredSource {
	out := []unansweredSource{}
	listed := map[string]bool{}
	for _, o := range outcomes {
		if listed[o.IndexerName] {
			continue
		}
		if o.Err != nil {
			listed[o.IndexerName] = true
			out = append(out, unansweredSource{Name: o.IndexerName, Message: indexers.ProblemMessage(o)})
		}
		if o.Skipped {
			listed[o.IndexerName] = true
			msg := indexers.PausedMessage(o.IndexerName, o.Health, nil)
			if o.LimitNote != "" {
				msg = o.IndexerName + ": " + o.LimitNote
			}
			out = append(out, unansweredSource{Name: o.IndexerName, Message: msg})
		}
	}
	return out
}

// limitWarnings lists the indexers that were asked although their daily limit
// is used up (a search started by hand asks them anyway), once each.
func limitWarnings(outcomes []indexers.Outcome) []string {
	out := []string{}
	listed := map[string]bool{}
	for _, o := range outcomes {
		if o.LimitNote == "" || o.Skipped || listed[o.IndexerName] {
			continue
		}
		listed[o.IndexerName] = true
		out = append(out, fmt.Sprintf("Even though its daily limit is used up, %s was asked, so this search may be refused.", o.IndexerName))
	}
	return out
}

// writeSearch sends the release list: the bare list, or with ?detail=1 the
// list with what the indexers said. sources is how many were asked.
func writeSearch[T any](w http.ResponseWriter, r *http.Request, results []T, sources int, outcomes []indexers.Outcome) {
	writeSearchSkipping(w, r, results, sources, outcomes, nil)
}

// writeSearchSkipping is writeSearch that also names the indexers that were
// left out because they can't do this kind of search.
func writeSearchSkipping[T any](w http.ResponseWriter, r *http.Request, results []T, sources int, outcomes []indexers.Outcome, skipped []string) {
	writeSearchAll(w, r, results, sources, outcomes, skipped, nil)
}

// writeSearchAll is writeSearchSkipping that also lists, in "searched", every
// indexer in all with what it found.
func writeSearchAll[T any](w http.ResponseWriter, r *http.Request, results []T, sources int, outcomes []indexers.Outcome, skipped []string, all []indexers.Instance) {
	writeSearchIgnoring(w, r, results, sources, outcomes, skipped, all, ignoredReleases{})
}

// writeSearchWith is writeSearchAll that also adds lines to "searched" for
// sources that are not indexers (Soulseek).
func writeSearchWith[T any](w http.ResponseWriter, r *http.Request, results []T, sources int, outcomes []indexers.Outcome, skipped []string, all []indexers.Instance, extra []searchedIndexer) {
	if results == nil {
		results = []T{}
	}
	if r.URL.Query().Get("detail") != "1" {
		writeJSON(w, http.StatusOK, results)
		return
	}
	searched := append(searchedFrom(all, outcomes, indexerCounts(results), skipped), extra...)
	writeJSON(w, http.StatusOK, searchAnswer[T]{Results: results, Sources: sources, Unanswered: unansweredFrom(outcomes), Warnings: limitWarnings(outcomes), Skipped: skipped, Searched: searched})
}

// writeSearchIgnoring is writeSearchAll that also says how many releases were
// left out for not being the title asked for.
func writeSearchIgnoring[T any](w http.ResponseWriter, r *http.Request, results []T, sources int, outcomes []indexers.Outcome, skipped []string, all []indexers.Instance, ignored ignoredReleases) {
	if results == nil {
		results = []T{}
	}
	if r.URL.Query().Get("detail") != "1" {
		writeJSON(w, http.StatusOK, results)
		return
	}
	writeJSON(w, http.StatusOK, searchAnswer[T]{Results: results, Sources: sources, Unanswered: unansweredFrom(outcomes), Warnings: limitWarnings(outcomes), Skipped: skipped, Searched: searchedFrom(all, outcomes, indexerCounts(results), skipped), Ignored: ignored.count, IgnoredSamples: ignored.samples})
}
