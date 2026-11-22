package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediainfo"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/requests"
)

// mcpCtx is one MCP request: who is asking and what they may do.
type mcpCtx struct {
	s      *Server
	ctx    context.Context
	user   *auth.User
	remote string
}

// mcpToolError is a failure a tool reports to the assistant as text (with
// isError set), as opposed to a protocol error.
type mcpToolError struct{ msg string }

func (e *mcpToolError) Error() string { return e.msg }

func toolErr(format string, a ...any) error { return &mcpToolError{msg: fmt.Sprintf(format, a...)} }

// mcpArgError is arguments that don't fit the tool's schema (wrong type): a
// JSON-RPC "invalid params" error.
type mcpArgError struct{ msg string }

func (e *mcpArgError) Error() string { return e.msg }

// mcpTool is one tool. write marks tools that change something: they need an
// administrator's key that is not read-only, and confirm set to true.
type mcpTool struct {
	name  string
	desc  string
	write bool
	// download marks the tools that pick a release and start its download
	// (find_releases, grab_release) and the ones that add a title or search
	// for it, which ends in a download (add_movie, add_series, search_now).
	// Like write tools they need a key that is not read-only, and they are only
	// there while an administrator has switched on downloads for assistants.
	download bool
	schema   map[string]any
	run      func(c *mcpCtx, args json.RawMessage) (any, error)
}

// ---- Schema helpers ----

type props map[string]any

func schemaOf(p props, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": map[string]any(p)}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func pStr(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func pInt(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }
func pNum(desc string) map[string]any  { return map[string]any{"type": "number", "description": desc} }
func pBool(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
func pEnum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}
func pInts(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "integer"}}
}

// readTool builds a tool that only reads.
func readTool[T any](name, desc string, schema map[string]any, f func(c *mcpCtx, a T) (any, error)) *mcpTool {
	return &mcpTool{name: name, desc: desc, schema: schema, run: func(c *mcpCtx, raw json.RawMessage) (any, error) {
		var a T
		if err := decodeArgs(raw, &a); err != nil {
			return nil, err
		}
		return f(c, a)
	}}
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &mcpArgError{msg: "The arguments don't match the tool's schema: " + err.Error()}
	}
	return nil
}

var (
	toolsOnce sync.Once
	toolsList []*mcpTool
	toolsByID map[string]*mcpTool
)

func mcpTools() []*mcpTool {
	toolsOnce.Do(func() {
		toolsList = append(append(mcpReadTools(), mcpWriteTools()...), mcpDownloadTools()...)
		toolsByID = map[string]*mcpTool{}
		for _, t := range toolsList {
			toolsByID[t.name] = t
		}
	})
	return toolsList
}

func (c *mcpCtx) listTools() []map[string]any {
	out := []map[string]any{}
	for _, t := range mcpTools() {
		if (t.write || t.download) && c.user.ReadOnly() {
			continue // a read-only key doesn't even see them
		}
		if t.download && !c.s.mcpDownloadsOn() {
			continue
		}
		ann := map[string]any{"readOnlyHint": !t.write}
		if t.write {
			ann["destructiveHint"] = false
		}
		out = append(out, map[string]any{"name": t.name, "description": t.desc, "inputSchema": t.schema, "annotations": ann})
	}
	return out
}

// toolResult is the answer to tools/call.
func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}

func (c *mcpCtx) callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "tools/call needs a tool name."}
	}
	mcpTools()
	tool := toolsByID[p.Name]
	if tool == nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "Unknown tool: " + p.Name}
	}
	start := time.Now()
	var (
		out    any
		err    error
		result map[string]any
	)
	switch {
	case (tool.write || tool.download) && c.user.ReadOnly():
		err = toolErr("This API key is read-only, so it can't change anything. Use an administrator's key that is not read-only for this tool.")
	case tool.download && !c.s.mcpDownloadsOn():
		err = toolErr("Downloads are not allowed for AI assistants, and this tool can start one. An administrator can turn on Let assistants pick releases and start downloads under Settings > Media servers and apps > Other apps. Nothing was added, searched for or downloaded.")
	default:
		out, err = c.runTool(tool, coerceArgs(p.Arguments, tool.schema))
	}
	if err != nil {
		// Every failure is an ordinary tool result with isError set and a plain
		// explanation: some clients (Home Assistant) show a protocol error or an
		// empty answer as a bare "error communicating with the MCP server".
		msg := err.Error()
		var argErr *mcpArgError
		var toolE *mcpToolError
		switch {
		case errors.As(err, &argErr), errors.As(err, &toolE):
		case errors.Is(err, context.DeadlineExceeded) || c.ctx.Err() != nil:
			slog.Warn("mcp: a tool ran out of time", "tool", p.Name, "err", err)
			msg = "That took too long and was stopped. Try a narrower question, for example with a smaller limit or a filter."
		default:
			slog.Warn("mcp: a tool failed", "tool", p.Name, "err", err)
			msg = "Something went wrong while running that. An administrator can find the details in the log."
		}
		result = toolResult(msg, true)
	} else {
		text, merr := json.Marshal(out)
		if merr != nil {
			slog.Warn("mcp: could not encode a tool answer", "tool", p.Name, "err", merr)
			result = toolResult("The answer could not be built.", true)
		} else {
			s := string(text)
			if len(s) > mcpMaxText {
				s = s[:mcpMaxText] + "\n[The answer was cut here because it is long. Narrow the question with filters or a smaller limit.]"
			}
			result = toolResult(s, false)
		}
	}
	if took := time.Since(start); took > 5*time.Second {
		slog.Warn("mcp: a tool was slow", "tool", p.Name, "ms", took.Milliseconds())
	}
	// What is logged: the tool, how it went and how big the answer was. Never the arguments.
	slog.Info("mcp: tool call", "tool", p.Name, "write", tool.write, "error", result["isError"], "bytes", len(fmt.Sprint(result["content"])), "ms", time.Since(start).Milliseconds(), "account", c.user.ID)
	return result, nil
}

// runTool runs a tool and turns a panic into an error, so one bad tool call
// can't drop the connection (which a client reports as an empty error).
func (c *mcpCtx) runTool(tool *mcpTool, args json.RawMessage) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("mcp: a tool crashed", "tool", tool.name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			out, err = nil, fmt.Errorf("tool %s crashed: %v", tool.name, r)
		}
	}()
	return tool.run(c, args)
}

// coerceArgs makes the arguments a model sent fit the tool's schema where the
// meaning is plain: numbers and booleans sent as text ("5", "true"), whole
// numbers sent with a fraction, nulls for options left out and numbers or
// booleans sent for text. Anything it can't make sense of is left as it is
// and reported by the normal argument check.
func coerceArgs(raw json.RawMessage, schema map[string]any) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw
	}
	var m map[string]any
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return raw
	}
	props, _ := schema["properties"].(map[string]any)
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}
		spec, _ := props[k].(map[string]any)
		typ, _ := spec["type"].(string)
		switch typ {
		case "integer", "number":
			if s, ok := v.(string); ok {
				if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
					v = f
				} else if strings.TrimSpace(s) == "" {
					delete(m, k)
					continue
				}
			}
			if f, ok := v.(float64); ok && typ == "integer" {
				v = math.Trunc(f)
			}
		case "boolean":
			if s, ok := v.(string); ok {
				switch strings.ToLower(strings.TrimSpace(s)) {
				case "true", "yes", "1":
					v = true
				case "false", "no", "0":
					v = false
				case "":
					delete(m, k)
					continue
				}
			}
		case "string":
			switch x := v.(type) {
			case float64:
				v = strconv.FormatFloat(x, 'f', -1, 64)
			case bool:
				v = strconv.FormatBool(x)
			}
		}
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// canonicalKind maps the many ways a person or a model names a part of the
// library onto movies, tv, music or books; "" when it is none of them.
func canonicalKind(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "movie", "movies", "film", "films", "feature", "features":
		return "movies"
	case "tv", "show", "shows", "series", "tv show", "tv shows", "tv series", "television", "tvshow", "tvshows", "episode", "episodes":
		return "tv"
	case "music", "artist", "artists", "album", "albums", "song", "songs":
		return "music"
	case "book", "books", "ebook", "ebooks", "audiobook", "audiobooks":
		return "books"
	}
	return ""
}

// singularKind is canonicalKind for the tools that say movie, tv, music or book.
func singularKind(s string) string {
	switch canonicalKind(s) {
	case "movies":
		return "movie"
	case "tv":
		return "tv"
	case "music":
		return "music"
	case "books":
		return "book"
	}
	return ""
}

// ---- Calling the web app's own handlers ----

type memResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (m *memResponse) Header() http.Header { return m.header }
func (m *memResponse) WriteHeader(code int) {
	if m.status == 0 {
		m.status = code
	}
}
func (m *memResponse) Write(p []byte) (int, error) {
	if m.status == 0 {
		m.status = http.StatusOK
	}
	return m.body.Write(p)
}

// call runs one of the web app's routes as the account that is asking, with
// the same checks (administrator, read-only, switched-off modules). A body is
// sent as JSON; the answer is decoded into out when given. A refusal or a
// failure comes back as an mcpToolError carrying the app's own sentence.
func (c *mcpCtx) call(method, path string, body, out any) error {
	var rd io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(auth.WithUser(c.ctx, c.user), method, "http://mediarium.internal"+path, rd)
	if err != nil {
		return err
	}
	req.RemoteAddr = c.remote
	req.Header.Set("Content-Type", "application/json")
	rec := &memResponse{header: http.Header{}}
	c.s.mcpRoutes().ServeHTTP(rec, req)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	if rec.status >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.body.Bytes(), &e)
		if e.Error == "" {
			e.Error = fmt.Sprintf("The request failed (%d).", rec.status)
		}
		return &mcpToolError{msg: e.Error}
	}
	if out != nil && rec.body.Len() > 0 {
		if err := json.Unmarshal(rec.body.Bytes(), out); err != nil {
			return fmt.Errorf("read the answer of %s: %w", path, err)
		}
	}
	return nil
}

// ---- Small helpers ----

const gib = 1 << 30

func sizeGB(b int64) float64 { return math.Round(float64(b)/gib*100) / 100 }

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	return min(n, max)
}

// mcpWindow cuts a window out of a list of length total.
func mcpWindow(total, offset, limit int) (int, int) {
	if offset < 0 || offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	return offset, end
}

func containsFold(hay, needle string) bool {
	return needle == "" || strings.Contains(strings.ToLower(hay), strings.ToLower(strings.TrimSpace(needle)))
}

// qualityMatches compares a stored quality ("WEBDL-1080p") with what was asked
// for ("1080p", "4k", "remux").
func qualityMatches(have, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return true
	}
	switch want {
	case "4k", "uhd":
		want = "2160p"
	case "hd":
		want = "1080p"
	}
	return strings.Contains(strings.ToLower(have), want)
}

// groupOf is the release group of a file: from the release name it came from,
// else from the file's own name (titles imported from an existing library
// have no release name).
func groupOf(releaseTitle, filePath string) string {
	if releaseTitle != "" {
		if g := parser.Parse(releaseTitle).Group; g != "" {
			return g
		}
	}
	if filePath == "" {
		return ""
	}
	base := filepath.Base(filePath)
	return parser.Parse(strings.TrimSuffix(base, filepath.Ext(base))).Group
}

func (c *mcpCtx) profileNames() map[int64]string {
	out := map[int64]string{}
	list, err := c.s.QualityRepo.List()
	if err != nil {
		return out
	}
	for _, p := range list {
		out[p.ID] = p.Name
	}
	if def := c.s.defaultProfileID(); def != 0 {
		out[0] = out[def]
	}
	return out
}

func yearOf(y int) any {
	if y == 0 {
		return nil
	}
	return y
}

// ---- The read tools ----

func mcpReadTools() []*mcpTool {
	return []*mcpTool{
		readTool("library_search",
			"Search the library and filter it. kind is movies (default), tv, music or books. Filters: query (part of the title; movies and TV also match the genre names and the description, so \"action\" finds action films), genre (a genre name such as Action, Comedy, Horror, Sci-Fi; movies and TV), quality (such as 1080p, 2160p, 4k, remux, bluray), min_size_gb and max_size_gb (size of the file; for TV the total of the show), release_group (part of the group name, movies and TV), audio_language (movies and TV: has an audio track in this language, such as German or de), missing_subtitle_language (movies and TV: no subtitles in this language, inside the video or as a file beside it) video_codec (movies and TV: h264, hevc, av1, vp9, mpeg2), incomplete (movies and TV: true keeps only titles with a file far shorter than the movie or episode should be, a fake or cut-off download), has_file, monitored. Returns total (all matches) and one page of results (limit up to 100, offset to page). To count, ask with limit 1 and read total. Sizes are in GB (1024 MB steps).",
			schemaOf(props{
				"query": pStr("Part of the title, a genre name or a word from the description (title matches come first)."), "genre": pStr("A genre name such as Action, Comedy, Horror or Sci-Fi (movies and TV)."),
				"kind":    pEnum("Which part of the library. Default movies.", "movies", "tv", "music", "books"),
				"quality": pStr("Quality text such as 1080p, 2160p, 4k, bluray, remux."), "min_size_gb": pNum("Smallest size in GB."), "max_size_gb": pNum("Largest size in GB."),
				"release_group": pStr("Part of the release group's name."), "audio_language": pStr("Only titles with an audio track in this language, for example German, de or ger."),
				"incomplete":                pBool("True: only titles that have a file far shorter than the movie or episode should be (under half of its runtime). False: only titles without such a file."),
				"missing_subtitle_language": pStr("Only titles whose files have no subtitles in this language (inside the video or beside it), for example Dutch or nl."),
				"video_codec":               pStr("Only titles with this video codec: h264, hevc, av1, vp9, mpeg2."), "has_file": pBool("true: only titles with a file. false: only titles without one."),
				"monitored": pBool("true: only monitored. false: only unmonitored."), "limit": pInt("How many to return, 1 to 100. Default 25."), "offset": pInt("Skip this many results."),
			}), toolLibrarySearch),
		readTool("library_stats", "Totals for the library: how many movies, shows, episodes and albums there are and how many have files, the space used, the split by quality, and how movies divide by file size and by release group. Good for overview questions.",
			schemaOf(props{}), toolLibraryStats),
		readTool("lookup_title", "Look a movie or show up on TMDB by name to find its id, for add_movie or add_series. Shows whether it is already in the library.",
			schemaOf(props{"query": pStr("The title."), "kind": pEnum("movie (default) or tv.", "movie", "tv")}, "query"), toolLookupTitle),
		readTool("movie_details", "One movie in detail: file, quality, size, release group, release name, profile, status. Give id (from library_search) or query (the title; if several match, the candidates are listed).",
			schemaOf(props{"id": pInt("Mediarium's id of the movie."), "query": pStr("Part of the title.")}), toolMovieDetails),
		readTool("series_details", "One show in detail: seasons, episodes with and without files, missing aired episodes, qualities, release groups and size. Give id or query.",
			schemaOf(props{"id": pInt("Mediarium's id of the show."), "query": pStr("Part of the title.")}), toolSeriesDetails),
		readTool("wanted_list", "What Mediarium is still looking for. kind missing (no file yet, default) or cutoff (has a file, but below the quality the profile wants). media movie or tv narrows it. Rows are grouped per movie or show.",
			schemaOf(props{"kind": pEnum("missing (default) or cutoff.", "missing", "cutoff"), "media": pEnum("movie or tv.", "movie", "tv"), "query": pStr("Part of the title."),
				"limit": pInt("How many rows, 1 to 100. Default 25."), "offset": pInt("Skip this many rows.")}), toolWantedList),
		readTool("queue_status", "The download queue: what is waiting, downloading, importing or paused, with progress. include_finished adds completed, failed and stopped ones.",
			schemaOf(props{"include_finished": pBool("Also list finished, failed and stopped downloads."), "limit": pInt("How many to list, 1 to 100. Default 25.")}), toolQueueStatus),
		readTool("recent_history", "The newest lines of the Activity feed (grabs, imports, failures, removals). errors_only keeps problems only; query searches the text.",
			schemaOf(props{"errors_only": pBool("Only failures and problems."), "query": pStr("Text the line must contain."), "limit": pInt("How many lines, 1 to 100. Default 25.")}), toolRecentHistory),
		readTool("downloads_failed", "Downloads that failed, with the reason in plain words. Use retry_failed_download to block the release and try another.",
			schemaOf(props{"limit": pInt("How many, 1 to 100. Default 25.")}), toolDownloadsFailed),
		readTool("health_report", "The same health items as the dashboard: what is broken or worth fixing (missing keys, unreachable services, full disks).",
			schemaOf(props{}), toolHealth),
		readTool("requests_list", "Requests people made in the request portal. Filter by user (part of the person's name), status (pending, approved, declined) and kind (movie, tv, music, book). Newest first.",
			schemaOf(props{"user": pStr("Part of the requester's name."), "status": pEnum("pending, approved or declined.", "pending", "approved", "declined"),
				"kind": pEnum("movie, tv, music or book.", "movie", "tv", "music", "book"), "limit": pInt("How many, 1 to 100. Default 25.")}), toolRequestsList),
		readTool("profiles_list", "The quality profiles: allowed qualities, cutoff, size limit, must/must-not terms, preferred terms with their scores, and custom formats with scores. Use the ids or names in set_movie_profile and add_preferred_term.",
			schemaOf(props{}), toolProfilesList),
		readTool("release_group_report", "Which titles do NOT come from a given release group (for example FuN). kind is movies (default) or tv. Titles whose group can't be told are counted and listed as unknown.",
			schemaOf(props{"group": pStr("The release group, such as FuN."), "kind": pEnum("movies (default) or tv.", "movies", "tv"), "limit": pInt("How many titles to list, 1 to 100. Default 50.")}, "group"), toolReleaseGroupReport),
	}
}

// -- library_search --

type librarySearchArgs struct {
	Query        string   `json:"query"`
	Genre        string   `json:"genre"`
	Kind         string   `json:"kind"`
	Quality      string   `json:"quality"`
	MinSizeGB    *float64 `json:"min_size_gb"`
	MaxSizeGB    *float64 `json:"max_size_gb"`
	ReleaseGroup string   `json:"release_group"`
	HasFile      *bool    `json:"has_file"`
	Monitored    *bool    `json:"monitored"`
	Limit        int      `json:"limit"`
	Offset       int      `json:"offset"`
	// AudioLanguage and VideoCodec filter on what the files hold (movies and
	// TV): the language of an audio track ("German", "de", "ger") and the video
	// codec (h264, hevc, av1, vp9, mpeg2).
	AudioLanguage string `json:"audio_language"`
	VideoCodec    string `json:"video_codec"`
	// MissingSubtitleLanguage keeps the titles whose files have no subtitles in
	// this language, neither inside the video nor as a file beside it.
	MissingSubtitleLanguage string `json:"missing_subtitle_language"`
	// Incomplete keeps (true) or leaves out (false) the titles with a file far
	// shorter than the movie or episode should be.
	Incomplete *bool `json:"incomplete"`
}

// mediaWanted says whether any of a title's file summaries satisfies the
// audio_language and video_codec filters; with neither set everything does.
func (a librarySearchArgs) mediaWanted(sums ...library.MediaSummary) bool {
	if sub := mediainfo.LanguageCode(a.MissingSubtitleLanguage); sub != "" {
		// Only titles whose files were read can lack a language.
		if len(sums) == 0 {
			return false
		}
		for _, m := range sums {
			if slices.Contains(mediainfo.Split(m.SLangs), sub) {
				return false
			}
		}
	}
	if a.AudioLanguage == "" && a.VideoCodec == "" {
		return true
	}
	lang := mediainfo.LanguageCode(a.AudioLanguage)
	codec := normalizeVideoCodec(a.VideoCodec)
	for _, m := range sums {
		if lang != "" && !slices.Contains(mediainfo.Split(m.ALangs), lang) {
			continue
		}
		if codec != "" && normalizeVideoCodec(m.VCodec) != codec {
			continue
		}
		return true
	}
	return false
}

// normalizeVideoCodec lets "h264", "H.264", "x264" and "avc" mean the same thing.
func normalizeVideoCodec(s string) string {
	s = strings.ToLower(strings.NewReplacer(".", "", "-", "", " ", "", "_", "").Replace(strings.TrimSpace(s)))
	switch s {
	case "h264", "x264", "avc":
		return "h264"
	case "h265", "x265", "hevc":
		return "hevc"
	case "mpeg2", "mpeg2video":
		return "mpeg2"
	}
	return s
}

// mediaText is a short description of what a title's files hold, for the answer.
func mediaText(sums []library.MediaSummary) (video, audio string) {
	if len(sums) == 0 {
		return "", ""
	}
	p := mediaSummaryOf(sums...)
	return strings.Join(p.VideoCodecs, ", "), strings.Join(p.AudioLangs, ", ")
}

// withMediaNote says, when the answer was filtered on what the files hold,
// how many files have been read so far: the others cannot match yet.
func (c *mcpCtx) withMediaNote(a librarySearchArgs, out map[string]any) map[string]any {
	if a.AudioLanguage == "" && a.VideoCodec == "" && a.MissingSubtitleLanguage == "" && a.Incomplete == nil {
		return out
	}
	if done, total, err := c.s.MovieRepo.MediaInfoCounts(); err == nil && done < total {
		out["note"] = fmt.Sprintf("Mediarium has read the audio and video details of %d of %d files so far; the rest cannot match yet.", done, total)
	}
	return out
}

func (a librarySearchArgs) sizeOK(gb float64, hasFile bool) bool {
	if a.MinSizeGB == nil && a.MaxSizeGB == nil {
		return true
	}
	if !hasFile {
		return false
	}
	return (a.MinSizeGB == nil || gb >= *a.MinSizeGB) && (a.MaxSizeGB == nil || gb <= *a.MaxSizeGB)
}

// textRank says whether a title matches the query: 0 when its title does, 1
// when a genre does, 2 when its description does. ok is false when none does.
func (a librarySearchArgs) textRank(title, overview string, genres []string) (rank int, ok bool) {
	q := strings.TrimSpace(a.Query)
	if q == "" {
		return 0, true
	}
	if containsFold(title, q) {
		return 0, true
	}
	if genreMatches(genres, q) {
		return 1, true
	}
	if containsFold(overview, q) {
		return 2, true
	}
	return 0, false
}

// genreMatches is true when one of the genres contains want ("action" finds
// "Action & Adventure"); sci-fi finds both TMDB spellings.
func genreMatches(genres []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return true
	}
	wants := []string{want}
	switch strings.NewReplacer("-", "", " ", "").Replace(want) {
	case "scifi", "sf", "sciencefiction":
		wants = []string{"science fiction", "sci-fi"}
	case "romcom":
		wants = []string{"romance"}
	case "docu", "doc":
		wants = []string{"documentary"}
	}
	for _, g := range genres {
		g = strings.ToLower(g)
		for _, w := range wants {
			if strings.Contains(g, w) {
				return true
			}
		}
	}
	return false
}

// genreNote says how many titles have no genre information yet, when the
// question depended on genres.
func (a librarySearchArgs) genreNote(out map[string]any, missing int) {
	if missing > 0 && (a.Genre != "" || strings.TrimSpace(a.Query) != "") {
		out["genre_note"] = fmt.Sprintf("%d titles have no genre information yet, so a genre match can miss them.", missing)
	}
}

// emptyNote adds a plain sentence when nothing matched, so the assistant
// doesn't have to guess what an empty list means.
func emptyNote(out map[string]any, total, inLibrary int, what string) map[string]any {
	if total == 0 {
		out["message"] = fmt.Sprintf("No titles matched. The library holds %d %s in total.", inLibrary, what)
	}
	return out
}

func toolLibrarySearch(c *mcpCtx, a librarySearchArgs) (any, error) {
	limit := clampLimit(a.Limit, 25, 100)
	kind := "movies"
	if strings.TrimSpace(a.Kind) != "" {
		if kind = canonicalKind(a.Kind); kind == "" {
			return nil, toolErr("kind must be movies, tv, music or books.")
		}
	}
	switch kind {
	case "movies":
		return c.searchMovies(a, limit)
	case "tv":
		return c.searchSeries(a, limit)
	case "music":
		return c.searchOther(a, limit, "/api/music/artists")
	}
	return c.searchOther(a, limit, "/api/books")
}

func (c *mcpCtx) searchMovies(a librarySearchArgs, limit int) (any, error) {
	list, err := c.s.MovieRepo.List()
	if err != nil {
		return nil, err
	}
	names := c.profileNames()
	now := time.Now()
	type row struct {
		ID           int64   `json:"id"`
		TMDBID       int     `json:"tmdb_id"`
		Title        string  `json:"title"`
		Year         any     `json:"year,omitempty"`
		Status       string  `json:"status"`
		HasFile      bool    `json:"has_file"`
		Quality      string  `json:"quality,omitempty"`
		SizeGB       float64 `json:"size_gb,omitempty"`
		ReleaseGroup string  `json:"release_group,omitempty"`
		Profile      string  `json:"profile,omitempty"`
		Monitored    bool    `json:"monitored"`
		Video        string  `json:"video_codec,omitempty"`
		AudioLangs   string  `json:"audio_languages,omitempty"`
		Genres       string  `json:"genres,omitempty"`
		Incomplete   bool    `json:"looks_incomplete,omitempty"`
		rank         int
		path         string
	}
	rows := []row{}
	media := c.s.mediaSummaryNow(a.MissingSubtitleLanguage != "").movies
	short := c.s.incompleteNow().movies
	noGenres := 0
	sizeFilter := a.MinSizeGB != nil || a.MaxSizeGB != nil
	for _, m := range list {
		if m.Genres == nil {
			noGenres++
		}
		rank, ok := a.textRank(m.Title, m.Overview, m.Genres)
		if !ok || !genreMatches(m.Genres, a.Genre) || !qualityMatches(m.Quality, a.Quality) {
			continue
		}
		hasFile := m.Status == library.StatusDownloaded && m.FilePath != ""
		if a.HasFile != nil && *a.HasFile != hasFile {
			continue
		}
		if a.Monitored != nil && *a.Monitored != m.Monitored {
			continue
		}
		_, isShort := short[m.ID]
		if a.Incomplete != nil && *a.Incomplete != isShort {
			continue
		}
		var gb float64
		if sizeFilter { // reading a file's size can be slow (network disks): only when it decides the answer
			gb = sizeGB(cachedFileSize(m.FilePath, now))
			if !a.sizeOK(gb, hasFile) {
				continue
			}
		}
		group := groupOf(m.ReleaseTitle, m.FilePath)
		if a.ReleaseGroup != "" && !containsFold(group, a.ReleaseGroup) {
			continue
		}
		var sums []library.MediaSummary
		if ms, ok := media[m.ID]; ok {
			sums = []library.MediaSummary{ms}
		}
		if !a.mediaWanted(sums...) {
			continue
		}
		video, audio := mediaText(sums)
		rows = append(rows, row{ID: m.ID, TMDBID: m.TMDBID, Title: m.Title, Year: yearOf(m.Year), Status: string(m.Status), HasFile: hasFile,
			Quality: m.Quality, SizeGB: gb, ReleaseGroup: group, Profile: names[m.ProfileID], Monitored: m.Monitored, Video: video, AudioLangs: audio,
			Genres: strings.Join(m.Genres, ", "), Incomplete: isShort, rank: rank, path: m.FilePath})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].rank != rows[j].rank {
			return rows[i].rank < rows[j].rank
		}
		return strings.ToLower(rows[i].Title) < strings.ToLower(rows[j].Title)
	})
	from, to := mcpWindow(len(rows), a.Offset, limit)
	if !sizeFilter {
		for i := from; i < to; i++ {
			rows[i].SizeGB = sizeGB(cachedFileSize(rows[i].path, now))
		}
	}
	out := c.withMediaNote(a, map[string]any{"kind": "movies", "total": len(rows), "offset": from, "results": rows[from:to]})
	a.genreNote(out, noGenres)
	return emptyNote(out, len(rows), len(list), "movies"), nil
}

func (c *mcpCtx) searchSeries(a librarySearchArgs, limit int) (any, error) {
	data, err := c.s.seriesList(c.ctx)
	if err != nil {
		return nil, err
	}
	names := c.profileNames()
	needEpisodes := a.Quality != "" || a.ReleaseGroup != ""
	type row struct {
		ID         int64   `json:"id"`
		TMDBID     int     `json:"tmdb_id"`
		Title      string  `json:"title"`
		Year       any     `json:"year,omitempty"`
		Episodes   int     `json:"episodes"`
		WithFile   int     `json:"episodes_with_file"`
		Missing    int     `json:"episodes_missing"`
		SizeGB     float64 `json:"size_gb,omitempty"`
		Profile    string  `json:"profile,omitempty"`
		Monitored  bool    `json:"monitored"`
		ShowStatus string  `json:"show_status,omitempty"`
		Video      string  `json:"video_codec,omitempty"`
		AudioLangs string  `json:"audio_languages,omitempty"`
		Genres     string  `json:"genres,omitempty"`
		ShortFiles int     `json:"incomplete_files,omitempty"`
		rank       int
	}
	rows := []row{}
	media := c.s.mediaSummaryNow(a.MissingSubtitleLanguage != "").series
	short := c.s.incompleteNow().series
	noGenres := 0
	for _, sr := range data.list {
		if sr.Genres == nil {
			noGenres++
		}
		rank, ok := a.textRank(sr.Title, sr.Overview, sr.Genres)
		if !ok || !genreMatches(sr.Genres, a.Genre) {
			continue
		}
		if a.Monitored != nil && *a.Monitored != sr.Monitored {
			continue
		}
		if a.Incomplete != nil && *a.Incomplete != (len(short[sr.ID]) > 0) {
			continue
		}
		have := sr.DownloadedCount
		if a.HasFile != nil && *a.HasFile != (have > 0) {
			continue
		}
		gb := sizeGB(data.sizes[sr.ID])
		if !a.sizeOK(gb, have > 0) {
			continue
		}
		if needEpisodes {
			eps, err := c.s.MovieRepo.ListEpisodes(sr.ID)
			if err != nil {
				return nil, err
			}
			hit := false
			for _, e := range eps {
				if e.Status != library.StatusDownloaded {
					continue
				}
				if qualityMatches(e.Quality, a.Quality) && (a.ReleaseGroup == "" || containsFold(groupOf(e.ReleaseTitle, e.FilePath), a.ReleaseGroup)) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		if !a.mediaWanted(media[sr.ID]...) {
			continue
		}
		video, audio := mediaText(media[sr.ID])
		rows = append(rows, row{ID: sr.ID, TMDBID: sr.TMDBID, Title: sr.Title, Year: yearOf(sr.Year), Episodes: sr.EpisodeCount, WithFile: have,
			Missing: max(sr.EpisodeCount-have, 0), SizeGB: gb, Profile: names[sr.ProfileID], Monitored: sr.Monitored, Video: video, AudioLangs: audio,
			Genres: strings.Join(sr.Genres, ", "), ShortFiles: len(short[sr.ID]), rank: rank})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].rank != rows[j].rank {
			return rows[i].rank < rows[j].rank
		}
		return strings.ToLower(rows[i].Title) < strings.ToLower(rows[j].Title)
	})
	from, to := mcpWindow(len(rows), a.Offset, limit)
	out := c.withMediaNote(a, map[string]any{"kind": "tv", "total": len(rows), "offset": from, "results": rows[from:to]})
	a.genreNote(out, noGenres)
	return emptyNote(out, len(rows), len(data.list), "shows"), nil
}

// searchOther lists music artists or books through the web app's own list
// (the music and book modules keep their data in their own shapes).
func (c *mcpCtx) searchOther(a librarySearchArgs, limit int, path string) (any, error) {
	var list []map[string]any
	if err := c.call(http.MethodGet, path+"?brief=1", nil, &list); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, m := range list {
		title, _ := m["title"].(string)
		if title == "" {
			title, _ = m["name"].(string)
		}
		if !containsFold(title, a.Query) {
			continue
		}
		if a.Monitored != nil {
			if mon, ok := m["monitored"].(bool); ok && mon != *a.Monitored {
				continue
			}
		}
		for _, k := range []string{"overview", "posterUrl", "poster", "coverUrl", "cover", "image", "imageUrl", "bio", "description"} {
			delete(m, k)
		}
		rows = append(rows, m)
	}
	from, to := mcpWindow(len(rows), a.Offset, limit)
	out := map[string]any{"kind": strings.TrimPrefix(path, "/api/"), "total": len(rows), "offset": from, "results": rows[from:to],
		"note": "For music and books only query (the title) and monitored are used as filters."}
	return emptyNote(out, len(rows), len(list), strings.TrimPrefix(path, "/api/")), nil
}

// -- library_stats --

func toolLibraryStats(c *mcpCtx, _ struct{}) (any, error) {
	var st libraryStatsPayload
	if err := c.call(http.MethodGet, "/api/stats/library", nil, &st); err != nil {
		return nil, err
	}
	out := map[string]any{
		"movies": st.Movies, "movies_with_file": st.MoviesHave, "movie_space_gb": sizeGB(st.MovieBytes),
		"shows": st.Shows, "episodes": st.Episodes, "episodes_with_file": st.EpisodesHave, "episode_space_gb": sizeGB(st.EpisodeBytes),
		"albums": st.Albums, "albums_with_file": st.AlbumsHave,
	}
	quality := []map[string]any{}
	for _, q := range st.Quality {
		quality = append(quality, map[string]any{"quality": q.Label, "count": q.Count, "size_gb": sizeGB(q.Bytes)})
	}
	out["by_quality"] = quality

	movies, err := c.s.MovieRepo.List()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	buckets := []struct {
		label string
		upTo  float64
		n     int
	}{{"under 5 GB", 5, 0}, {"5 to 15 GB", 15, 0}, {"15 to 30 GB", 30, 0}, {"over 30 GB", math.MaxFloat64, 0}}
	groups := map[string]int{}
	for _, m := range movies {
		if m.Status != library.StatusDownloaded || m.FilePath == "" {
			continue
		}
		gb := float64(cachedFileSize(m.FilePath, now)) / gib
		for i := range buckets {
			if gb < buckets[i].upTo {
				buckets[i].n++
				break
			}
		}
		g := groupOf(m.ReleaseTitle, m.FilePath)
		if g == "" {
			g = "(unknown)"
		}
		groups[g]++
	}
	by := []map[string]any{}
	for _, b := range buckets {
		by = append(by, map[string]any{"size": b.label, "movies": b.n})
	}
	out["movies_by_file_size"] = by
	type gc struct {
		Group  string `json:"release_group"`
		Movies int    `json:"movies"`
	}
	top := []gc{}
	for g, n := range groups {
		top = append(top, gc{g, n})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Movies != top[j].Movies {
			return top[i].Movies > top[j].Movies
		}
		return top[i].Group < top[j].Group
	})
	out["top_release_groups_movies"] = top[:min(len(top), 10)]
	return out, nil
}

// -- lookup_title --

func toolLookupTitle(c *mcpCtx, a struct {
	Query string `json:"query"`
	Kind  string `json:"kind"`
}) (any, error) {
	if strings.TrimSpace(a.Query) == "" {
		return nil, toolErr("Give a title to look up.")
	}
	kind := "movie"
	if canonicalKind(a.Kind) == "tv" {
		kind = "tv"
	}
	var found []importCandidatePayload
	if err := c.call(http.MethodGet, "/api/tmdb/search?kind="+kind+"&q="+url.QueryEscape(a.Query), nil, &found); err != nil {
		return nil, err
	}
	type row struct {
		TMDBID    int    `json:"tmdb_id"`
		Title     string `json:"title"`
		Year      any    `json:"year,omitempty"`
		NameTag   string `json:"name_tag,omitempty"`
		InLibrary bool   `json:"in_library"`
	}
	rows := []row{}
	for _, f := range found[:min(len(found), 10)] {
		in := false
		if kind == "tv" {
			_, in, _ = c.s.MovieRepo.GetSeriesByTMDBID(f.TMDBID)
		} else {
			_, in, _ = c.s.MovieRepo.GetByTMDBID(f.TMDBID)
		}
		rows = append(rows, row{TMDBID: f.TMDBID, Title: f.Title, Year: yearOf(f.Year), NameTag: f.NameTag, InLibrary: in})
	}
	return map[string]any{"kind": kind, "results": rows}, nil
}

// -- movie_details / series_details --

type pickArgs struct {
	ID    int64  `json:"id"`
	Query string `json:"query"`
}

func toolMovieDetails(c *mcpCtx, a pickArgs) (any, error) {
	list, err := c.s.MovieRepo.List()
	if err != nil {
		return nil, err
	}
	var hits []library.Movie
	for _, m := range list {
		if (a.ID != 0 && m.ID == a.ID) || (a.ID == 0 && a.Query != "" && containsFold(m.Title, a.Query)) {
			hits = append(hits, m)
		}
	}
	if a.ID == 0 && strings.TrimSpace(a.Query) == "" {
		return nil, toolErr("Give an id or a query.")
	}
	switch len(hits) {
	case 0:
		return nil, toolErr("No movie in the library matches that.")
	case 1:
	default:
		type cand struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Year  any    `json:"year,omitempty"`
		}
		var cands []cand
		for _, m := range hits[:min(len(hits), 10)] {
			cands = append(cands, cand{m.ID, m.Title, yearOf(m.Year)})
		}
		return map[string]any{"several_matches": true, "candidates": cands, "note": "Ask which one, or call again with the id."}, nil
	}
	m := hits[0]
	names := c.profileNames()
	out := map[string]any{
		"id": m.ID, "tmdb_id": m.TMDBID, "title": m.Title, "year": yearOf(m.Year), "status": string(m.Status), "monitored": m.Monitored,
		"profile": names[m.ProfileID], "release_date": m.ReleaseDate, "genres": m.Genres, "keeps_current_file": m.NoUpgrade,
	}
	if m.Status == library.StatusDownloaded && m.FilePath != "" {
		out["file_name"] = filepath.Base(m.FilePath)
		out["size_gb"] = sizeGB(cachedFileSize(m.FilePath, time.Now()))
		out["quality"] = m.Quality
		out["release_group"] = groupOf(m.ReleaseTitle, m.FilePath)
		if m.ReleaseTitle != "" {
			out["release_name"] = m.ReleaseTitle
		}
	}
	return out, nil
}

func toolSeriesDetails(c *mcpCtx, a pickArgs) (any, error) {
	data, err := c.s.seriesList(c.ctx)
	if err != nil {
		return nil, err
	}
	if a.ID == 0 && strings.TrimSpace(a.Query) == "" {
		return nil, toolErr("Give an id or a query.")
	}
	var hits []library.Series
	for _, sr := range data.list {
		if (a.ID != 0 && sr.ID == a.ID) || (a.ID == 0 && containsFold(sr.Title, a.Query)) {
			hits = append(hits, sr)
		}
	}
	switch len(hits) {
	case 0:
		return nil, toolErr("No show in the library matches that.")
	case 1:
	default:
		type cand struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Year  any    `json:"year,omitempty"`
		}
		var cands []cand
		for _, sr := range hits[:min(len(hits), 10)] {
			cands = append(cands, cand{sr.ID, sr.Title, yearOf(sr.Year)})
		}
		return map[string]any{"several_matches": true, "candidates": cands, "note": "Ask which one, or call again with the id."}, nil
	}
	sr := hits[0]
	eps, err := c.s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC().Format("2006-01-02")
	type season struct {
		Season   int      `json:"season"`
		Episodes int      `json:"episodes"`
		WithFile int      `json:"with_file"`
		Missing  []string `json:"missing_aired,omitempty"`
	}
	seasons := map[int]*season{}
	qualities, groups := map[string]int{}, map[string]int{}
	var bytes int64
	now := time.Now()
	for _, e := range eps {
		se := seasons[e.Season]
		if se == nil {
			se = &season{Season: e.Season}
			seasons[e.Season] = se
		}
		se.Episodes++
		if e.Status == library.StatusDownloaded {
			se.WithFile++
			qualities[e.Quality]++
			g := groupOf(e.ReleaseTitle, e.FilePath)
			if g == "" {
				g = "(unknown)"
			}
			groups[g]++
			bytes += cachedFileSize(e.FilePath, now)
		} else if e.AirDate != "" && e.AirDate <= today && !e.AirsLater(time.Now()) && e.Monitored && len(se.Missing) < 40 {
			se.Missing = append(se.Missing, fmt.Sprintf("S%02dE%02d", e.Season, e.Episode))
		}
	}
	list := make([]*season, 0, len(seasons))
	for _, se := range seasons {
		list = append(list, se)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Season < list[j].Season })
	return map[string]any{
		"id": sr.ID, "tmdb_id": sr.TMDBID, "title": sr.Title, "year": yearOf(sr.Year), "monitored": sr.Monitored,
		"profile": c.profileNames()[sr.ProfileID], "size_gb": sizeGB(bytes), "seasons": list, "qualities": qualities, "release_groups": groups,
	}, nil
}

// -- wanted_list --

func toolWantedList(c *mcpCtx, a struct {
	Kind   string `json:"kind"`
	Media  string `json:"media"`
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}) (any, error) {
	kind := "missing"
	if strings.EqualFold(a.Kind, "cutoff") {
		kind = "cutoff"
	}
	q := url.Values{"group": {"1"}, "kind": {kind}, "limit": {strconv.Itoa(clampLimit(a.Limit, 25, 100))}, "offset": {strconv.Itoa(max(a.Offset, 0))}}
	if media := singularKind(a.Media); media == "movie" || media == "tv" {
		q.Set("media", media)
	}
	if strings.TrimSpace(a.Query) != "" {
		q.Set("q", strings.TrimSpace(a.Query))
	}
	var out map[string]any
	if err := c.call(http.MethodGet, "/api/wanted?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// -- queue and history --

func toolQueueStatus(c *mcpCtx, a struct {
	IncludeFinished bool `json:"include_finished"`
	Limit           int  `json:"limit"`
}) (any, error) {
	var items []queueItemPayload
	if err := c.call(http.MethodGet, "/api/queue", nil, &items); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	type row struct {
		ID       int64   `json:"id"`
		Title    string  `json:"title"`
		Subtitle string  `json:"subtitle,omitempty"`
		Release  string  `json:"release,omitempty"`
		Status   string  `json:"status"`
		Progress float64 `json:"progress_percent"`
		SizeGB   float64 `json:"size_gb,omitempty"`
		Protocol string  `json:"protocol,omitempty"`
		Error    string  `json:"error,omitempty"`
		Note     string  `json:"note,omitempty"`
	}
	rows := []row{}
	for _, it := range items {
		counts[it.Status]++
		finished := it.Status == string(queue.StatusCompleted) || it.Status == string(queue.StatusFailed) || it.Status == string(queue.StatusStopped)
		if finished && !a.IncludeFinished {
			continue
		}
		rows = append(rows, row{ID: it.ID, Title: it.Title, Subtitle: it.Subtitle, Release: it.ReleaseTitle, Status: it.Status,
			Progress: math.Round(it.ProgressPct*10) / 10, SizeGB: sizeGB(it.SizeBytes), Protocol: it.Protocol, Error: it.Error, Note: it.Note})
	}
	limit := clampLimit(a.Limit, 25, 100)
	return map[string]any{"counts_by_status": counts, "listed": len(rows[:min(len(rows), limit)]), "total_listed_kind": len(rows), "items": rows[:min(len(rows), limit)]}, nil
}

func toolRecentHistory(c *mcpCtx, a struct {
	ErrorsOnly bool   `json:"errors_only"`
	Query      string `json:"query"`
	Limit      int    `json:"limit"`
}) (any, error) {
	limit := clampLimit(a.Limit, 25, 100)
	fetch := limit
	if a.ErrorsOnly {
		fetch = 1000
	}
	q := url.Values{"limit": {strconv.Itoa(fetch)}}
	if strings.TrimSpace(a.Query) != "" {
		q.Set("q", strings.TrimSpace(a.Query))
	}
	var entries []activityEntryPayload
	if err := c.call(http.MethodGet, "/api/activity?"+q.Encode(), nil, &entries); err != nil {
		return nil, err
	}
	type row struct {
		When    string `json:"when"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	rows := []row{}
	for _, e := range entries {
		if a.ErrorsOnly && !isProblemEvent(e.EventType, e.Message) {
			continue
		}
		rows = append(rows, row{When: e.CreatedAt, Type: e.EventType, Message: e.Message})
		if len(rows) == limit {
			break
		}
	}
	return map[string]any{"entries": rows}, nil
}

// isProblemEvent tells a failure in the Activity feed from the rest.
func isProblemEvent(eventType, message string) bool {
	switch eventType {
	case "failed", "conflict", "skipped":
		return true
	}
	m := strings.ToLower(message)
	return strings.Contains(m, "fail") || strings.Contains(m, "error") || strings.Contains(m, "couldn't") || strings.Contains(m, "could not")
}

func toolDownloadsFailed(c *mcpCtx, a struct {
	Limit int `json:"limit"`
}) (any, error) {
	var items []queueItemPayload
	if err := c.call(http.MethodGet, "/api/queue", nil, &items); err != nil {
		return nil, err
	}
	type row struct {
		QueueID     int64  `json:"queue_id"`
		Title       string `json:"title"`
		Subtitle    string `json:"subtitle,omitempty"`
		Release     string `json:"release"`
		Protocol    string `json:"protocol,omitempty"`
		Indexer     string `json:"indexer,omitempty"`
		Reason      string `json:"reason,omitempty"`
		Note        string `json:"note,omitempty"`
		CompletedAt string `json:"failed_at,omitempty"`
	}
	rows := []row{}
	for _, it := range items {
		if it.Status != string(queue.StatusFailed) {
			continue
		}
		rows = append(rows, row{QueueID: it.ID, Title: it.Title, Subtitle: it.Subtitle, Release: it.ReleaseTitle, Protocol: it.Protocol,
			Indexer: it.IndexerName, Reason: it.Error, Note: it.Note, CompletedAt: it.CompletedAt})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CompletedAt > rows[j].CompletedAt })
	limit := clampLimit(a.Limit, 25, 100)
	return map[string]any{"total": len(rows), "failed": rows[:min(len(rows), limit)],
		"next_step": "retry_failed_download with a queue_id blocklists that release and searches for another one."}, nil
}

// -- health, requests, profiles --

func toolHealth(c *mcpCtx, _ struct{}) (any, error) {
	var out struct {
		Items []healthItem `json:"items"`
	}
	if err := c.call(http.MethodGet, "/api/health", nil, &out); err != nil {
		return nil, err
	}
	type row struct {
		Level  string `json:"level"`
		Title  string `json:"title"`
		Impact string `json:"impact,omitempty"`
	}
	rows := []row{}
	for _, h := range out.Items {
		rows = append(rows, row{Level: h.Level, Title: h.Title, Impact: h.Impact})
	}
	return map[string]any{"ok": len(rows) == 0, "items": rows}, nil
}

func toolRequestsList(c *mcpCtx, a struct {
	User   string `json:"user"`
	Status string `json:"status"`
	Kind   string `json:"kind"`
	Limit  int    `json:"limit"`
}) (any, error) {
	all, err := c.s.Requests.List(0)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	type row struct {
		ID        int64  `json:"id"`
		Kind      string `json:"kind"`
		Title     string `json:"title"`
		Year      any    `json:"year,omitempty"`
		Requester string `json:"requested_by"`
		Status    string `json:"status"`
		Asked     string `json:"asked_at"`
		Decided   string `json:"decided_at,omitempty"`
		Note      string `json:"note,omitempty"`
		Available string `json:"available_at,omitempty"`
		FourK     bool   `json:"four_k,omitempty"`
	}
	rows := []row{}
	pending := 0
	for _, r := range all {
		if a.User != "" && !containsFold(r.Requester, a.User) {
			continue
		}
		if a.Status != "" && !strings.EqualFold(r.Status, a.Status) {
			continue
		}
		if a.Kind != "" && !strings.EqualFold(r.Kind, singularKind(a.Kind)) {
			continue
		}
		if r.Status == requests.Pending {
			pending++
		}
		rows = append(rows, row{ID: r.ID, Kind: r.Kind, Title: r.Title, Year: yearOf(r.Year), Requester: r.Requester, Status: r.Status,
			Asked: r.CreatedAt.UTC().Format(time.RFC3339), Decided: r.DecidedAt, Note: r.Note, Available: r.AvailableAt, FourK: r.FourK})
	}
	limit := clampLimit(a.Limit, 25, 100)
	return map[string]any{"total": len(rows), "pending_among_them": pending, "requests": rows[:min(len(rows), limit)]}, nil
}

func toolProfilesList(c *mcpCtx, _ struct{}) (any, error) {
	var list profileListPayload
	if err := c.call(http.MethodGet, "/api/quality-profiles", nil, &list); err != nil {
		return nil, err
	}
	formatNames := map[int64]string{}
	if fs, err := c.s.FormatRepo.List(); err == nil {
		for _, f := range fs {
			formatNames[f.ID] = f.Name
		}
	}
	type scored struct {
		Name  string `json:"name"`
		Score int    `json:"score"`
	}
	type row struct {
		ID             int64              `json:"id"`
		Name           string             `json:"name"`
		Default        bool               `json:"default_for_movies"`
		DefaultTV      bool               `json:"default_for_tv,omitempty"`
		Allowed        []string           `json:"allowed_qualities"`
		Cutoff         string             `json:"cutoff"`
		UpgradeAllowed bool               `json:"upgrades_allowed"`
		MaxSizeGB      float64            `json:"max_size_gb,omitempty"`
		MustContain    []string           `json:"must_contain,omitempty"`
		MustNotContain []string           `json:"must_not_contain,omitempty"`
		Preferred      []preferredPayload `json:"preferred_terms"`
		Formats        []scored           `json:"custom_formats,omitempty"`
		MinFormatScore int                `json:"min_format_score,omitempty"`
		// QualityOrder is set when the profile has its own order: best first, a group as "Name: a + b".
		QualityOrder []string `json:"quality_order,omitempty"`
		InUse        int      `json:"titles_using_it"`
	}
	rows := []row{}
	for _, p := range list.Profiles {
		r := row{ID: p.ID, Name: p.Name, Default: p.ID == list.DefaultID, DefaultTV: list.DefaultTVID != 0 && p.ID == list.DefaultTVID, Allowed: p.Allowed,
			Cutoff: p.Cutoff, UpgradeAllowed: p.UpgradeAllowed, MaxSizeGB: p.MaxSizeGB, MustContain: p.MustContain, MustNotContain: p.MustNotContain,
			Preferred: p.Preferred, MinFormatScore: p.MinFormatScore, InUse: p.InUse}
		if r.Preferred == nil {
			r.Preferred = []preferredPayload{}
		}
		for _, it := range p.QualityOrder {
			if it.Name != "" {
				r.QualityOrder = append(r.QualityOrder, it.Name+": "+strings.Join(it.Tiers, " + "))
			} else if len(it.Tiers) > 0 {
				r.QualityOrder = append(r.QualityOrder, it.Tiers[0])
			}
		}
		for _, fs := range p.FormatScores {
			name := formatNames[fs.FormatID]
			if name == "" {
				name = "format " + strconv.FormatInt(fs.FormatID, 10)
			}
			r.Formats = append(r.Formats, scored{name, fs.Score})
		}
		rows = append(rows, r)
	}
	return map[string]any{"profiles": rows}, nil
}

// -- release_group_report --

func toolReleaseGroupReport(c *mcpCtx, a struct {
	Group string `json:"group"`
	Kind  string `json:"kind"`
	Limit int    `json:"limit"`
}) (any, error) {
	group := strings.TrimSpace(a.Group)
	if group == "" {
		return nil, toolErr("Give the release group to check, such as FuN.")
	}
	limit := clampLimit(a.Limit, 50, 100)
	if canonicalKind(a.Kind) == "tv" {
		return c.groupReportTV(group, limit)
	}
	movies, err := c.s.MovieRepo.List()
	if err != nil {
		return nil, err
	}
	type row struct {
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Year    any    `json:"year,omitempty"`
		Group   string `json:"release_group"`
		Quality string `json:"quality,omitempty"`
	}
	other := []row{}
	withFile, from, unknown := 0, 0, 0
	for _, m := range movies {
		if m.Status != library.StatusDownloaded || m.FilePath == "" {
			continue
		}
		withFile++
		g := groupOf(m.ReleaseTitle, m.FilePath)
		switch {
		case strings.EqualFold(g, group):
			from++
			continue
		case g == "":
			unknown++
			g = "(unknown)"
		}
		other = append(other, row{m.ID, m.Title, yearOf(m.Year), g, m.Quality})
	}
	sort.SliceStable(other, func(i, j int) bool { return strings.ToLower(other[i].Title) < strings.ToLower(other[j].Title) })
	return map[string]any{
		"kind": "movies", "group": group, "movies_with_file": withFile, "from_group": from, "not_from_group": len(other), "group_unknown": unknown,
		"titles_not_from_group": other[:min(len(other), limit)],
	}, nil
}

func (c *mcpCtx) groupReportTV(group string, limit int) (any, error) {
	list, err := c.s.MovieRepo.ListSeries()
	if err != nil {
		return nil, err
	}
	type row struct {
		ID           int64  `json:"id"`
		Title        string `json:"title"`
		WithFile     int    `json:"episodes_with_file"`
		FromGroup    int    `json:"from_group"`
		NotFromGroup int    `json:"not_from_group"`
	}
	rows := []row{}
	totalFile, totalFrom := 0, 0
	for _, sr := range list {
		eps, err := c.s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			return nil, err
		}
		r := row{ID: sr.ID, Title: sr.Title}
		for _, e := range eps {
			if e.Status != library.StatusDownloaded || e.FilePath == "" {
				continue
			}
			r.WithFile++
			if strings.EqualFold(groupOf(e.ReleaseTitle, e.FilePath), group) {
				r.FromGroup++
			} else {
				r.NotFromGroup++
			}
		}
		totalFile += r.WithFile
		totalFrom += r.FromGroup
		if r.NotFromGroup > 0 {
			rows = append(rows, r)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].NotFromGroup != rows[j].NotFromGroup {
			return rows[i].NotFromGroup > rows[j].NotFromGroup
		}
		return strings.ToLower(rows[i].Title) < strings.ToLower(rows[j].Title)
	})
	return map[string]any{
		"kind": "tv", "group": group, "episodes_with_file": totalFile, "from_group": totalFrom, "not_from_group": totalFile - totalFrom,
		"shows_with_episodes_not_from_group": len(rows), "shows": rows[:min(len(rows), limit)],
	}, nil
}
