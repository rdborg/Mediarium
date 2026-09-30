package migrate

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// NZBGet is read through its JSON-RPC API (POST /jsonrpc, basic auth), and
// only with the read-only methods in nzbgetReadOnly. Its "config" method
// lists every option as {Name, Value}; news servers are ServerN.* options.

type nzbgetOption struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

type nzbgetData struct {
	Version string
	Options []nzbgetOption
}

func fetchNZBGet(ctx context.Context, hc *http.Client, c LoginConn) (nzbgetData, error) {
	var d nzbgetData
	if err := rpc(ctx, hc, c, "version", &d.Version); err != nil {
		return d, err
	}
	if err := rpc(ctx, hc, c, "config", &d.Options); err != nil {
		return d, fmt.Errorf("read settings: %w", err)
	}
	return d, nil
}

var nzbgetServerOption = regexp.MustCompile(`^(?i)server(\d+)\.(\w+)$`)
var nzbgetCategoryName = regexp.MustCompile(`^(?i)category\d+\.name$`)

func nzbgetYes(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "1", "on":
		return true
	}
	return false
}

// nzbgetServers turns NZBGet's option list into news servers (in NZBGet's
// order), its destination folder and its category names.
func nzbgetServers(opts []nzbgetOption) (servers []newsServer, destDir string, categories []string) {
	byNum := map[int]map[string]string{}
	for _, o := range opts {
		if m := nzbgetServerOption.FindStringSubmatch(o.Name); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			if byNum[n] == nil {
				byNum[n] = map[string]string{}
			}
			byNum[n][strings.ToLower(m[2])] = o.Value
			continue
		}
		switch {
		case strings.EqualFold(o.Name, "DestDir"):
			destDir = o.Value
		case nzbgetCategoryName.MatchString(o.Name) && strings.TrimSpace(o.Value) != "":
			categories = append(categories, strings.TrimSpace(o.Value))
		}
	}
	nums := make([]int, 0, len(byNum))
	for n := range byNum {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		o := byNum[n]
		port, _ := strconv.Atoi(strings.TrimSpace(o["port"]))
		conns, _ := strconv.Atoi(strings.TrimSpace(o["connections"]))
		level, _ := strconv.Atoi(strings.TrimSpace(o["level"]))
		active := true // NZBGet treats a missing Active as yes
		if v, ok := o["active"]; ok {
			active = nzbgetYes(v)
		}
		servers = append(servers, newsServer{
			Name: strings.TrimSpace(o["name"]), Host: strings.TrimSpace(o["host"]), Port: port,
			SSL: nzbgetYes(o["encryption"]), Username: o["username"], Password: o["password"],
			Connections: conns, Priority: level, Optional: nzbgetYes(o["optional"]), Enabled: active,
		})
	}
	return servers, destDir, categories
}

func planNZBGet(p *plan, sp *ServersPreview, d *nzbgetData, seen map[serverKey]bool) {
	servers, dest, cats := nzbgetServers(d.Options)
	sp.CompleteDir, sp.Categories = dest, cats
	for _, s := range servers {
		planServer(p, sp, "NZBGet", s, seen)
	}
}
