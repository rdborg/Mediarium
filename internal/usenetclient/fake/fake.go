// Package fake holds small stand-ins for SABnzbd and NZBGet for tests: just
// enough of their web APIs for Mediarium's code to talk to. Not used by the
// app itself.
package fake

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Result is how a download ends in the stand-ins.
type Result struct {
	Failed  bool
	Message string // SABnzbd's fail_message; for NZBGet the history status, such as FAILURE/HEALTH
	Path    string // where the finished files are, as the client sees them
}

// Add is one release sent to a stand-in.
type Add struct {
	Name     string
	Category string
	Priority string
	URL      string
	NZB      string // the file's content when it was uploaded
}

// SAB imitates SABnzbd's /api?mode=...&output=json&apikey=... A new job waits
// in the queue for one look, then downloads for Steps looks, then moves to the
// history with Result.
type SAB struct {
	mu         sync.Mutex
	APIKey     string
	Categories []string
	Steps      int
	Result     Result
	// Hold keeps every job downloading for ever.
	Hold bool
	// Finished is the time a job reaches the history (SABnzbd's "completed");
	// zero means now.
	Finished time.Time
	Adds     []Add
	Actions  []string
	jobs     []*sabJob
	next     int
}

type sabJob struct {
	id      string
	name    string
	cat     string
	looks   int
	paused  bool
	history bool
	failed  bool
	msg     string
	path    string
	done    time.Time
	del     bool
}

func (s *SAB) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path != "/api" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	mode := q.Get("mode")
	if mode == "version" {
		fmt.Fprint(w, `{"version":"4.3.2"}`)
		return
	}
	if q.Get("apikey") != s.APIKey {
		fmt.Fprint(w, `{"status": false, "error": "API Key Incorrect"}`)
		return
	}
	switch mode {
	case "get_cats":
		cats := append([]string{"*"}, s.Categories...)
		_ = json.NewEncoder(w).Encode(map[string]any{"categories": cats})
	case "addurl", "addfile":
		a := Add{Name: q.Get("nzbname"), Category: q.Get("cat"), Priority: q.Get("priority")}
		if mode == "addurl" {
			a.URL = q.Get("name")
		} else {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				fmt.Fprint(w, `{"status": false, "error": "no file"}`)
				return
			}
			f, _, err := r.FormFile("name")
			if err != nil {
				fmt.Fprint(w, `{"status": false, "error": "no file"}`)
				return
			}
			b, _ := io.ReadAll(f)
			a.NZB = string(b)
		}
		s.Adds = append(s.Adds, a)
		s.next++
		id := fmt.Sprintf("SABnzbd_nzo_%04d", s.next)
		s.jobs = append(s.jobs, &sabJob{id: id, name: a.Name, cat: a.Category})
		fmt.Fprintf(w, `{"status": true, "nzo_ids": [%q]}`, id)
	case "queue":
		if name := q.Get("name"); name != "" {
			s.Actions = append(s.Actions, "queue "+name+" "+q.Get("value")+" files="+q.Get("del_files"))
			for _, j := range s.jobs {
				if j.id != q.Get("value") || j.history {
					continue
				}
				switch name {
				case "pause":
					j.paused = true
				case "resume":
					j.paused = false
				case "delete":
					j.del = true
				}
			}
			s.drop()
			fmt.Fprint(w, `{"status": true, "nzo_ids": []}`)
			return
		}
		s.queueAnswer(w)
	case "history":
		if name := q.Get("name"); name != "" {
			s.Actions = append(s.Actions, "history "+name+" "+q.Get("value")+" files="+q.Get("del_files"))
			for _, j := range s.jobs {
				if j.id == q.Get("value") && j.history {
					j.del = true
				}
			}
			s.drop()
			fmt.Fprint(w, `{"status": true}`)
			return
		}
		s.historyAnswer(w, q.Get("nzo_ids"))
	default:
		fmt.Fprint(w, `{"status": false, "error": "not implemented"}`)
	}
}

func (s *SAB) drop() {
	kept := s.jobs[:0]
	for _, j := range s.jobs {
		if !j.del {
			kept = append(kept, j)
		}
	}
	s.jobs = kept
}

func (s *SAB) queueAnswer(w http.ResponseWriter) {
	var slots []map[string]any
	speed := "0.0"
	for _, j := range s.jobs {
		if j.history {
			continue
		}
		j.looks++
		if !s.Hold && !j.paused && j.looks > s.Steps+1 {
			j.history = true
			j.failed, j.msg, j.path = s.Result.Failed, s.Result.Message, s.Result.Path
			j.done = s.Finished
			if j.done.IsZero() {
				j.done = time.Now()
			}
			continue
		}
		status, pct := "Queued", 0
		switch {
		case j.paused:
			status = "Paused"
		case j.looks > 1:
			status, pct = "Downloading", min(99, (j.looks-1)*100/(s.Steps+1))
			speed = "2048.0"
		}
		slots = append(slots, map[string]any{
			"nzo_id": j.id, "filename": j.name, "status": status, "cat": orStar(j.cat), "mb": "100.00", "mbleft": fmt.Sprintf("%.2f", float64(100-pct)),
			"percentage": strconv.Itoa(pct), "timeleft": "0:01:30",
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"queue": map[string]any{"paused": false, "kbpersec": speed, "slots": slots}})
}

func orStar(c string) string {
	if c == "" {
		return "*"
	}
	return c
}

func (s *SAB) historyAnswer(w http.ResponseWriter, ids string) {
	var slots []map[string]any
	for _, j := range s.jobs {
		if !j.history {
			continue
		}
		if ids != "" && !strings.Contains(ids, j.id) {
			continue
		}
		status := "Completed"
		if j.failed {
			status = "Failed"
		}
		slots = append(slots, map[string]any{
			"nzo_id": j.id, "name": j.name, "status": status, "category": orStar(j.cat), "bytes": 104857600,
			"fail_message": j.msg, "storage": j.path, "completed": j.done.Unix(),
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"history": map[string]any{"slots": slots}})
}

// Pause pauses a job by hand, as a person in the client's own page would.
func (s *SAB) Pause(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.id == id {
			j.paused = true
		}
	}
}

// Snapshot returns what was sent and which controls were pressed.
func (s *SAB) Snapshot() (adds []Add, actions []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Add(nil), s.Adds...), append([]string(nil), s.Actions...)
}

// Seed puts a job straight into the history, finished at Finished (or now),
// and returns its id.
func (s *SAB) Seed(name, category string, res Result) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	j := &sabJob{id: fmt.Sprintf("SABnzbd_nzo_%04d", s.next), name: name, cat: category, history: true,
		failed: res.Failed, msg: res.Message, path: res.Path, done: s.Finished}
	if j.done.IsZero() {
		j.done = time.Now()
	}
	s.jobs = append(s.jobs, j)
	return j.id
}

// Remove deletes every job, as a person clearing the client by hand would.
func (s *SAB) Remove() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = nil
}

// NZBGet imitates NZBGet's /jsonrpc with basic authentication.
type NZBGet struct {
	mu         sync.Mutex
	User, Pass string
	Categories []string
	Steps      int
	Result     Result
	Hold       bool
	Adds       []Add
	Actions    []string
	jobs       []*ngJob
	next       int
}

type ngJob struct {
	id      int
	name    string
	cat     string
	looks   int
	paused  bool
	history bool
	failed  bool
	msg     string
	path    string
	del     bool
}

type ngRequest struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func (n *NZBGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.URL.Path != "/jsonrpc" {
		http.NotFound(w, r)
		return
	}
	if u, p, ok := r.BasicAuth(); !ok || u != n.User || p != n.Pass {
		w.Header().Set("WWW-Authenticate", `Basic realm="NZBGet"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req ngRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	reply := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.1", "id": 1, "result": result})
	}
	str := func(i int) string {
		var v string
		if i < len(req.Params) {
			_ = json.Unmarshal(req.Params[i], &v)
		}
		return v
	}
	switch req.Method {
	case "version":
		reply("21.1")
	case "config":
		var conf []map[string]string
		for i, c := range n.Categories {
			conf = append(conf, map[string]string{"Name": fmt.Sprintf("Category%d.Name", i+1), "Value": c})
		}
		conf = append(conf, map[string]string{"Name": "MainDir", "Value": "/downloads"})
		reply(conf)
	case "append":
		a := Add{Name: strings.TrimSuffix(str(0), ".nzb"), Category: str(2)}
		content := str(1)
		if strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
			a.URL = content
		} else if b, err := base64.StdEncoding.DecodeString(content); err == nil {
			a.NZB = string(b)
		}
		var prio int
		if len(req.Params) > 3 {
			_ = json.Unmarshal(req.Params[3], &prio)
		}
		a.Priority = strconv.Itoa(prio)
		n.Adds = append(n.Adds, a)
		n.next++
		n.jobs = append(n.jobs, &ngJob{id: n.next, name: a.Name, cat: a.Category})
		reply(n.next)
	case "listgroups":
		var groups []map[string]any
		for _, j := range n.jobs {
			if j.history {
				continue
			}
			j.looks++
			if !n.Hold && !j.paused && j.looks > n.Steps+1 {
				j.history = true
				j.failed, j.msg, j.path = n.Result.Failed, n.Result.Message, n.Result.Path
				continue
			}
			status, left := "QUEUED", 100
			switch {
			case j.paused:
				status = "PAUSED"
			case j.looks > 1:
				status, left = "DOWNLOADING", 100-min(99, (j.looks-1)*100/(n.Steps+1))
			}
			groups = append(groups, map[string]any{"NZBID": j.id, "NZBName": j.name, "Status": status, "Category": j.cat, "FileSizeMB": 100, "RemainingSizeMB": left})
		}
		reply(groups)
	case "status":
		reply(map[string]any{"DownloadRate": 2097152})
	case "history":
		var items []map[string]any
		for _, j := range n.jobs {
			if !j.history {
				continue
			}
			status := "SUCCESS/ALL"
			if j.failed {
				status = j.msg // the stand-in takes the status word itself, e.g. FAILURE/HEALTH
			}
			items = append(items, map[string]any{"NZBID": j.id, "Name": j.name, "Status": status, "Category": j.cat, "FileSizeMB": 100, "DestDir": j.path, "FinalDir": j.path})
		}
		reply(items)
	case "editqueue":
		cmd, ids := str(0), []int{}
		if len(req.Params) > 2 {
			_ = json.Unmarshal(req.Params[2], &ids)
		}
		found := false
		for _, id := range ids {
			n.Actions = append(n.Actions, fmt.Sprintf("%s %d", cmd, id))
			for _, j := range n.jobs {
				if j.id != id {
					continue
				}
				switch {
				case cmd == "GroupPause" && !j.history:
					j.paused, found = true, true
				case cmd == "GroupResume" && !j.history:
					j.paused, found = false, true
				case (cmd == "GroupDelete" || cmd == "GroupFinalDelete") && !j.history:
					j.history, j.failed, j.msg = true, true, "DELETED/MANUAL"
					found = true
				case cmd == "HistoryFinalDelete" && j.history:
					j.del, found = true, true
				}
			}
		}
		kept := n.jobs[:0]
		for _, j := range n.jobs {
			if !j.del {
				kept = append(kept, j)
			}
		}
		n.jobs = kept
		reply(found)
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"name": "JSONRPCError", "message": "Invalid procedure"}})
	}
}

// Snapshot returns what was sent and which controls were pressed.
func (n *NZBGet) Snapshot() (adds []Add, actions []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Add(nil), n.Adds...), append([]string(nil), n.Actions...)
}
