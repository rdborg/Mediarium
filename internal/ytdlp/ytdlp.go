// Package ytdlp runs a yt-dlp program that the person supplied, for the
// Channel archiver. Mediarium never ships, downloads or updates yt-dlp: this
// package only starts the file at the path it was given, with fixed arguments.
//
// Every argument list is built as a slice (never a shell string), the address
// always comes after "--" so it can never be read as an option, and a title or
// any other text from a listing is never part of an argument or a path.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Formats the archiver can keep a video in.
const (
	FormatVideo = "video" // video up to 1080p, as mp4
	FormatMP3   = "mp3"   // audio only
	FormatM4A   = "m4a"   // audio only
)

// ValidFormat reports whether f is one of the formats above.
func ValidFormat(f string) bool { return f == FormatVideo || f == FormatMP3 || f == FormatM4A }

// IsAudio reports whether the format keeps only the sound.
func IsAudio(f string) bool { return f == FormatMP3 || f == FormatM4A }

// DefaultHosts are the sites the archiver follows without any extra setting.
var DefaultHosts = []string{"youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be"}

var errHost = errors.New("Use the name of a public website, like example.com.")

var hostName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// CleanHost checks a host name an administrator wants to allow and returns it
// in lower case. A plain name only: no address, no port, no path, and never an
// IP number or a local name.
func CleanHost(h string) (string, error) {
	h = strings.ToLower(strings.TrimSpace(h))
	if len(h) > 253 || !hostName.MatchString(h) {
		return "", errHost
	}
	{
		last := h[strings.LastIndex(h, ".")+1:]
		if last == "local" || last == "localhost" || last == "internal" || last == "lan" || last == "home" || last == "test" {
			return "", errHost
		}
		if strings.Trim(last, "0123456789") == "" {
			return "", errHost
		}
	}
	return h, nil
}

var channelRoot = regexp.MustCompile(`^/(@[^/]+|channel/[^/]+|c/[^/]+|user/[^/]+)/?$`)

// ValidateURL checks an address to follow: https only, no sign-in details, no
// other port, and a host that is on the list (DefaultHosts plus extraHosts).
// It returns the address in the form that is stored and passed to yt-dlp. A
// bare YouTube channel address gets /videos so that the listing holds videos,
// not the channel's tabs.
func ValidateURL(raw string, extraHosts []string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 500 || strings.ContainsAny(raw, " \t\r\n\x00") {
		return "", errors.New("That address isn't right. Paste the address of a channel or a playlist.")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("That address isn't right. Paste the address of a channel or a playlist.")
	}
	if u.Scheme != "https" {
		return "", errors.New("Only https addresses can be followed.")
	}
	if p := u.Port(); u.User != nil || (p != "" && p != "443") {
		return "", errors.New("An address with a user name or a port can't be followed.")
	}
	host := strings.ToLower(u.Hostname())
	ok := false
	for _, h := range append(append([]string{}, DefaultHosts...), extraHosts...) {
		if host == h {
			ok = true
		}
	}
	if !ok {
		return "", errors.New("That website isn't on the list. Only YouTube is allowed unless you add the website under Other websites.")
	}
	u.Host = host
	u.Fragment = ""
	if channelRoot.MatchString(u.Path) && strings.HasSuffix(host, "youtube.com") {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/videos"
	}
	return u.String(), nil
}

// ---- running yt-dlp

// Runner starts one yt-dlp program.
type Runner struct {
	Path string // absolute path of the executable the person supplied
	// Timeout is the longest one download may take; zero means 2 hours.
	Timeout time.Duration
}

var versionLine = regexp.MustCompile(`^[0-9][0-9A-Za-z._+-]{3,39}$`)

// CheckPath explains what is wrong with a path before it is used: it has to be
// an absolute path to an existing file, not a folder.
func CheckPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" || strings.ContainsRune(p, 0) || !filepath.IsAbs(p) {
		return errors.New("Give the full path of the yt-dlp file, like /usr/local/bin/yt-dlp.")
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return errors.New("There is no yt-dlp file at that path. In Docker, use the path inside the container.")
	}
	return nil
}

func (r Runner) env() []string {
	env := []string{"LANG=C.UTF-8", "PYTHONIOENCODING=utf-8"}
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SYSTEMROOT", "XDG_CACHE_HOME", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// run starts the program with args (never through a shell). stdout goes to
// out, stderr is kept to its last few kilobytes.
func (r Runner) run(ctx context.Context, dir string, args []string, out io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, r.Path, args...)
	cmd.Dir = dir
	cmd.Env = r.env()
	cmd.WaitDelay = 5 * time.Second
	errBuf := &tailBuffer{max: 4096}
	cmd.Stderr = errBuf
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout = out
	err := cmd.Run()
	return errBuf.String(), err
}

// Version runs yt-dlp --version and returns the version it prints. It fails
// when the file doesn't start, takes too long, or prints something that isn't
// a version.
func (r Runner) Version(ctx context.Context) (string, error) {
	if err := CheckPath(r.Path); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out limitedBuffer
	out.max = 1024
	stderr, err := r.run(ctx, "", VersionArgs(), &out)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("Nothing came back from yt-dlp in 20 seconds.")
		}
		return "", fmt.Errorf("The yt-dlp file couldn't be started: %s", firstLine(stderr, err))
	}
	v := strings.TrimSpace(out.String())
	if !versionLine.MatchString(v) {
		return "", errors.New("That file doesn't look like yt-dlp: it didn't answer with a version.")
	}
	return v, nil
}

// Video is one entry of a listing.
type Video struct {
	ID       string
	Title    string
	Channel  string
	Duration int    // seconds, 0 when unknown
	URL      string // the video's own address when the listing gave one
}

// ListLimit is the most videos one listing reads (the newest ones).
const ListLimit = 200

// List asks yt-dlp for the newest videos of a followed address without
// downloading any. The address must have passed ValidateURL.
func (r Runner) List(ctx context.Context, address string) ([]Video, string, error) {
	if err := CheckPath(r.Path); err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	pr, pw := io.Pipe()
	type res struct {
		v   []Video
		err error
	}
	done := make(chan res, 1)
	go func() {
		v, err := ParseListing(pr)
		_, _ = io.Copy(io.Discard, pr)
		done <- res{v, err}
	}()
	stderr, err := r.run(ctx, "", ListArgs(address), pw)
	_ = pw.Close()
	got := <-done
	if err != nil && len(got.v) == 0 {
		if ctx.Err() != nil {
			return nil, stderr, errors.New("Reading the list took too long.")
		}
		return nil, stderr, fmt.Errorf("Reading the list failed: %s", firstLine(stderr, err))
	}
	// Some entries can fail (private or removed videos) while the rest is fine.
	return got.v, stderr, got.err
}

// DownloadResult is what one finished download left in its folder.
type DownloadResult struct {
	Media string // the audio or video file
	Info  string // the .info.json next to it, "" when there is none
}

// Download fetches one video into dir (an empty folder made for it). It uses a
// fixed output name built from the id, so nothing from the video's own text
// reaches a path.
func (r Runner) Download(ctx context.Context, address, format, dir string) (DownloadResult, string, error) {
	if err := CheckPath(r.Path); err != nil {
		return DownloadResult{}, "", err
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Hour
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stderr, err := r.run(ctx, dir, DownloadArgs(address, format), nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return DownloadResult{}, stderr, errors.New("The download took too long and was stopped.")
		}
		if ctx.Err() != nil {
			return DownloadResult{}, stderr, ctx.Err()
		}
		return DownloadResult{}, stderr, fmt.Errorf("The download failed: %s", firstLine(stderr, err))
	}
	res, err := findOutput(dir, format)
	return res, stderr, err
}

// ---- arguments

// VersionArgs is the argument list of the test.
func VersionArgs() []string { return []string{"--ignore-config", "--version"} }

// ListArgs lists the newest videos of address as one JSON line each.
func ListArgs(address string) []string {
	return []string{
		"--ignore-config", "--no-warnings", "--ignore-errors", "--no-cache-dir",
		"--flat-playlist", "--dump-json",
		"--playlist-end", fmt.Sprint(ListLimit),
		"--socket-timeout", "30",
		"--", address,
	}
}

// DownloadArgs fetches one video into the folder yt-dlp runs in. The name is
// only the video's id; the program moves it to its final name afterwards.
func DownloadArgs(address, format string) []string {
	a := []string{
		"--ignore-config", "--no-warnings", "--no-playlist", "--no-cache-dir",
		"--no-progress", "--newline", "--retries", "3", "--socket-timeout", "30",
		"--write-info-json", "--no-write-comments",
		"-o", "%(id)s.%(ext)s",
	}
	switch format {
	case FormatMP3, FormatM4A:
		a = append(a, "-f", "bestaudio/best", "-x", "--audio-format", format, "--audio-quality", "0")
	default:
		a = append(a, "-f", "bv*[height<=1080][ext=mp4]+ba[ext=m4a]/bv*[height<=1080]+ba/b[height<=1080]/b", "--merge-output-format", "mp4")
	}
	return append(a, "--", address)
}

// ---- reading what yt-dlp printed

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidID reports whether id is safe to keep and to put in a file name.
func ValidID(id string) bool { return videoID.MatchString(id) }

type listLine struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Channel  string  `json:"channel"`
	Uploader string  `json:"uploader"`
	Type     string  `json:"_type"`
	IEKey    string  `json:"ie_key"`
	Duration float64 `json:"duration"`
	URL      string  `json:"url"`
	Web      string  `json:"webpage_url"`
}

// ParseListing reads yt-dlp's --dump-json lines. Lines that aren't JSON, have
// no safe id or are playlists or channel tabs themselves are skipped. The same
// id twice is kept once.
func ParseListing(r io.Reader) ([]Video, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []Video
	seen := map[string]bool{}
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var l listLine
		if json.Unmarshal(line, &l) != nil {
			continue
		}
		if !ValidID(l.ID) || seen[l.ID] || l.Type == "playlist" || strings.Contains(l.IEKey, "Tab") {
			continue
		}
		seen[l.ID] = true
		name := l.Channel
		if name == "" {
			name = l.Uploader
		}
		addr := l.Web
		if addr == "" {
			addr = l.URL
		}
		out = append(out, Video{ID: l.ID, Title: l.Title, Channel: name, Duration: int(l.Duration), URL: addr})
	}
	return out, sc.Err()
}

// VideoAddress is the address of one video, for the download. On YouTube it is
// built from the id, so a listing can't send a download somewhere else. On
// another allowed website it is the address the listing gave, which has to pass
// the same check as a followed address. followed is the followed address.
func VideoAddress(followed string, v Video, extraHosts []string) (string, error) {
	if !ValidID(v.ID) {
		return "", errors.New("unsafe video id")
	}
	fu, err := url.Parse(followed)
	if err != nil {
		return "", err
	}
	if isYouTube(fu.Hostname()) {
		return "https://www.youtube.com/watch?v=" + v.ID, nil
	}
	if v.URL != "" {
		if u, err := ValidateURL(v.URL, extraHosts); err == nil {
			return u, nil
		}
	}
	return "", errors.New("That video's address isn't one that can be downloaded.")
}

func isYouTube(host string) bool {
	return host == "youtu.be" || host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")
}

func findOutput(dir, format string) (DownloadResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return DownloadResult{}, err
	}
	var res DownloadResult
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		low := strings.ToLower(n)
		switch {
		case strings.HasSuffix(low, ".info.json"):
			res.Info = filepath.Join(dir, n)
		case strings.HasSuffix(low, ".part"), strings.HasSuffix(low, ".ytdl"), strings.HasSuffix(low, ".json"), strings.HasSuffix(low, ".jpg"), strings.HasSuffix(low, ".webp"), strings.HasSuffix(low, ".png"):
		default:
			if res.Media == "" || (IsAudio(format) && strings.HasSuffix(low, "."+format)) {
				res.Media = filepath.Join(dir, n)
			}
		}
	}
	if res.Media == "" {
		return DownloadResult{}, errors.New("The download finished but left no file.")
	}
	return res, nil
}

// ---- names

var badName = regexp.MustCompile(`[\x00-\x1f<>:"/\\|?*]`)

// SafeName makes text from a listing usable as one file or folder name: no
// separators, no control characters, no leading dots, shortened to max bytes
// (cut on a letter, not in the middle of one). An empty result is fallback.
func SafeName(s, fallback string, max int) string {
	s = badName.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimLeft(s, ". ")
	for len(s) > max {
		r := []rune(s)
		s = string(r[:len(r)-1])
	}
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return fallback
	}
	return s
}

// ---- small buffers

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

// limitedBuffer keeps the first max bytes and throws the rest away.
type limitedBuffer struct {
	b   bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.b.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.b.Write(p[:room])
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string { return l.b.String() }

// firstLine is the last line of stderr that says ERROR (or the last line at
// all), short enough for a log, or the Go error when stderr was empty.
func firstLine(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	pick := ""
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "ERROR") {
			pick = l
		}
	}
	if pick == "" && len(lines) > 0 {
		pick = strings.TrimSpace(lines[len(lines)-1])
	}
	if pick == "" {
		pick = err.Error()
	}
	if len(pick) > 300 {
		pick = pick[:300]
	}
	return pick
}
