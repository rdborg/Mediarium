package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/subtitles"
)

// subtitleGetBudget is how long one "get subtitles now" request works before
// it returns what it has (the rest is reported as still waiting).
const subtitleGetBudget = 50 * time.Second

// maxSubtitleItemsPerRequest bounds the item lists the subtitle endpoints take.
const maxSubtitleItemsPerRequest = 5000

// subtitleQuotaPayload is the JSON shape of GET /api/subtitles/quota, also
// embedded in the reply to POST /api/subtitles/get.
type subtitleQuotaPayload struct {
	HasKey      bool    `json:"hasKey"`
	HasAccount  bool    `json:"hasAccount"`
	Limit       int     `json:"limit"`
	Used        int     `json:"used"`
	Remaining   int     `json:"remaining"`
	WindowHours int     `json:"windowHours"`
	ResetsAt    *string `json:"resetsAt"`
	Source      string  `json:"source"` // "reported" or "estimated"
	// MissingItems is the downloaded titles missing a wanted-language subtitle
	// (titles marked "no subtitles wanted" left out); MissingFiles counts
	// title x language pairs.
	MissingItems int    `json:"missingItems"`
	MissingFiles int    `json:"missingFiles"`
	DaysToFinish int    `json:"daysToFinish"`
	Message      string `json:"message"`
	Exceeded     bool   `json:"exceeded"`
}

func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// quotaMessage is the plain-language warning shown when more subtitles are
// wanted than today's downloads allow. It is empty when everything fits.
func quotaMessage(missingFiles int, st subtitles.QuotaState, hasAccount bool) string {
	if missingFiles == 0 || missingFiles <= st.Remaining || st.Limit <= 0 {
		return ""
	}
	days := daysToFinish(missingFiles, st.Limit)
	msg := fmt.Sprintf("%s wanted, %s left today. At %d a day that takes %s.",
		plural(missingFiles, "subtitle"), plural(st.Remaining, "download"), st.Limit, plural(days, "day"))
	switch {
	case !hasAccount:
		msg += fmt.Sprintf(" A free OpenSubtitles account raises it to about %d a day.", subtitles.AccountDailyLimit)
	case st.Limit <= subtitles.AccountDailyLimit:
		msg += " An OpenSubtitles VIP account raises it further."
	}
	return msg
}

func daysToFinish(missingFiles, limit int) int {
	if missingFiles <= 0 || limit <= 0 {
		return 0
	}
	return (missingFiles + limit - 1) / limit
}

func (s *Server) subtitleQuotaPayload() (subtitleQuotaPayload, error) {
	titles, files, err := s.subtitleBacklog()
	if err != nil {
		return subtitleQuotaPayload{}, err
	}
	st := s.subtitleQuota()
	hasKey, hasAccount := s.Subtitles().HasAPIKey(), s.Subtitles().HasCredentials()
	p := subtitleQuotaPayload{
		HasKey: hasKey, HasAccount: hasAccount,
		Limit: st.Limit, Used: st.Used, Remaining: st.Remaining,
		WindowHours: int(subtitles.QuotaWindow / time.Hour),
		Source:      st.Source, Exceeded: st.Exceeded,
		MissingItems: titles, MissingFiles: files,
		DaysToFinish: daysToFinish(files, st.Limit),
	}
	if st.ResetsAt != nil {
		t := st.ResetsAt.UTC().Format(time.RFC3339)
		p.ResetsAt = &t
	}
	if hasKey {
		p.Message = quotaMessage(files, st, hasAccount)
	}
	return p, nil
}

// handleSubtitleQuota reports today's OpenSubtitles download allowance and how
// much is waiting to be fetched. It never contacts OpenSubtitles, and answers
// 409 while subtitles are switched off.
func (s *Server) handleSubtitleQuota(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfSubtitlesOff(w) {
		return
	}
	p, err := s.subtitleQuotaPayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type subtitleItemRef struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

type subtitleItemsRequest struct {
	Items []subtitleItemRef `json:"items"`
	All   bool              `json:"all"`
}

// parseSubtitleItems validates the kinds and ids of a request's item list.
func parseSubtitleItems(refs []subtitleItemRef) ([]subtitles.Item, error) {
	if len(refs) > maxSubtitleItemsPerRequest {
		return nil, fmt.Errorf("That's too many titles at once. Choose at most %d at a time.", maxSubtitleItemsPerRequest)
	}
	out := make([]subtitles.Item, 0, len(refs))
	for _, r := range refs {
		if (r.Kind != "movie" && r.Kind != "episode") || r.ID <= 0 {
			return nil, errors.New(`Each title needs a kind ("movie" or "episode") and an ID.`)
		}
		out = append(out, subtitles.Item{Kind: r.Kind, ID: r.ID})
	}
	return out, nil
}

// handleSubtitlesDismiss marks titles as not needing subtitles. It answers 409
// while subtitles are switched off.
func (s *Server) handleSubtitlesDismiss(w http.ResponseWriter, r *http.Request) {
	s.changeDismissed(w, r, true)
}

// handleSubtitlesRestore undoes a dismissal. It answers 409 while subtitles are
// switched off.
func (s *Server) handleSubtitlesRestore(w http.ResponseWriter, r *http.Request) {
	s.changeDismissed(w, r, false)
}

func (s *Server) changeDismissed(w http.ResponseWriter, r *http.Request, dismiss bool) {
	if s.refuseIfSubtitlesOff(w) {
		return
	}
	var req subtitleItemsRequest
	if err := decodeJSON(r, &req); err != nil || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items is required")
		return
	}
	items, err := parseSubtitleItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if dismiss {
		for _, it := range items {
			if _, err := s.loadSubtitleItem(it.Kind, it.ID); err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, errSubtitleItemNotFound) {
					status = http.StatusBadRequest
				}
				writeError(w, status, err.Error())
				return
			}
		}
		err = s.SubtitleDismissed.Dismiss(items)
	} else {
		err = s.SubtitleDismissed.Restore(items)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := "restored"
	if dismiss {
		key = "dismissed"
	}
	writeJSON(w, http.StatusOK, map[string]int{key: len(items)})
}

// handleSubtitlesGet fetches subtitles now for the named titles (or every
// title missing one, with all=true), with the same rules as the automatic
// sweep except that it also tries titles it failed to find something for
// recently. It works for about a minute at most, stops cleanly when
// OpenSubtitles' daily limit is reached and reports what is left. It answers
// 409 while subtitles are switched off.
func (s *Server) handleSubtitlesGet(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfSubtitlesOff(w) {
		return
	}
	if !s.Subtitles().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your OpenSubtitles API key first (Settings > Info, lists and subtitles > Subtitles).")
		return
	}
	var req subtitleItemsRequest
	if err := decodeJSON(r, &req); err != nil || (!req.All && len(req.Items) == 0) {
		writeError(w, http.StatusBadRequest, "items (or all) is required")
		return
	}
	refs, err := parseSubtitleItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var (
		items      []subtitleItem
		unknownCnt int
	)
	if req.All {
		items, err = s.downloadedSubtitleItems()
	} else {
		for _, ref := range refs {
			it, lerr := s.loadSubtitleItem(ref.Kind, ref.ID)
			switch {
			case errors.Is(lerr, errSubtitleItemNotFound):
				unknownCnt++
			case lerr != nil:
				err = lerr
			default:
				items = append(items, it)
			}
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), subtitleGetBudget)
	defer cancel()
	res, err := s.fetchSubtitles(ctx, items, subtitleFetchOptions{force: true})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	res.skipped += unknownCnt

	quota, err := s.subtitleQuotaPayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"downloaded": res.downloaded,
		"stopped":    res.quota,
		"skipped":    res.skipped,
		"notFound":   res.notFound,
		"remaining":  res.remaining,
		"message":    subtitleGetMessage(res, quota),
		"quota":      quota,
	})
}

func subtitleGetMessage(res subtitleFetchResult, quota subtitleQuotaPayload) string {
	var parts []string
	switch {
	case res.downloaded > 0:
		parts = append(parts, fmt.Sprintf("Got %s.", plural(res.downloaded, "subtitle")))
	case res.remaining == 0 && res.notFound == 0 && res.skipped == 0:
		parts = append(parts, "There was nothing to get.")
	default:
		parts = append(parts, "No subtitles were downloaded.")
	}
	if res.notFound > 0 {
		parts = append(parts, fmt.Sprintf("%d had no match on OpenSubtitles.", res.notFound))
	}
	if res.skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped (marked as not wanted, or the video file is missing).", res.skipped))
	}
	if res.quota {
		limit := "OpenSubtitles' daily download limit is reached."
		if quota.ResetsAt != nil {
			limit = "OpenSubtitles' daily download limit is reached. It resets around " + *quota.ResetsAt + "."
		}
		parts = append(parts, limit)
	}
	if res.timedOut {
		parts = append(parts, "Stopped after about a minute.")
	}
	if res.remaining > 0 {
		parts = append(parts, fmt.Sprintf("%s still waiting. Get them again later.", plural(res.remaining, "subtitle")))
	}
	return strings.Join(parts, " ")
}
