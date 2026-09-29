// Package vpn implements the built-in VPN protection: userspace WireGuard
// embedded directly in the process via
// wireguard-go + a gVisor netstack, so tunneling torrent traffic needs no
// NET_ADMIN capability or /dev/net/tun device — the same technique
// projects like Tailscale use. The "provider picker" the UI shows is a
// friendly wrapper over this one generic mechanism: every provider
// integration ultimately reduces to feeding this a WireGuard config, and
// the generic "paste your own config" option (always available)
// exercises the exact same path as a named provider.
package vpn

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// Config is a generic WireGuard peer config — deliberately provider-
// agnostic. A "provider picker" in Settings is just a UI convenience for
// producing one of these; the tunnel itself never special-
// cases a provider.
type Config struct {
	PrivateKey          string // base64, as found in a standard WireGuard config file
	PeerPublicKey       string // base64
	PresharedKey        string // base64, optional
	Endpoint            string // host:port
	AllowedIPs          []string
	LocalAddresses      []string // this device's tunnel IP(s), e.g. "10.2.0.2/32"
	DNS                 []string
	PersistentKeepalive int // seconds, 0 = disabled
	MTU                 int // 0 = device.DefaultMTU
	ListenPort          int // 0 = let the OS pick (queryable afterward via Tunnel.ListenPort)
}

// Tunnel is a live WireGuard connection. Callers dial out through it via
// DialContext, which is all that's needed to route torrent traffic (or,
// optionally, Usenet/general app traffic) through the tunnel
// instead of the host network directly.
type Tunnel struct {
	dev  *device.Device
	tnet *netstack.Net
}

// Up brings a tunnel to the configured peer. It does not verify the
// handshake completes — that happens asynchronously, same as any
// WireGuard client; callers should treat a Dial failing/timing out as the
// signal the tunnel isn't actually passing traffic (this is also what the
// kill switch in the download engine checks).
func Up(cfg Config) (*Tunnel, error) {
	localAddrs, err := parseAddrs(cfg.LocalAddresses)
	if err != nil {
		return nil, fmt.Errorf("parse local addresses: %w", err)
	}
	dnsAddrs, err := parseAddrs(cfg.DNS)
	if err != nil {
		return nil, fmt.Errorf("parse dns addresses: %w", err)
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = device.DefaultMTU
	}

	tunDev, tnet, err := netstack.CreateNetTUN(localAddrs, dnsAddrs, mtu)
	if err != nil {
		return nil, fmt.Errorf("create netstack tun: %w", err)
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, "vpn: "))

	uapiConf, err := cfg.uapiConfig()
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("build wireguard config: %w", err)
	}
	if err := dev.IpcSet(uapiConf); err != nil {
		dev.Close()
		return nil, fmt.Errorf("apply wireguard config: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("bring wireguard device up: %w", err)
	}

	return &Tunnel{dev: dev, tnet: tnet}, nil
}

// DialContext dials addr through the tunnel — use this as the dialer for
// anything that should go over the VPN (torrent traffic by
// default, Usenet/general traffic optionally).
func (t *Tunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return t.tnet.DialContext(ctx, network, addr)
}

// ListenTCP listens on addr within the tunnel's own virtual network stack
// (mainly useful for tests that need a reachable-through-the-tunnel
// endpoint; production use of this package is all outbound Dial calls).
func (t *Tunnel) ListenTCP(addr string) (net.Listener, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", addr, err)
	}
	return t.tnet.ListenTCP(tcpAddr)
}

// ListenPort returns the UDP port this tunnel's WireGuard endpoint is
// actually bound to — useful when Config.ListenPort was 0 (OS-assigned)
// and a peer needs to be told where to reach this instance.
func (t *Tunnel) ListenPort() (int, error) {
	raw, err := t.dev.IpcGet()
	if err != nil {
		return 0, fmt.Errorf("ipc get: %w", err)
	}
	for _, line := range strings.Split(raw, "\n") {
		if port, ok := strings.CutPrefix(line, "listen_port="); ok {
			var n int
			if _, err := fmt.Sscanf(port, "%d", &n); err != nil {
				return 0, fmt.Errorf("parse listen_port %q: %w", port, err)
			}
			return n, nil
		}
	}
	return 0, fmt.Errorf("listen_port not found in device state")
}

// PublicIP asks a third-party IP-echo service, dialed through the tunnel,
// what address it's seeing — the standard way any VPN app confirms traffic
// is actually egressing via the provider rather than leaking
// ("status visibility"). This is an on-demand outbound request the user
// explicitly triggers by viewing VPN status, not a background poll.
func (t *Tunnel) PublicIP(ctx context.Context) (string, error) {
	return t.publicIPFrom(ctx, "https://api.ipify.org")
}

// publicIPFrom is PublicIP with the echo-service URL as a parameter, purely
// so tests can point it at a local fixture server through a real tunnel
// instead of making a live call to a third party (tests use local
// fixtures over live network calls).
func (t *Tunnel) publicIPFrom(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Transport: &http.Transport{DialContext: t.DialContext},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request through tunnel: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ip echo service returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("unexpected response from ip echo service: %q", ip)
	}
	return ip, nil
}

// Close tears down the tunnel. Any in-flight Dial-ed connections through
// it will start failing — the natural kill-switch behavior ("if the
// tunnel drops, torrent traffic must pause immediately") as long
// as callers treat a dial/read/write error on a tunneled connection as
// fatal for that transfer rather than silently falling back to a direct
// connection.
func (t *Tunnel) Close() {
	t.dev.Close()
}

func parseAddrs(addrs []string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		// Local addresses are commonly written with a /32 or /128 suffix
		// (as in a wg-quick [Interface] Address= line); netip.ParseAddr
		// doesn't accept that, so strip it.
		if idx := strings.IndexByte(a, '/'); idx >= 0 {
			a = a[:idx]
		}
		addr, err := netip.ParseAddr(a)
		if err != nil {
			return nil, fmt.Errorf("parse address %q: %w", a, err)
		}
		out = append(out, addr)
	}
	return out, nil
}

// uapiConfig renders cfg into the WireGuard UAPI config-protocol format
// IpcSet expects (https://www.wireguard.com/xplatform/) — keys are hex,
// not the base64 used in on-disk .conf files, so user-pasted base64 keys
// are converted here.
func (c Config) uapiConfig() (string, error) {
	privHex, err := base64KeyToHex(c.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("private key: %w", err)
	}
	pubHex, err := base64KeyToHex(c.PeerPublicKey)
	if err != nil {
		return "", fmt.Errorf("peer public key: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", privHex)
	if c.ListenPort != 0 {
		fmt.Fprintf(&b, "listen_port=%d\n", c.ListenPort)
	}
	fmt.Fprintf(&b, "public_key=%s\n", pubHex)
	if c.Endpoint != "" {
		// Omitted when unknown — WireGuard learns a peer's endpoint
		// dynamically from its first valid handshake (peer "roaming"),
		// so a responder doesn't need to know an initiator's address
		// ahead of time.
		fmt.Fprintf(&b, "endpoint=%s\n", c.Endpoint)
	}
	if c.PresharedKey != "" {
		pskHex, err := base64KeyToHex(c.PresharedKey)
		if err != nil {
			return "", fmt.Errorf("preshared key: %w", err)
		}
		fmt.Fprintf(&b, "preshared_key=%s\n", pskHex)
	}
	if c.PersistentKeepalive > 0 {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", c.PersistentKeepalive)
	}
	allowedIPs := c.AllowedIPs
	if len(allowedIPs) == 0 {
		allowedIPs = []string{"0.0.0.0/0", "::/0"}
	}
	for _, ip := range allowedIPs {
		fmt.Fprintf(&b, "allowed_ip=%s\n", ip)
	}
	return b.String(), nil
}

func base64KeyToHex(key string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("expected a 32-byte key, got %d bytes", len(raw))
	}
	return hex.EncodeToString(raw), nil
}
