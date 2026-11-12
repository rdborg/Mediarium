package indexers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netproxy"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/problems"
)

// Protocol distinguishes Usenet (Newznab) from torrent (Torznab) indexers.
// Both speak the same underlying XML API shape (see the package doc
// comment in newznab.go); this only changes how results
// are tagged/handled downstream (search UI protocol icon, which download
// engine a grab uses).
type Protocol string

const (
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
	// ProtocolSoulseek is music from Soulseek through the person's slskd
	// (api/soulseek.go); no indexer gives it, only the album search.
	ProtocolSoulseek Protocol = "soulseek"
)

// Kind says how an indexer is searched.
type Kind string

const (
	KindNewznab   Kind = "newznab"   // Newznab API (Usenet)
	KindTorznab   Kind = "torznab"   // Torznab API (torrents, e.g. Prowlarr/Jackett)
	KindCardigann Kind = "cardigann" // a site driven by a community definition
)

// Instance is one user-configured indexer: a Newznab/Torznab API (base URL
// + API key), or a definition-based site (definition id + the settings the
// definition asks for).
type Instance struct {
	ID           int64
	Name         string
	Kind         Kind
	DefinitionID string
	BaseURL      string
	APIKey       string
	Protocol     Protocol
	Enabled      bool
	// Settings are a definition-based indexer's values (secrets included,
	// decrypted); nil for API indexers.
	Settings map[string]string
	// Cardigann runs definition-based indexers; the repo sets it.
	Cardigann *CardigannManager
	// Tracker keeps the indexer's back-off and search counters; the repo sets
	// it. Nil (an instance built by hand) means neither is kept.
	Tracker *Tracker
	// LastTestError is the message of the last failed Test ("" = passed or
	// never tested); LastTestAt is when it ran.
	LastTestError string
	LastTestAt    time.Time
	// Priority is a number from PriorityMin to PriorityMax; a lower number is
	// preferred (PriorityDefault when nothing was chosen).
	Priority int
	// RequestsPerDay is the daily limit a person set for the indexer; 0 = none
	// set, so what the indexer itself reported (if anything) is used.
	RequestsPerDay int
	// MinSeeders is the fewest seeders a torrent release from this indexer needs
	// to be picked by itself; 0 = off. Usenet indexers ignore it.
	MinSeeders int
	// ExtraCategories are category numbers the indexer is searched in besides the
	// standard ones, per kind of media (see ExtraCategories).
	ExtraCategories ExtraCategories
	// TorrentRoute is where torrents from this indexer are downloaded: one of
	// the Route constants, empty for the usual order.
	TorrentRoute string
	// SecretError is set when the saved key or password of this indexer could
	// not be read (usually a secret.key that does not belong to the database).
	// The indexer is still listed, so it can be fixed, but it is never searched.
	SecretError string
}

// markUnreadable records that this indexer's saved secret cannot be read. The
// message doubles as its last test result, so the Indexers page shows it.
func (i *Instance) markUnreadable() {
	i.SecretError = fmt.Sprintf("Mediarium can't read the saved key of %s: re-enter it.", i.Name)
	i.LastTestError = i.SecretError
}

// Indexer priorities are numbers from 1 to 50, like Prowlarr: a lower
// number is tried and picked first. Between two equally good releases the
// one from the lower-numbered indexer is picked.
const (
	PriorityMin     = 1
	PriorityMax     = 50
	PriorityDefault = 25
	// The three old levels, kept for files and scripts that still use them.
	PriorityPreferred = 10
	PriorityNormal    = PriorityDefault
	PriorityLast      = 40
)

// ParsePriority reads a priority from JSON or YAML input: a whole number
// from 1 to 50, a numeric string, or one of the old words (preferred,
// normal, last resort). ok is false for anything else.
func ParsePriority(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, x >= PriorityMin && x <= PriorityMax
	case int64:
		return int(x), x >= PriorityMin && x <= PriorityMax
	case float64:
		n := int(x)
		return n, float64(n) == x && n >= PriorityMin && n <= PriorityMax
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		switch strings.NewReplacer("_", " ", "-", " ").Replace(s) {
		case "preferred":
			return PriorityPreferred, true
		case "normal":
			return PriorityNormal, true
		case "last resort", "last", "lastresort":
			return PriorityLast, true
		}
		n, err := strconv.Atoi(s)
		return n, err == nil && n >= PriorityMin && n <= PriorityMax
	}
	return 0, false
}

// Where a torrent is downloaded. An indexer can ask for one of them; empty
// follows the usual order (client, then debrid, then built-in).
const (
	RouteClient  = "client"  // the download client (qBittorrent and the like)
	RouteDebrid  = "debrid"  // the debrid service
	RouteBuiltin = "builtin" // the built-in torrent client
)

// CleanTorrentRoute keeps a stored route to the known ones; anything else is
// the usual order.
func CleanTorrentRoute(r string) string {
	switch r {
	case RouteClient, RouteDebrid, RouteBuiltin:
		return r
	}
	return ""
}

// MinSeedersMax is the largest minimum a person can set.
const MinSeedersMax = 1000

// ClampMinSeeders keeps a stored minimum inside 0 to MinSeedersMax.
func ClampMinSeeders(n int) int {
	return min(max(n, 0), MinSeedersMax)
}

// ClampPriority returns p, or PriorityDefault when p is outside 1 to 50
// (nothing chosen).
func ClampPriority(p int) int {
	if p < PriorityMin || p > PriorityMax {
		return PriorityDefault
	}
	return p
}

// IsCardigann reports whether the instance is a definition-based site.
func (inst Instance) IsCardigann() bool { return inst.Kind == KindCardigann }

// Outcome is one indexer's contribution to an aggregated search — Err is
// set instead of aborting the whole search, so a single bad/slow indexer
// never stalls the others.
type Outcome struct {
	IndexerName string
	// IndexerID, Protocol and Elapsed say which indexer it was, whether it is a
	// torrent or Usenet one, and how long its search took (zero when it was
	// not asked).
	IndexerID int64
	Protocol  Protocol
	Elapsed   time.Duration
	Results   []Result
	Err       error
	// Skipped is set when the indexer was not asked because it is paused after
	// repeated failures (see Tracker); PausedUntil says until when. Err is nil
	// then: a paused indexer is not a new problem.
	Skipped     bool
	PausedUntil time.Time
	Health      Health
	// LimitNote is set when the indexer's daily limit is used up. The indexer
	// is then skipped by automatic searches (Skipped is true, Err is nil: it is
	// not a problem). In a search a person started it is still asked, and the
	// note is the warning to show.
	LimitNote string
}

// apostropheless drops the apostrophes and quote marks inside a name.
var apostropheless = strings.NewReplacer("'", "", "’", "", "‘", "", "`", "", "´", "")

// SearchText cleans what is sent to an indexer. Release names are written without
// apostrophes ("Hells.Kitchen.US.S24E01", "Marvels.Agents.of.SHIELD"), and an
// indexer that is asked for "Hell's Kitchen" often answers with nothing, so the
// apostrophes go before the search.
func SearchText(q string) string {
	return strings.Join(strings.Fields(apostropheless.Replace(q)), " ")
}

// SearchAll queries every enabled instance concurrently and returns one
// Outcome per instance ("one unified result list ... clearly
// tagged by source").
func SearchAll(ctx context.Context, instances []Instance, query string, categories []int) []Outcome {
	query = SearchText(query)
	var enabled []Instance
	for _, inst := range instances {
		if inst.Enabled && inst.SecretError == "" {
			enabled = append(enabled, inst)
		}
	}

	manual := isManual(ctx)
	outcomes := make([]Outcome, len(enabled))
	var wg sync.WaitGroup
	for i, inst := range enabled {
		if until, paused := inst.Tracker.PausedUntil(inst.ID); paused {
			h, _ := inst.Tracker.Health(inst.ID)
			outcomes[i] = Outcome{IndexerName: inst.Name, Skipped: true, PausedUntil: until, Health: h}
			continue
		}
		var limitNote string
		if q := inst.Tracker.Quota(inst.ID, inst.RequestsPerDay); q.Blocked {
			limitNote = q.Message(nil)
			if !manual {
				outcomes[i] = Outcome{IndexerName: inst.Name, Skipped: true, PausedUntil: q.BackAt, LimitNote: limitNote}
				continue
			}
			slog.Warn("indexers: asking an indexer that has reached its daily limit, because a person started the search", "indexer", inst.Name)
		}
		wg.Add(1)
		go func(i int, inst Instance, limitNote string) {
			defer wg.Done()
			started := time.Now()
			defer func() { outcomes[i].Elapsed = time.Since(started) }()
			// A bug, or a site that finds one, must cost this indexer's
			// results only, not the whole program.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("indexers: search crashed", "indexer", inst.Name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
					outcomes[i] = Outcome{IndexerName: inst.Name, Err: fmt.Errorf("%s: the search failed because of an internal error", inst.Name)}
				}
			}()
			timeout := FeedWait(inst.BaseURL)
			if inst.IsCardigann() {
				timeout = inst.Cardigann.SearchBudget()
			}
			// One search after another on an indexer, never a burst.
			if err := inst.Tracker.Pace(ctx, inst.ID, inst.Tracker.SearchGap(inst.ID)); err != nil {
				if errors.Is(err, ErrSearchQueueFull) {
					err = fmt.Errorf("%s: %w, so this search was skipped", inst.Name, err)
				}
				outcomes[i] = Outcome{IndexerName: inst.Name, Err: err}
				return
			}
			inst.Tracker.CountRequest(inst.ID, 1)
			reqCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var (
				results  []Result
				err      error
				laterErr error // a page after the first failed (Newznab)
			)
			// The indexer's own extra categories go along with the standard ones.
			categories := inst.ExtraCategories.WithExtraCategories(categories)
			if inst.IsCardigann() {
				if inst.Cardigann == nil {
					err = fmt.Errorf("%s: sites from the site list are not available", inst.Name)
				} else {
					results, err = inst.Cardigann.Search(reqCtx, inst, query, categories)
				}
			} else {
				client := NewNewznabClient(inst.Name, inst.BaseURL, inst.APIKey)
				results, err = client.Search(reqCtx, query, categories)
				laterErr = client.LaterPageErr
				if client.ExtraRequests > 0 {
					inst.Tracker.CountRequest(inst.ID, client.ExtraRequests)
				}
				// An indexer that takes the show's or film's id is asked by id too.
				if err == nil {
					byID, n := client.SearchByID(reqCtx, categories)
					if n > 0 {
						inst.Tracker.CountRequest(inst.ID, n)
					}
					results = mergeByLink(results, byID)
				}
			}
			for j := range results {
				results[j].Protocol = inst.Protocol
				if results[j].Torrent {
					results[j].Protocol = ProtocolTorrent
				}
				results[j].Priority = inst.Priority
				if results[j].Protocol == ProtocolTorrent {
					results[j].MinSeeders = inst.MinSeeders
					results[j].TorrentRoute = inst.TorrentRoute
				}
			}
			outcomes[i] = Outcome{IndexerName: inst.Name, Results: results, Err: err, LimitNote: limitNote}
			// A search cut short by the caller, or waiting for the VPN, says
			// nothing about the indexer.
			if ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, netproxy.ErrWaitingForVPN) {
				inst.Tracker.RecordSearch(inst.ID, results, err)
				if lerr := laterErr; err == nil && lerr != nil && !errors.Is(lerr, context.Canceled) && !errors.Is(lerr, context.DeadlineExceeded) {
					inst.Tracker.RecordLaterPage(inst.ID, lerr)
				}
			}
		}(i, inst, limitNote)
	}
	wg.Wait()
	for i, inst := range enabled {
		outcomes[i].IndexerID, outcomes[i].Protocol = inst.ID, inst.Protocol
	}
	if ctx.Err() == nil {
		for _, o := range outcomes {
			noteSearchProblem(o)
		}
	}
	return outcomes
}

// noteSearchProblem puts an indexer that failed in the problem log: it
// limited the requests, refused the key, could not be reached or gave an error.
// A wait for the VPN isn't logged: health already reports the VPN.
func noteSearchProblem(o Outcome) {
	// a search skipped because others were waiting says nothing about the indexer
	if o.Err == nil || IsDailyLimit(o.Err) || errors.Is(o.Err, context.Canceled) || errors.Is(o.Err, ErrSearchQueueFull) || errors.Is(o.Err, netproxy.ErrWaitingForVPN) {
		return
	}
	problems.Record(problems.Problem{Code: problems.IndexerCode(o.Err), Subject: o.IndexerName, Message: ProblemMessage(o), Err: o.Err})
}

// ProblemMessage says in plain words why an indexer did not answer:
// "Example says you have made too many requests.", "Could not reach Example."
// It is empty when the indexer answered.
func ProblemMessage(o Outcome) string {
	if o.Err == nil {
		return ""
	}
	if msg := netproxy.WaitMessage(o.Err); msg != "" {
		return msg
	}
	if addr, ok := FlareSolverrDown(o.Err); ok {
		return FlareSolverrDownMessage(addr)
	}
	if status, ok := GoneStatus(o.Err); ok {
		return GoneMessage(status)
	}
	if IsDailyLimit(o.Err) {
		return fmt.Sprintf("The daily limit of requests for %s is used up. It is used again after midnight (UTC).", o.IndexerName)
	}
	switch problems.IndexerCode(o.Err) {
	case problems.CodeIndexerRateLimited:
		return fmt.Sprintf("%s says you have made too many requests.", o.IndexerName)
	case problems.CodeIndexerAuthRefused:
		return fmt.Sprintf("%s did not accept the API key or login.", o.IndexerName)
	case problems.CodeIndexerUnreachable:
		return fmt.Sprintf("Could not reach %s.", o.IndexerName)
	}
	return fmt.Sprintf("%s returned an error.", o.IndexerName)
}

// MergeResults flattens successful outcomes into a single slice, ignoring
// (but not discarding — callers can still inspect Outcome.Err) failed ones.
func MergeResults(outcomes []Outcome) []Result {
	var all []Result
	// Several queries for one thing (S00E05 and the special's title, an
	// anime's season and absolute number) often bring back the same release
	// from the same indexer: it is listed once.
	seen := map[[3]string]bool{}
	// The same release often comes from several indexers: parse each name once.
	parsed := map[string]*parser.Release{}
	for _, o := range outcomes {
		if o.Err != nil {
			continue
		}
		for _, r := range o.Results {
			key := [3]string{r.IndexerName, r.DownloadURL, r.Title}
			if r.DownloadURL != "" && seen[key] {
				continue
			}
			seen[key] = true
			p := parsed[r.Title]
			if p == nil {
				rel := parser.Parse(r.Title)
				p = &rel
				parsed[r.Title] = p
			}
			r.parsed = p
			all = append(all, r)
		}
	}
	return all
}
