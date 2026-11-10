package download

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Traffic per Usenet server (#167): what each login delivered, counted as the
// articles arrive and saved per day by SaveTraffic, so the Statistics page can
// show how much each provider is used. Only the built-in downloader counts;
// SABnzbd and NZBGet don't say which server served what.

// Traffic is what one server delivered.
type Traffic struct {
	Bytes    int64 // article bodies as they came off the wire
	Articles int64
	Missing  int64 // articles the server didn't have (430 or 423)
}

var traffic = struct {
	mu sync.Mutex
	m  map[string]*Traffic
}{m: map[string]*Traffic{}}

// trafficKey names a login: the same host, port and user is one server.
func trafficKey(cfg ClientConfig) string {
	return strings.ToLower(strings.TrimSpace(cfg.Host)) + ":" + strconv.Itoa(cfg.Port) + ":" + strings.TrimSpace(cfg.Username)
}

func countTraffic(cfg ClientConfig, bytes int64, missing bool) {
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	t := traffic.m[trafficKey(cfg)]
	if t == nil {
		t = &Traffic{}
		traffic.m[trafficKey(cfg)] = t
	}
	if missing {
		t.Missing++
		return
	}
	t.Bytes += bytes
	t.Articles++
}

// takeTraffic hands over what was counted since the last call.
func takeTraffic() map[string]Traffic {
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	out := make(map[string]Traffic, len(traffic.m))
	for k, v := range traffic.m {
		out[k] = *v
	}
	traffic.m = map[string]*Traffic{}
	return out
}

// giveBackTraffic puts counts that could not be saved back, to try again.
func giveBackTraffic(m map[string]Traffic) {
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	for k, v := range m {
		t := traffic.m[k]
		if t == nil {
			t = &Traffic{}
			traffic.m[k] = t
		}
		t.Bytes += v.Bytes
		t.Articles += v.Articles
		t.Missing += v.Missing
	}
}

// SaveTraffic adds what was counted to today's row of each server. Counts for
// a login that is no longer a saved server are dropped.
func (r *Repo) SaveTraffic(now time.Time) error {
	counts := takeTraffic()
	if len(counts) == 0 {
		return nil
	}
	servers, err := r.ListAll()
	if err != nil {
		giveBackTraffic(counts)
		return err
	}
	day := now.UTC().Format("2006-01-02")
	for _, sc := range servers {
		t, ok := counts[trafficKey(sc.Config)]
		if !ok {
			continue
		}
		delete(counts, trafficKey(sc.Config))
		if _, err := r.db.Exec(`INSERT INTO usenet_server_stats (server_id, day, bytes, articles, missing) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(server_id, day) DO UPDATE SET bytes = bytes + excluded.bytes, articles = articles + excluded.articles, missing = missing + excluded.missing`,
			sc.ID, day, t.Bytes, t.Articles, t.Missing); err != nil {
			giveBackTraffic(map[string]Traffic{trafficKey(sc.Config): t})
			return fmt.Errorf("save usenet server traffic: %w", err)
		}
	}
	return nil
}

// ServerTraffic is one server's traffic over a period.
type ServerTraffic struct {
	ServerID int64
	Name     string
	Host     string
	Enabled  bool
	Traffic
}

// TrafficSince adds up each saved server's traffic since the given day (UTC).
// A server with nothing in the period is listed with zeros.
func (r *Repo) TrafficSince(since time.Time) ([]ServerTraffic, error) {
	rows, err := r.db.Query(`SELECT c.id, c.name, c.host, c.enabled, COALESCE(SUM(s.bytes), 0), COALESCE(SUM(s.articles), 0), COALESCE(SUM(s.missing), 0)
		FROM download_clients c LEFT JOIN usenet_server_stats s ON s.server_id = c.id AND s.day >= ?
		GROUP BY c.id ORDER BY c.priority, c.id`, since.UTC().Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("read usenet server traffic: %w", err)
	}
	defer rows.Close()
	var out []ServerTraffic
	for rows.Next() {
		var t ServerTraffic
		if err := rows.Scan(&t.ServerID, &t.Name, &t.Host, &t.Enabled, &t.Bytes, &t.Articles, &t.Missing); err != nil {
			return nil, fmt.Errorf("read usenet server traffic: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TrafficKeepDays is how long a server's daily traffic is kept: a little more
// than two years, so a yearly plan can be looked at month by month (#167).
const TrafficKeepDays = 800

// MonthTraffic is what one server delivered in one calendar month (UTC).
type MonthTraffic struct {
	Month    string // "2026-11"
	ServerID int64
	Traffic
}

// TrafficByMonth adds the saved days up per month and server, from the month
// of since onwards, oldest month first.
func (r *Repo) TrafficByMonth(since time.Time) ([]MonthTraffic, error) {
	rows, err := r.db.Query(`SELECT substr(s.day, 1, 7), s.server_id, SUM(s.bytes), SUM(s.articles), SUM(s.missing)
		FROM usenet_server_stats s JOIN download_clients c ON c.id = s.server_id
		WHERE s.day >= ? GROUP BY substr(s.day, 1, 7), s.server_id ORDER BY 1, 2`, since.UTC().Format("2006-01")+"-01")
	if err != nil {
		return nil, fmt.Errorf("read usenet server traffic by month: %w", err)
	}
	defer rows.Close()
	var out []MonthTraffic
	for rows.Next() {
		var t MonthTraffic
		if err := rows.Scan(&t.Month, &t.ServerID, &t.Bytes, &t.Articles, &t.Missing); err != nil {
			return nil, fmt.Errorf("read usenet server traffic by month: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PruneTraffic forgets days older than keepDays.
func (r *Repo) PruneTraffic(now time.Time, keepDays int) {
	cut := now.UTC().AddDate(0, 0, -keepDays).Format("2006-01-02")
	if _, err := r.db.Exec(`DELETE FROM usenet_server_stats WHERE day < ?`, cut); err != nil {
		slog.Warn("download: prune usenet server traffic", "err", err)
	}
}
