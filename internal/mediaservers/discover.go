package mediaservers

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Finding media servers on the local network, two ways at once:
//
//   - Broadcast: Jellyfin and Emby answer "who is JellyfinServer?" / "who is
//     EmbyServer?" on UDP 7359, and Plex answers a GDM "M-SEARCH" on UDP
//     32414. Quick and exact, but only works when Mediarium shares the
//     network with them (not from Docker's default bridge network).
//   - Scan: try the usual ports (32400 for Plex, 8096 for Jellyfin and Emby)
//     on every address of a few private networks, then ask whatever answers
//     who it is, without signing in.
//
// Only private addresses are ever contacted, so this can't be pointed at the
// internet.

// Found is one media server that answered.
type Found struct {
	Kind         Kind   `json:"kind"`
	Name         string `json:"name"`
	Address      string `json:"address"` // base URL, e.g. http://192.168.1.10:32400
	Version      string `json:"version,omitempty"`
	ID           string `json:"id,omitempty"` // Plex machineIdentifier / Jellyfin or Emby server Id
	AlreadyAdded bool   `json:"alreadyAdded"`
	Via          string `json:"via"` // "broadcast" | "scan"
}

// Discovery is the outcome of one search.
type Discovery struct {
	Found   []Found  `json:"found"`
	Scanned []string `json:"scanned"` // the networks scanned, as CIDR
	Note    string   `json:"note,omitempty"`
}

// Probe is one port the scan tries on every address.
type Probe struct {
	Port   int
	Scheme string // http | https
	Plex   bool   // Plex (/identity) rather than Jellyfin/Emby (/System/Info/Public)
}

// DefaultProbes are Plex's and Jellyfin/Emby's standard http ports.
var DefaultProbes = []Probe{{Port: 32400, Scheme: "http", Plex: true}, {Port: 8096, Scheme: "http"}}

// CommonSubnets are home networks scanned besides Mediarium's own.
var CommonSubnets = []string{"192.168.0.0/24", "192.168.1.0/24", "10.0.0.0/24", "10.0.1.0/24", "172.16.0.0/24"}

// MaxSubnets is how many networks one search covers at most.
const MaxSubnets = 8

// Discoverer finds media servers. The zero value is ready to use; the fields
// exist so tests can point it at local fakes.
type Discoverer struct {
	Budget        time.Duration // whole search; default 10s
	BroadcastWait time.Duration // how long to listen for broadcast answers; default 2s, negative skips broadcasts
	DialTimeout   time.Duration // per address and port; default 600ms
	Workers       int           // addresses tried at once; default 256 (8 networks x 254 hosts x 2 ports fit the budget even when nothing answers)
	Probes        []Probe       // default DefaultProbes

	// Where the broadcasts go ("host:port"); empty means the standard
	// addresses plus every local network's broadcast address.
	EmbyTargets []string
	PlexTargets []string

	allow      func(net.IP) bool            // which addresses may be contacted; default PrivateIPv4
	interfaces func() ([]*net.IPNet, error) // Mediarium's own networks; default the host's interfaces
	dial       func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (d *Discoverer) budget() time.Duration {
	if d.Budget > 0 {
		return d.Budget
	}
	return 10 * time.Second
}

func (d *Discoverer) broadcastWait() time.Duration {
	if d.BroadcastWait != 0 {
		return d.BroadcastWait
	}
	return 2 * time.Second
}

func (d *Discoverer) dialTimeout() time.Duration {
	if d.DialTimeout > 0 {
		return d.DialTimeout
	}
	return 600 * time.Millisecond
}

func (d *Discoverer) workers() int {
	if d.Workers > 0 {
		return d.Workers
	}
	return 256
}

func (d *Discoverer) probes() []Probe {
	if len(d.Probes) > 0 {
		return d.Probes
	}
	return DefaultProbes
}

func (d *Discoverer) allowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if d.allow != nil {
		return d.allow(ip)
	}
	return PrivateIPv4(ip)
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

// PrivateIPv4 reports whether ip is an IPv4 address on a private network:
// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, or 100.64.0.0/10 (carrier-grade
// NAT, which Tailscale uses).
func PrivateIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return v4.IsPrivate() || cgnat.Contains(v4)
}

// ParseSubnets checks networks typed by the person: IPv4, private, a /24 or
// smaller, at most MaxSubnets. A plain address counts as that one address.
// Problems are UserErrors. Duplicates are dropped.
func (d *Discoverer) ParseSubnets(in []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	seen := map[string]bool{}
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		cidr := raw
		if !strings.Contains(cidr, "/") {
			cidr += "/32"
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil || n.IP.To4() == nil {
			return nil, userErr(err, "%q is not a network Mediarium understands. Use an IPv4 network like 192.168.1.0/24.", raw)
		}
		if ones, _ := n.Mask.Size(); ones < 24 {
			return nil, userErr(nil, "%s is too large to scan. Use a /24 (256 addresses) or smaller, like %s/24.", raw, n.IP.String())
		}
		if !d.allowed(n.IP) || !d.allowed(lastIP(n)) {
			return nil, userErr(nil, "%s is not a private network. Mediarium only looks for media servers on private networks (10.x.x.x, 172.16.x.x to 172.31.x.x, 192.168.x.x and 100.64.x.x to 100.127.x.x).", raw)
		}
		if seen[n.String()] {
			continue
		}
		seen[n.String()] = true
		out = append(out, n)
	}
	if len(out) > MaxSubnets {
		return nil, userErr(nil, "That is %d networks; search at most %d at a time.", len(out), MaxSubnets)
	}
	return out, nil
}

// DefaultSubnets is what a search without networks covers: Mediarium's own
// private networks (a larger one narrowed to the /24 around Mediarium's own
// address), then CommonSubnets, at most MaxSubnets in all.
func (d *Discoverer) DefaultSubnets() []*net.IPNet {
	var out []*net.IPNet
	seen := map[string]bool{}
	add := func(n *net.IPNet) {
		if len(out) < MaxSubnets && !seen[n.String()] {
			seen[n.String()] = true
			out = append(out, n)
		}
	}
	list := d.interfaces
	if list == nil {
		list = interfaceNets
	}
	own, err := list()
	if err != nil {
		slog.Debug("media server discovery: list network interfaces", "err", err)
	}
	for _, n := range own {
		ip := n.IP.To4()
		if ip == nil || !d.allowed(ip) {
			continue
		}
		ones, _ := n.Mask.Size()
		if ones < 24 {
			ones = 24
		}
		mask := net.CIDRMask(ones, 32)
		add(&net.IPNet{IP: ip.Mask(mask), Mask: mask})
	}
	for _, c := range CommonSubnets {
		if _, n, err := net.ParseCIDR(c); err == nil {
			add(n)
		}
	}
	return out
}

// interfaceNets lists the IPv4 networks of the host's interfaces that are up.
func interfaceNets() ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	var out []*net.IPNet
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				out = append(out, &net.IPNet{IP: n.IP.To4(), Mask: n.Mask})
			}
		}
	}
	return out, nil
}

func lastIP(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	out := make(net.IP, 4)
	for i := range out {
		out[i] = ip[i] | ^n.Mask[len(n.Mask)-4+i]
	}
	return out
}

// hosts lists the addresses of n worth trying: all but the network and
// broadcast addresses (all of them for a /31 or /32).
func hosts(n *net.IPNet) []net.IP {
	ones, _ := n.Mask.Size()
	start := binary.BigEndian.Uint32(n.IP.To4())
	end := binary.BigEndian.Uint32(lastIP(n))
	if ones <= 30 {
		start, end = start+1, end-1
	}
	var out []net.IP
	for v := start; v >= start && v <= end; v++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, v)
		out = append(out, ip)
		if v == end {
			break
		}
	}
	return out
}

// Discover searches the given networks (DefaultSubnets when empty) and
// listens for broadcast answers at the same time. It never fails: whatever
// was found within the time budget is returned.
func (d *Discoverer) Discover(ctx context.Context, subnets []*net.IPNet) Discovery {
	if len(subnets) == 0 {
		subnets = d.DefaultSubnets()
	}
	ctx, cancel := context.WithTimeout(ctx, d.budget())
	defer cancel()

	var bcast []Found
	var wg sync.WaitGroup
	if d.broadcastWait() > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bcast = d.broadcast(ctx)
		}()
	}
	scanned, unchecked := d.scan(ctx, subnets)
	wg.Wait()

	res := Discovery{Found: mergeFound(append(bcast, scanned...)), Scanned: []string{}}
	for _, n := range subnets {
		res.Scanned = append(res.Scanned, n.String())
	}
	switch {
	case unchecked > 0:
		res.Note = fmt.Sprintf("The search stopped after %d seconds with %d addresses not checked. Search fewer networks to check them all.", int(d.budget().Seconds()), unchecked)
	case len(res.Found) == 0:
		res.Note = "Nothing answered. If Mediarium runs in Docker on the default bridge network, broadcasts don't reach your home network, so only the networks listed were scanned. Type your home network (for example 192.168.1.0/24) and search again, or add the server by its address."
	}
	return res
}

// mergeFound drops duplicates (same server id, or same address), keeping
// the first and filling its blanks from the others, and sorts by name.
func mergeFound(in []Found) []Found {
	out := []Found{}
	index := map[string]int{}
	for _, f := range in {
		keys := []string{"addr:" + addressKey(f.Address)}
		if f.ID != "" {
			keys = append(keys, "id:"+string(f.Kind)+":"+f.ID)
		}
		at := -1
		for _, k := range keys {
			if i, ok := index[k]; ok {
				at = i
				break
			}
		}
		if at < 0 {
			at = len(out)
			out = append(out, f)
		} else {
			e := &out[at]
			if e.Name == "" || strings.HasPrefix(e.Name, e.Kind.Label()+" at ") {
				if f.Name != "" {
					e.Name = f.Name
				}
			}
			if e.Version == "" {
				e.Version = f.Version
			}
			if e.ID == "" {
				e.ID = f.ID
			}
		}
		for _, k := range keys {
			index[k] = at
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !strings.EqualFold(out[i].Name, out[j].Name) {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].Address < out[j].Address
	})
	return out
}

// addressKey reduces a base URL to host:port for comparing addresses.
func addressKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		if !strings.Contains(raw, "://") {
			if u, err = url.Parse("http://" + strings.TrimSpace(raw)); err != nil || u.Host == "" {
				return strings.ToLower(strings.TrimSpace(raw))
			}
		} else {
			return strings.ToLower(strings.TrimSpace(raw))
		}
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return strings.ToLower(net.JoinHostPort(u.Hostname(), port))
}

// MarkAdded sets AlreadyAdded on every found server that is already saved:
// the same server id, or the same address.
func MarkAdded(found []Found, saved []Server) {
	for i := range found {
		_, found[i].AlreadyAdded = FindSaved(saved, "", found[i].ID, found[i].Address)
	}
}

// FindSaved finds the saved server that is the same as the one described:
// the same server id, or the same address (scheme and trailing slash aside).
// A non-empty kind must match too.
func FindSaved(saved []Server, kind Kind, serverID, address string) (Server, bool) {
	for _, s := range saved {
		if kind != "" && s.Kind != kind {
			continue
		}
		if (serverID != "" && s.ServerID == serverID) || (address != "" && addressKey(s.BaseURL) == addressKey(address)) {
			return s, true
		}
	}
	return Server{}, false
}

// probeClient is the HTTP client for asking a found server who it is: short
// timeouts, no redirects, and it refuses to connect anywhere not allowed.
func (d *Discoverer) probeClient() *http.Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, fmt.Errorf("probe address %q: %w", addr, err)
				}
				if !d.allowed(net.ParseIP(host)) {
					return nil, fmt.Errorf("probe address %s: not a private network address", host)
				}
				return dialer.DialContext(ctx, network, addr)
			},
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: 2 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// getInto fetches base+path and decodes it (JSON, or XML for Plex).
func getInto(ctx context.Context, hc *http.Client, kind Kind, base, path string, out any) error {
	req, err := newRequest(ctx, kind, http.MethodGet, base, path, nil, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mediarium")
	if kind == KindPlex {
		req.Header.Set("X-Plex-Product", "Mediarium")
	}
	c := &Client{HTTP: hc}
	return c.exchange(req, Server{Kind: kind}, base, out)
}

// identify asks base who it is, without signing in: Plex's /identity, or
// Jellyfin/Emby's /System/Info/Public. ok is false when it isn't a media server.
func (d *Discoverer) identify(ctx context.Context, hc *http.Client, base string, plex bool) (Found, bool) {
	if plex {
		var doc plexDoc
		if err := getInto(ctx, hc, KindPlex, base, "/identity", &doc); err != nil {
			return Found{}, false
		}
		id := doc.container()
		if id.MachineIdentifier == "" {
			return Found{}, false
		}
		return Found{Kind: KindPlex, Address: base, ID: id.MachineIdentifier, Version: id.Version}, true
	}
	var info embyPublicInfo
	if err := getInto(ctx, hc, KindJellyfin, base, "/System/Info/Public", &info); err != nil {
		return Found{}, false
	}
	if info.ID == "" || info.Version == "" {
		return Found{}, false
	}
	return Found{Kind: info.kind(), Name: info.ServerName, Address: base, ID: info.ID, Version: info.Version}, true
}

type scanJob struct {
	ip    net.IP
	probe Probe
}

// scan tries every probe on every host of subnets. unchecked counts the
// address/port pairs the time budget left untried.
func (d *Discoverer) scan(ctx context.Context, subnets []*net.IPNet) (found []Found, unchecked int) {
	dial := d.dial
	if dial == nil {
		dialer := &net.Dialer{Timeout: d.dialTimeout()}
		dial = dialer.DialContext
	}
	hc := d.probeClient()
	jobs := make(chan scanJob)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < d.workers(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					mu.Lock()
					unchecked++
					mu.Unlock()
					continue
				}
				addr := net.JoinHostPort(j.ip.String(), strconv.Itoa(j.probe.Port))
				dctx, cancel := context.WithTimeout(ctx, d.dialTimeout())
				conn, err := dial(dctx, "tcp", addr)
				cancel()
				if err != nil {
					continue
				}
				_ = conn.Close()
				f, ok := d.identify(ctx, hc, j.probe.Scheme+"://"+addr, j.probe.Plex)
				if !ok {
					continue
				}
				if f.Name == "" {
					f.Name = f.Kind.Label() + " at " + j.ip.String()
				}
				f.Via = "scan"
				mu.Lock()
				found = append(found, f)
				mu.Unlock()
			}
		}()
	}
	total, sent := 0, 0
	for _, n := range subnets {
		total += len(hosts(n)) * len(d.probes())
	}
feed:
	for _, n := range subnets {
		for _, ip := range hosts(n) {
			if !d.allowed(ip) {
				continue // never happens for checked subnets; belt and braces
			}
			for _, p := range d.probes() {
				select {
				case jobs <- scanJob{ip: ip, probe: p}:
					sent++
				case <-ctx.Done():
					break feed
				}
			}
		}
	}
	close(jobs)
	wg.Wait()
	return found, unchecked + (total - sent)
}

// broadcast sends the discovery messages and collects answers until
// BroadcastWait is up.
func (d *Discoverer) broadcast(parent context.Context) []Found {
	ctx, cancel := context.WithTimeout(parent, d.broadcastWait())
	defer cancel()
	embyTargets, plexTargets := d.EmbyTargets, d.PlexTargets
	if len(embyTargets) == 0 || len(plexTargets) == 0 {
		bcasts := []string{"255.255.255.255"}
		if own, err := interfaceNets(); err == nil {
			for _, n := range own {
				if d.allowed(n.IP) {
					bcasts = append(bcasts, lastIP(n).String())
				}
			}
		}
		if len(embyTargets) == 0 {
			for _, b := range bcasts {
				embyTargets = append(embyTargets, net.JoinHostPort(b, "7359"))
			}
		}
		if len(plexTargets) == 0 {
			plexTargets = []string{"239.0.0.250:32414"}
			for _, b := range bcasts {
				plexTargets = append(plexTargets, net.JoinHostPort(b, "32414"))
			}
		}
	}
	type reply struct {
		kind Kind
		src  net.IP
		data []byte
	}
	var mu sync.Mutex
	var replies []reply
	var wg sync.WaitGroup
	ask := func(kind Kind, msg string, targets []string) {
		defer wg.Done()
		conn, err := net.ListenPacket("udp4", ":0")
		if err != nil {
			slog.Debug("media server discovery: open UDP socket", "type", kind.Label(), "err", err)
			return
		}
		defer conn.Close()
		sent := 0
		for _, t := range dedupe(targets) {
			addr, err := net.ResolveUDPAddr("udp4", t)
			if err != nil {
				continue
			}
			if _, err := conn.WriteTo([]byte(msg), addr); err != nil {
				slog.Debug("media server discovery: send broadcast", "type", kind.Label(), "to", t, "err", err)
				continue
			}
			sent++
		}
		if sent == 0 {
			return
		}
		deadline, _ := ctx.Deadline()
		_ = conn.SetReadDeadline(deadline)
		buf := make([]byte, 8192)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return // deadline reached
			}
			ua, ok := from.(*net.UDPAddr)
			if !ok || !d.allowed(ua.IP) {
				continue
			}
			mu.Lock()
			replies = append(replies, reply{kind: kind, src: ua.IP, data: append([]byte(nil), buf[:n]...)})
			mu.Unlock()
		}
	}
	wg.Add(3)
	go ask(KindJellyfin, "who is JellyfinServer?", embyTargets)
	go ask(KindEmby, "who is EmbyServer?", embyTargets)
	go ask(KindPlex, "M-SEARCH * HTTP/1.1\r\n\r\n", plexTargets)
	wg.Wait()

	// Confirm each answer over HTTP (for the version, and to tell Jellyfin
	// from Emby for sure), with what is left of the overall budget.
	hc := d.probeClient()
	cctx, ccancel := context.WithTimeout(parent, 3*time.Second)
	defer ccancel()
	var out []Found
	var owg sync.WaitGroup
	for _, r := range replies {
		var f Found
		var ok bool
		if r.kind == KindPlex {
			f, ok = parseGDM(r.data, r.src)
		} else {
			f, ok = parseEmbyReply(r.data, r.src, r.kind, d.allowed)
		}
		if !ok {
			continue
		}
		f.Via = "broadcast"
		owg.Add(1)
		go func(f Found, src net.IP) {
			defer owg.Done()
			bases := []string{f.Address}
			if u, err := url.Parse(f.Address); err == nil && u.Hostname() != src.String() {
				port := u.Port()
				if port == "" {
					port = "8096"
				}
				bases = append(bases, "http://"+net.JoinHostPort(src.String(), port))
			}
			for _, b := range bases {
				if got, ok := d.identify(cctx, hc, b, f.Kind == KindPlex); ok {
					f.Address, f.Version = b, got.Version
					if f.Kind != KindPlex {
						f.Kind = got.Kind
					}
					if got.ID != "" {
						f.ID = got.ID
					}
					if got.Name != "" {
						f.Name = got.Name
					}
					break
				}
			}
			mu.Lock()
			out = append(out, f)
			mu.Unlock()
		}(f, r.src)
	}
	owg.Wait()
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// parseEmbyReply reads a Jellyfin/Emby discovery answer:
// {"Address":"http://192.168.1.5:8096","Id":"...","Name":"..."}. An address
// that isn't an allowed IP is replaced by the IP the answer came from, so
// nothing but the local network is ever contacted.
func parseEmbyReply(data []byte, src net.IP, kind Kind, allowed func(net.IP) bool) (Found, bool) {
	var r struct {
		Address string `json:"Address"`
		ID      string `json:"Id"`
		Name    string `json:"Name"`
	}
	if err := json.Unmarshal(data, &r); err != nil || r.ID == "" {
		return Found{}, false
	}
	port := "8096"
	scheme := "http"
	host := ""
	if u, err := url.Parse(strings.TrimSpace(r.Address)); err == nil && u.Host != "" {
		host = u.Hostname()
		if u.Port() != "" {
			port = u.Port()
		}
		if u.Scheme == "https" {
			scheme = "https"
		}
	}
	if ip := net.ParseIP(host); ip == nil || !allowed(ip) {
		host = src.String()
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = kind.Label() + " at " + host
	}
	return Found{Kind: kind, Name: name, Address: scheme + "://" + net.JoinHostPort(host, port), ID: r.ID}, true
}

// parseGDM reads a Plex GDM answer: HTTP-style header lines, among them
// Name, Port, Resource-Identifier and Version. The address is the answer's
// source IP.
func parseGDM(data []byte, src net.IP) (Found, bool) {
	sc := bufio.NewScanner(io.LimitReader(strings.NewReader(string(data)), 8192))
	var first = true
	f := Found{Kind: KindPlex}
	port := "32400"
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			if !strings.Contains(line, "200") {
				return Found{}, false
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "name":
			f.Name = v
		case "port":
			if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
				port = v
			}
		case "resource-identifier":
			f.ID = v
		case "version":
			f.Version = v
		case "content-type":
			if !strings.Contains(v, "plex/media-server") {
				return Found{}, false
			}
		}
	}
	if f.ID == "" {
		return Found{}, false
	}
	if f.Name == "" {
		f.Name = "Plex at " + src.String()
	}
	f.Address = "http://" + net.JoinHostPort(src.String(), port)
	return f, true
}
