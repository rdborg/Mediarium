package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The two tools that let an assistant choose a release and start its download:
// find_releases and grab_release. They are off until an administrator switches
// on Let assistants pick releases and start downloads (setting mcp.downloads),
// and like every change they need a key that is not read-only.
//
// Starting a download takes two calls. The first grab_release call changes
// nothing: it answers with what would be downloaded and a confirm_token that
// lives a few minutes, works once, and belongs to that account and that
// release. Only a second call carrying the token starts the download. The
// assistant never sees a download address: it names a release by the short id
// find_releases gave it, and the address stays on the server.

const (
	mcpOfferTTL   = 30 * time.Minute // how long a release found by find_releases can still be picked
	mcpTokenTTL   = 5 * time.Minute  // how long a confirm_token lives
	mcpOfferMax   = 500              // most releases remembered at once
	mcpTokenMax   = 100              // most unused tokens remembered at once
	mcpFindLimit  = 10               // releases find_releases lists unless asked for more
	mcpFindMax    = 25
	mcpTokenStamp = "mcpgrab_"
)

// mcpOffer is one release an assistant was shown.
type mcpOffer struct {
	userID  int64
	movie   bool
	titleID int64
	label   string // "Salt & Static (2019)", for the preview
	release string
	url     string // the opaque reference of the search results; resolved when the grab runs
	size    int64
	proto   string
	season  int
	episode int
	expires time.Time
}

type mcpGrabToken struct {
	userID  int64
	offerID string
	expires time.Time
}

// mcpDownloads remembers the releases shown to assistants and the confirm
// tokens handed out. It lives in memory: a restart makes every unused token
// and release id invalid, which is the safe direction. The zero value works.
type mcpDownloads struct {
	mu     sync.Mutex
	offers map[string]*mcpOffer
	tokens map[string]*mcpGrabToken
	now    func() time.Time // tests move the clock
}

func (d *mcpDownloads) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

func mcpRandom(prefix string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("mcp: no randomness: " + err.Error())
	}
	return prefix + hex.EncodeToString(b)
}

// prune drops what has run out. Callers hold d.mu.
func (d *mcpDownloads) prune(now time.Time) {
	for k, o := range d.offers {
		if !now.Before(o.expires) {
			delete(d.offers, k)
		}
	}
	for k, t := range d.tokens {
		if !now.Before(t.expires) {
			delete(d.tokens, k)
		}
	}
}

// addOffer remembers a release and returns the id the assistant uses for it.
func (d *mcpDownloads) addOffer(o mcpOffer) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.clock()
	d.prune(now)
	if d.offers == nil {
		d.offers = map[string]*mcpOffer{}
	}
	if len(d.offers) >= mcpOfferMax {
		// Make room by dropping the one that runs out first.
		var oldest string
		for k, v := range d.offers {
			if oldest == "" || v.expires.Before(d.offers[oldest].expires) {
				oldest = k
			}
		}
		delete(d.offers, oldest)
	}
	o.expires = now.Add(mcpOfferTTL)
	id := mcpRandom("r", 6)
	d.offers[id] = &o
	return id
}

// offer returns a release this account was shown and that has not run out.
func (d *mcpDownloads) offer(userID int64, id string) (mcpOffer, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune(d.clock())
	o := d.offers[id]
	if o == nil || o.userID != userID {
		return mcpOffer{}, false
	}
	return *o, true
}

// mint makes the confirm token for a release. An earlier token for the same
// release and account stops working: only the newest preview can be confirmed.
func (d *mcpDownloads) mint(userID int64, offerID string) (token string, ttl time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.clock()
	d.prune(now)
	if d.tokens == nil {
		d.tokens = map[string]*mcpGrabToken{}
	}
	for k, t := range d.tokens {
		if t.userID == userID && t.offerID == offerID {
			delete(d.tokens, k)
		}
	}
	if len(d.tokens) >= mcpTokenMax {
		for k := range d.tokens { // map order is random: drop any one
			delete(d.tokens, k)
			break
		}
	}
	token = mcpRandom(mcpTokenStamp, 16)
	d.tokens[token] = &mcpGrabToken{userID: userID, offerID: offerID, expires: now.Add(mcpTokenTTL)}
	return token, mcpTokenTTL
}

// redeem uses a token up. It is taken out of the store whether or not it fits,
// so a wrong guess can not be tried again; it succeeds only for the account and
// the release it was made for, before it runs out.
func (d *mcpDownloads) redeem(userID int64, offerID, token string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.clock()
	t := d.tokens[token]
	delete(d.tokens, token)
	d.prune(now)
	return t != nil && t.userID == userID && t.offerID == offerID && now.Before(t.expires)
}

// forget drops a release once it has been grabbed.
func (d *mcpDownloads) forget(offerID string) {
	d.mu.Lock()
	delete(d.offers, offerID)
	d.mu.Unlock()
}

func mcpDownloadTools() []*mcpTool {
	return []*mcpTool{
		{
			name: "find_releases", download: true,
			desc: "Search the indexers for downloadable releases of a title that is in the library (add it first with add_movie or add_series). " +
				"Give movie_id, or series_id with season (and episode for one episode) or complete true for packs of several seasons. Lists the best matches first, each with a release_id. Nothing is downloaded: " +
				"to download one, call grab_release with its release_id.",
			schema: schemaOf(props{
				"movie_id":  pInt("A movie's id (library_search)."),
				"series_id": pInt("A show's id (library_search)."),
				"season":    pInt("The season to look for (shows)."),
				"episode":   pInt("One episode of that season (shows). Left out, season packs are listed."),
				"complete":  pBool("Shows: list packs that hold several seasons or the whole show instead."),
				"limit":     pInt("How many releases to list. Default 10, at most 25."),
			}),
			run: func(c *mcpCtx, raw json.RawMessage) (any, error) {
				var a struct {
					MovieID  int64 `json:"movie_id"`
					SeriesID int64 `json:"series_id"`
					Season   *int  `json:"season"`
					Episode  int   `json:"episode"`
					Complete bool  `json:"complete"`
					Limit    int   `json:"limit"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				return c.findReleases(a.MovieID, a.SeriesID, a.Season, a.Episode, a.Complete, a.Limit)
			},
		},
		{
			name: "grab_release", write: true, download: true,
			desc: "Download a release that find_releases listed. Takes two calls. The first, with only release_id, changes nothing: it answers with what would be downloaded and a confirm_token. " +
				"Tell the person what would be downloaded and ask. Only if they say yes, call grab_release again with the same release_id and that confirm_token. The token works once and runs out after a few minutes.",
			schema: schemaOf(props{
				"release_id":    pStr("The release_id from find_releases."),
				"confirm_token": pStr("The confirm_token from the first call. Leave it out the first time."),
			}, "release_id"),
			run: func(c *mcpCtx, raw json.RawMessage) (any, error) {
				var a struct {
					ReleaseID    string `json:"release_id"`
					ConfirmToken string `json:"confirm_token"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				return c.grabRelease(strings.TrimSpace(a.ReleaseID), strings.TrimSpace(a.ConfirmToken))
			},
		},
	}
}

// findReleases runs the web app's own interactive search for one library title.
func (c *mcpCtx) findReleases(movieID, seriesID int64, season *int, episode int, complete bool, limit int) (any, error) {
	if (movieID > 0) == (seriesID > 0) {
		return nil, toolErr("Give either movie_id or series_id. Find them with library_search; add the title first if it is not in the library.")
	}
	if limit <= 0 {
		limit = mcpFindLimit
	}
	limit = min(limit, mcpFindMax)
	var (
		path, label string
		seasonN     int
		movie       = movieID > 0
		id          = movieID
	)
	if movie {
		m, err := c.s.MovieRepo.Get(movieID)
		if err != nil {
			return nil, toolErr("There is no movie with id %d.", movieID)
		}
		label = titleYear(m.Title, m.Year)
		path = fmt.Sprintf("/api/movies/%d/search", movieID)
	} else {
		id = seriesID
		sr, err := c.s.MovieRepo.GetSeries(seriesID)
		if err != nil {
			return nil, toolErr("There is no show with id %d.", seriesID)
		}
		label = titleYear(sr.Title, sr.Year)
		q := url.Values{}
		switch {
		case complete:
			q.Set("complete", "1")
		case season == nil:
			return nil, toolErr("Say which season to look for (season), or set complete for packs of several seasons.")
		default:
			seasonN = *season
			q.Set("season", strconv.Itoa(seasonN))
			if episode > 0 {
				q.Set("episode", strconv.Itoa(episode))
			}
		}
		path = fmt.Sprintf("/api/series/%d/search?%s", seriesID, q.Encode())
	}
	var found []searchResultPayload
	if err := c.call(http.MethodGet, path, nil, &found); err != nil {
		return nil, err
	}
	type release struct {
		ReleaseID  string   `json:"release_id"`
		Title      string   `json:"title"`
		Indexer    string   `json:"indexer"`
		Protocol   string   `json:"protocol"`
		SizeGB     float64  `json:"size_gb"`
		Resolution string   `json:"resolution,omitempty"`
		Source     string   `json:"source,omitempty"`
		Codec      string   `json:"codec,omitempty"`
		Group      string   `json:"group,omitempty"`
		Seeders    int      `json:"seeders,omitempty"`
		Season     int      `json:"season,omitempty"`
		Episodes   []int    `json:"episodes,omitempty"`
		Best       bool     `json:"best_match,omitempty"`
		Problems   []string `json:"problems,omitempty"`
	}
	out := []release{}
	skipped := 0
	for _, r := range found {
		if r.Blocklisted || r.DownloadURL == "" {
			skipped++
			continue
		}
		if len(out) >= limit {
			break
		}
		offer := mcpOffer{userID: c.user.ID, movie: movie, titleID: id, label: label, release: r.Title, url: r.DownloadURL, size: r.SizeBytes, proto: r.Protocol, season: seasonN, episode: episode}
		if !movie && r.Season > 0 {
			offer.season = r.Season
		}
		problems := r.Rejections
		if len(problems) > 3 {
			problems = problems[:3]
		}
		out = append(out, release{
			ReleaseID: c.s.mcp.dl.addOffer(offer), Title: r.Title, Indexer: r.IndexerName, Protocol: r.Protocol,
			SizeGB: float64(r.SizeBytes) / (1 << 30), Resolution: r.Resolution, Source: r.Source, Codec: r.Codec, Group: r.Group,
			Seeders: r.Seeders, Season: r.Season, Episodes: r.Episodes, Best: r.Best, Problems: problems,
		})
	}
	res := map[string]any{"title": label, "found": len(found), "releases": out}
	if skipped > 0 {
		res["left_out"] = fmt.Sprintf("%d release(s) were left out because they failed before and are blocked.", skipped)
	}
	if len(out) == 0 {
		res["note"] = "No release was found. The title may be too new, or no indexer has it."
	} else {
		res["next_step"] = "Nothing has been downloaded. To download one, call grab_release with its release_id, then ask the person before confirming."
	}
	return res, nil
}

func mcpGrabNotValid() error {
	return toolErr("That confirm_token is not valid. It may have been used already, belong to another release or have run out. Nothing was downloaded. Call grab_release with only the release_id to get a new one.")
}

// grabRelease is both steps of grab_release.
func (c *mcpCtx) grabRelease(releaseID, token string) (any, error) {
	if releaseID == "" {
		return nil, toolErr("release_id is needed. Get it from find_releases.")
	}
	dl := &c.s.mcp.dl
	offer, ok := dl.offer(c.user.ID, releaseID)
	if !ok {
		if token != "" {
			dl.redeem(c.user.ID, releaseID, token) // a token for a release that is gone is used up too
		}
		return nil, toolErr("There is no release with that release_id, or it has run out (they are kept for %d minutes). Nothing was downloaded. Search again with find_releases.", int(mcpOfferTTL/time.Minute))
	}
	// The title must still be there; the web route checks it again when the grab runs.
	if offer.movie {
		if _, err := c.s.MovieRepo.Get(offer.titleID); err != nil {
			return nil, toolErr("%s is no longer in the library. Nothing was downloaded.", offer.label)
		}
	} else if _, err := c.s.MovieRepo.GetSeries(offer.titleID); err != nil {
		return nil, toolErr("%s is no longer in the library. Nothing was downloaded.", offer.label)
	}

	if token == "" {
		tok, ttl := dl.mint(c.user.ID, releaseID)
		kind := "show"
		if offer.movie {
			kind = "movie"
		}
		return map[string]any{
			"changed":         false,
			"preview":         true,
			"would_do":        fmt.Sprintf("Download the release %q (%.1f GB, %s) for the %s %s and add it to the download queue.", offer.release, float64(offer.size)/(1<<30), offer.proto, kind, offer.label),
			"confirm_token":   tok,
			"expires_seconds": int(ttl / time.Second),
			"next_step":       "Nothing has been downloaded. Tell the person what would be downloaded and ask if they want it. Only if they say yes, call grab_release again with the same release_id and this confirm_token.",
		}, nil
	}

	if !dl.redeem(c.user.ID, releaseID, token) {
		return nil, mcpGrabNotValid()
	}
	var out struct {
		QueueID int64 `json:"queueId"`
	}
	var err error
	if offer.movie {
		err = c.call(http.MethodPost, fmt.Sprintf("/api/movies/%d/grab", offer.titleID), map[string]any{
			"releaseTitle": offer.release, "downloadUrl": offer.url, "sizeBytes": offer.size, "protocol": offer.proto,
		}, &out)
	} else {
		body := map[string]any{"releaseTitle": offer.release, "downloadUrl": offer.url, "sizeBytes": offer.size, "protocol": offer.proto}
		if offer.season > 0 {
			body["season"] = offer.season
		}
		if offer.episode > 0 {
			body["episode"] = offer.episode
		}
		err = c.call(http.MethodPost, fmt.Sprintf("/api/series/%d/grab", offer.titleID), body, &out)
	}
	if err != nil {
		return nil, err
	}
	dl.forget(releaseID)
	return map[string]any{
		"changed":  true,
		"done":     fmt.Sprintf("Started downloading %q for %s.", offer.release, offer.label),
		"queue_id": out.QueueID,
		"next":     "Follow it with queue_status.",
	}, nil
}
