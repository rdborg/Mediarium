# VPN protection

Mediarium has a WireGuard VPN client built in, for torrent traffic and, if you choose, indexer traffic. It runs inside the app, so the container needs no extra privileges: no `NET_ADMIN` and no `/dev/net/tun`.

Find it under **Settings > Downloading > VPN and proxy**. It's optional: nothing goes through a VPN until you add a connection and activate it. From then on, all torrent traffic goes through it while it's connected, and indexer traffic too if you switch that on.

## What goes through the VPN

- **Torrent traffic** (talking to peers, to HTTP trackers, to web seeds and fetching the details a magnet link points to), whenever a VPN is connected. UDP trackers (`udp://`) and DHT stay off unless you switch on **UDP trackers and DHT through the VPN**. They then leave through the tunnel, so peers and trackers see the VPN address, and their names are looked up through the VPN's DNS. If that lookup fails, nothing is sent from your home address. uTP stays off. See [UDP trackers and DHT](#udp-trackers-and-dht).
- **Indexers, if you choose.** Switch on **Send indexer traffic through the VPN** and searches, RSS, release downloads, IRC announces and debrid go through it too. See [Indexers through the VPN](#indexers-through-the-vpn).
- **Nothing else.** TMDB, subtitles, media servers and your Usenet provider all connect directly. Usenet downloads are encrypted between you and your provider, and do not show your address to other people the way a torrent swarm does. To send Usenet somewhere else too, use a [proxy](#a-socks5-or-http-proxy-instead).

## Add a connection

1. Pick your provider in **Add a VPN connection**. The list is in alphabetical order, from AirVPN to Windscribe, with **Your own WireGuard server** and **Not listed / any other provider** at the end. Choosing a provider shows short steps for getting its WireGuard file, a link to the provider's own page, and a warning if that provider has something that trips people up. It also fills in the DNS servers for the few providers where every customer gets the same ones (Mullvad and Surfshark). It never touches your keys. Every provider ends up with the same fields, so if yours isn't on the list, pick **Not listed** and carry on from step 3. The [provider list](#providers-and-where-to-get-the-file) below says what each one needs.
2. Get your WireGuard details from your provider, using the steps and link on the page. Mediarium never asks for your VPN account login. You copy the values from your provider's own page.
3. The quickest way: paste your whole WireGuard `.conf` file into **Paste your .conf file**, or press **Or choose the file**. Every field below is filled in from it, including the preshared key, DNS servers and allowed IPs. Only the first `[Peer]` is used. Or fill in the form by hand:

| Field | What to enter |
|---|---|
| Label | A name for you, for example "My VPN". |
| Endpoint | The server's address and port, like `vpn.example.com:51820`. A server name or an IP address both work. A name is looked up each time the connection starts, the way `wg-quick` does it, and the name itself is what's kept. |
| Private key | Your WireGuard private key (44 characters). |
| Peer public key | The server's public key (44 characters). |
| Preshared key | Optional. Only if your config has a `PresharedKey` line (Windscribe's do). Without it the tunnel never connects. |
| Local tunnel address | The address your provider gives you, like `10.2.0.2/32`. Several can be separated by commas. |
| DNS servers | Optional. The `DNS` line of your config, like `10.2.0.1`. |
| Allowed IPs | Optional. Leave empty to send everything through the tunnel (`0.0.0.0/0, ::/0`). |

4. Press **Add connection**, then **Activate** on its row. The status at the top says **Connected** followed by the connection's label in brackets, once the VPN server has answered. **Disconnect** turns the tunnel off again.

You can store several connections; one is active at a time. Private and preshared keys are stored encrypted (with the key in `secret.key`) and are never shown again or written to the log.

## Providers and where to get the file

This list is in alphabetical order and is not a recommendation: it only says where each provider keeps its WireGuard file and what to watch for. It was checked against the providers' own help pages in October 2026. Anything a provider's pages don't say is left out rather than guessed. Providers change their sites, so if a menu name has moved, the link on the page still gets you to the right place.
"Forwarded ports" means a port the provider opens for incoming connections. Mediarium doesn't use one: it only connects out to other peers, and nothing listens through the tunnel. So you never need to turn it on.

| Provider | How to get the file | Good to know | Forwarded ports at the provider | Sources |
|---|---|---|---|---|
| AirVPN | Client Area, then **Config Generator**. Pick WireGuard and a server. | AirVPN makes the keys. Renewing your keys gives a new address, so download the file again. Up to 5 connections at once, and each device needs its own key. | Yes, up to 5 ports from 2048 up, set in the Client Area. | [WireGuard FAQ](https://airvpn.org/faq/wireguard/), [connections FAQ](https://airvpn.org/faq/multiple_connections/), [port forwarding FAQ](https://airvpn.org/faq/port_forwarding/) |
| AzireVPN | [manager.azirevpn.com/wireguard](https://manager.azirevpn.com/wireguard), the WireGuard configuration generator. | 10 devices, 5 active at once. One address holds up to 3 public keys. | Yes, per device in the manager. | [Blog](https://blog.azirevpn.com/new-location-osaka-japan/), [FAQ](https://www.azirevpn.com/support/faq), [API docs](https://www.azirevpn.com/docs/api/ips), [port forwarding](https://blog.azirevpn.com/port-forwarding/) |
| hide.me | Members area, then **WireGuard** in the right-hand menu, **Generate keys**, pick a location, **Download WireGuard config file**. | Premium accounts only. Up to 10 static keys. One file per location. | Only "dynamic" port forwarding. Whether it works with a downloaded file isn't stated. | [Help article](https://hide.me/en/help/how-to-connect-via-wireguard-configuration-file/), [Premium only](https://hide.me/en/help/does-hide-me-work-with-any-wireguard-client/), [announcement](https://hide.me/en/blog/wireguard-static-configuration-now-available-to-premium-users/), [port forwarding](https://hide.me/en/help/how-do-i-activate-dynamic-port-forwarding/) |
| IPVanish | [my.ipvanish.com](https://my.ipvanish.com/), then **WireGuard** in the left menu. Pick a location and press **Generate**. | The preshared key is left blank, so there is none. WireGuard uses UDP port 51820. | No. | [Setup guide](https://support.ipvanish.com/hc/en-us/articles/32069446435099-WireGuard-Linux-command-line-setup), [ports](https://support.ipvanish.com/hc/en-us/articles/115002074594-Ports-used-for-VPN-Protocols), [port forwarding](https://support.ipvanish.com/hc/en-us/articles/115002080173-Port-Forwarding) |
| IVPN | Client Area, then the [WireGuard config generator](https://www.ivpn.net/account/wireguard-config). **Generate key** (or add your own), pick Single or Multi-hop and a server, download the zip. | 5 devices on Standard and Plus, 10 on Pro. The usual DNS server is 172.16.0.1. The tunnel address has an IPv6 part too. | No, ended on 30 September 2023. | [WireGuard FAQ](https://www.ivpn.net/knowledgebase/general/wireguard-faq/), [Linux guide](https://www.ivpn.net/setup/linux-wireguard/), [Windows guide](https://www.ivpn.net/setup/windows-10-wireguard/), [port forwarding](https://www.ivpn.net/blog/gradual-removal-of-port-forwarding) |
| Mullvad | Log in at [mullvad.net/en/account/wireguard-config](https://mullvad.net/en/account/wireguard-config). Pick a platform, **Generate key**, pick a server, **Download file**. | Up to 5 devices, one key each. DNS is 10.64.0.1 in Mullvad's own script. The lists in the file have no spaces after the commas, and AllowedIPs is written `::0/0`; both work. | No, removed in May 2023. | [Windows guide](https://mullvad.net/en/help/wireguard-app-windows), [FAQ](https://mullvad.net/en/help/faq), [script](https://github.com/mullvad/mullvad-wg.sh), [port forwarding](https://mullvad.net/en/blog/2023/5/29/removing-the-support-for-forwarded-ports) |
| NordVPN | Nord Account, then **NordVPN**, **Advanced Settings**, **Set up NordVPN manually**. Pick a server, **Get setup configuration**, choose WireGuard, **Download config**. | Needs your service credentials, not your Nord Account password. We couldn't see what the file contains: if it has no `PrivateKey` line, Mediarium can't use it. NordVPN's pages don't describe any other way to get the key, so there is no preset for one. 10 devices. | Not on regular servers. | [Download steps](https://support.nordvpn.com/hc/en-us/articles/19617516982929), [Linux guide](https://support.nordvpn.com/hc/en-us/articles/51499345558289), [devices](https://support.nordvpn.com/hc/en-us/articles/19476515228305), [port forwarding](https://support.nordvpn.com/hc/en-us/articles/19483392309649) |
| OVPN | Sign in, then the WireGuard configuration generator. It makes a key pair in your browser or takes your own public key. | 4 devices at once, plus one per year of subscription. A public key can only be connected once at a time. With your own public key the file has a placeholder where the private key goes. We couldn't see the exact menu name behind the login. | Yes, 49152 to 65535, up to 7 per account. | [Generator help](https://support.ovpn.com/hc/en-us/articles/46236349941523-How-do-I-enter-a-public-key-in-the-configuration-generator), [sample file](https://support.ovpn.com/hc/en-us/articles/46236349856659-I-am-connected-to-Wireguard-but-I-do-not-have-an-internet-connection-Is-there-anything-I-can-do-troubleshoot-this), [connections](https://support.ovpn.com/hc/en-us/articles/46236227982483-What-is-the-number-of-allowed-connections), [ports](https://support.ovpn.com/hc/en-us/articles/46236305597075-Opening-Ports) |
| Private Internet Access | No download page. PIA's own [manual connection scripts](https://github.com/pia-foss/manual-connections) write the file on your computer. | Run `run_setup.sh` with `VPN_PROTOCOL=wireguard`, `PIA_CONNECT=false`, `PIA_USER`, `PIA_PASS`, `PREFERRED_REGION` and `PIA_CONF_PATH`. It needs `curl`, `jq` and `wireguard-tools`. The address comes from PIA each time you run it and is written without a `/32`, which works. PIA's files are IPv4 only. If the connection stops working, run the script again. | Yes, through the same scripts. | [Repository](https://github.com/pia-foss/manual-connections), [WireGuard script](https://raw.githubusercontent.com/pia-foss/manual-connections/master/connect_to_wireguard_with_token.sh), [port forwarding script](https://raw.githubusercontent.com/pia-foss/manual-connections/master/port_forwarding.sh) |
| ProtonVPN | account.protonvpn.com, then **Downloads**, **WireGuard configuration**. Name it, pick a platform, options and a server, **Create**, then **Download**. | The free plan can create one too. Proton's own pages recommend its apps over manual files. | Yes on paid plans, with the NAT-PMP option on a P2P server. | [WireGuard configurations](https://protonvpn.com/support/wireguard-configurations), [port forwarding](https://protonvpn.com/support/port-forwarding-manual-setup) |
| StrongVPN | [wg.strongvpn.com](https://wg.strongvpn.com), signed in with your account email and password. Pick a system and a location, **Generate**. | One file per device. The 13th file deletes the oldest. Its FAQ is old and still says beta. | No. | [WireGuard FAQ](https://support.strongvpn.com/hc/en-us/articles/360035488393-WireGuard-FAQ), [port forwarding](https://support.strongvpn.com/hc/en-us/articles/360045757953-Does-Port-Forwarding-Work-with-StrongVPN) |
| Surfshark | **VPN**, **Manual setup**, **Desktop or mobile**, **WireGuard**. **I don't have a key pair**, **Generate a new key pair**, then **Choose a location** and download. | Make the key pair before you download, or the file has no keys. The private key is only shown once. DNS servers are 162.252.172.57 and 149.154.159.92. The server is a host name, which Mediarium looks up each time it connects. | No. | [Manual setup](https://support.surfshark.com/hc/en-us/articles/19262406140306), [Raspberry Pi guide](https://support.surfshark.com/hc/en-us/articles/32395023041810-How-to-set-up-WireGuard-on-Raspberry-Pi), [port forwarding](https://surfshark.com/blog/vpn-port-forwarding) |
| TorGuard | [Config Generator](https://www.torguard.net/config-generator) (sign in). Tunnel type WireGuard, pick a location, enter your VPN username, **Generate Config**. | TorGuard's pages show no real example file and disagree about the port (51820 in one, 1443 in others), so check the fields after pasting. A forwarded port needs the config made for that exact IP. | Yes, as a request in the client area, up to 24 hours to approve. | [DD-WRT guide](https://torguard.net/article/263/how-to-setup-wireguard-on-dd-wrt-with-torguard.html), [OpenWrt guide](https://torguard.net/support/articles/Routers-Modems/privaterouter-openwrt-wireguard-setup.php), [port forwarding](https://torguard.net/support/articles/TorGuard-Software-Features/getting-started-port-forwarding.php) |
| Windscribe | My Account, then the **Config Generator** tab, then **WireGuard**. Pick a location and a port (443 if unsure), keep **Generate new keypair**, **Download Config**. | Paid accounts only. The file has a preshared key, so paste all of it. | Pro only, one temporary port at a time. | [Where to find your configs](https://windscribe.com/knowledge-base/articles/where-do-i-access-my-wireguard-configs), [DD-WRT guide](https://windscribe.com/knowledge-base/articles/manual-wireguard-router-setup-guide-dd-wrt), [port forwarding](https://windscribe.com/knowledge-base/articles/what-is-ephemeral-port-forwarding-and-how-to-use-it) |
| Your own server (PiVPN, wg-easy) | PiVPN: `pivpn add`, then the file in the `configs` folder of your home folder. wg-easy: add a client in its web page. | Both write a preshared key. wg-easy can write `PostUp` lines and `PersistentKeepalive = 0`; Mediarium ignores the first and applies its own 25 seconds when the second is 0. PiVPN's default subnet is likely 10.19.17.0/24. Your traffic leaves from wherever the server is, so a server at home still shows your home address. | Not applicable. | [PiVPN docs](https://docs.pivpn.io/wireguard/), [PiVPN script](https://raw.githubusercontent.com/pivpn/pivpn/master/scripts/wireguard/makeCONF.sh), [wg-easy docs](https://wg-easy.github.io/wg-easy/latest/guides/clients/) |

The sample files the tests use to check all of this are in `internal/vpn/testdata/providers`. They have made-up keys. Each one is in the shape the provider documents, and the file says where its values come from.

### Providers that can't be used here

A provider can only be used here if it lets you get a WireGuard file or a key pair of your own to use in any WireGuard program. These don't, as far as their own pages say:

| Provider | Why not | Source |
|---|---|---|
| CyberGhost | "Does not support manual WireGuard configuration". WireGuard works in its apps only. | [Support article](https://support.cyberghostvpn.com/hc/en-us/articles/360015810460-Discover-More-About-the-WireGuard-VPN-Protocol) |
| ExpressVPN | WireGuard is in its apps only. The manual configuration page lists no WireGuard file. | [Protocols](https://www.expressvpn.com/what-is-vpn/protocols/wireguard), [manual configuration](https://www.expressvpn.com/support/manage-account/find-manual-configuration-credentials/) |
| FastestVPN | We found no page to get the file. Its guides say to ask support for it. If support sends you one, pick **Not listed** and paste it. | [Windows guide](https://support.fastestvpn.com/tutorials/windows/wireguard/) |
| Hotspot Shield | WireGuard is a setting inside its apps. No config file is documented. | [Protocol article](https://support.hotspotshield.com/hc/en-us/articles/360041911131-Which-VPN-protocol-should-I-choose) |
| Mozilla VPN | No support page for a manual config. Its client has a hidden command that exports one, which isn't documented for customers. | [Client change](https://github.com/mozilla-mobile/mozilla-vpn-client/pull/5817) |
| Perfect Privacy | Said in a blog post that WireGuard wasn't ready, and its site couldn't be reached when we checked. | [Blog (archived copy)](https://web.archive.org/web/20250713194043/https://www.perfect-privacy.com/en/blog/wireguard-vpn-pros-and-cons) |
| PureVPN | The Member Area makes a file, but it has to be switched on within minutes (the articles say 5 or 30; a paid add-on stretches it to 24 hours). A stored connection can't work that way. | [Add-on article](https://support.purevpn.com/en_US/account/how-to-add-wireguard-via-member-area), [Linux guide](https://support.purevpn.com/en_US/linux/how-to-setup-wireguard-on-linux) |
| TunnelBear | "Does not currently provide manual configuration". | [Help article](https://help.tunnelbear.com/hc/en-us/articles/360061194991-Does-TunnelBear-provide-manual-configuration-for-the-VPN-connection) |
| VyprVPN | Makes a WireGuard config on demand for each connection, and doesn't offer a file to download. | [Help article](https://support.vyprvpn.com/hc/en-us/articles/360044677511-How-does-VyprVPN-prevent-my-IP-address-from-being-stored-while-using-WireGuard) |
| Cloudflare WARP | Uses WireGuard inside, but there is no export. The files from third-party tools are unofficial and its terms don't allow reverse engineering the app, so it isn't treated as a provider here. | [Terms](https://www.cloudflare.com/application/terms/) |
| Tailscale exit nodes | Needs the Tailscale client. There is no plain WireGuard file. | [Exit nodes](https://tailscale.com/kb/1408/quick-guide-exit-nodes) |

If one of these providers later adds a file download, pick **Not listed** and paste it.

## What the status means

The status only says **Connected** while the tunnel is up and the VPN server has answered recently. It never says so just because a connection is switched on.

| Status | Meaning |
|---|---|
| **Connected (label)** | The tunnel is up and the server answers. |
| **Connecting (label)…** | The connection is starting, or waiting for the server to answer. It moves on by itself within a few seconds. |
| **Disconnected (label)** | The connection is switched on, but not working. The reason is shown under the status, for example that the server address could not be found or that the server stopped answering. |
| **Disconnected** | No connection is switched on. |

After a restart, Mediarium switches the connection that was on back on by itself, and keeps trying (every few seconds at first, then every minute) if the network is not ready yet. While a switched-on connection is not working, its row shows **Reconnect** instead of **Activate**, so you can also retry by hand. **Disconnect** turns it off for good.

While a connection is switched on but not working, torrents wait instead of connecting directly, whatever the kill switch says, so your home address is never shown by accident. Press **Disconnect** if you would rather download without the VPN. Removing the connection that is switched on also disconnects it.

A torrent that started without a VPN doesn't keep going from your home address once a VPN connects, or once the kill switch is switched on while none is connected (or a switched-on VPN is down). Within about a minute it is stopped with "the VPN connection changed" (a torrent that was only seeding just stops seeding). Press **Retry** to carry on through the VPN.

**The "Torrenting without a VPN" notice.** The dashboard warns when you have a torrent indexer and no VPN is connected. If your torrent client already runs behind a VPN of its own (a Gluetun container, for example), press **My download client is already behind a VPN** on the notice, or switch the same option on here under **Settings > VPN**. The notice then stays away (administrators only). Switch the option off to show it again. The notice is also left out by itself when the built-in torrent client is off and your torrents go to an external client, since Mediarium doesn't connect to any peers then. In scripts it is `vpnNoticeAcknowledged` in `/api/settings` (setting `vpn.notice_acknowledged`).

**Check my IP address** appears while connected. It makes a real request through the tunnel and shows the public address the internet sees, so you can check that it is your VPN's address and not your own.

### Is traffic really flowing?

"Connected" means the VPN server has answered. Under the connection that is switched on, Mediarium also shows:

- **Last handshake**, with how long ago it was. WireGuard repeats the handshake about every two minutes while the tunnel is in use, so a few seconds to a couple of minutes is normal. It turns into a warning when the server has been quiet for four minutes or more.
- **Received and sent**, what the tunnel has carried since it connected. If the sent number grows and the received number stays at zero, the server isn't answering.
- **No handshake yet** in a warning colour, when a new tunnel has waited about 20 seconds and the server still hasn't answered. The usual causes are a wrong server address, a wrong key, or a key that has expired.

The **Test** button on the connection goes one step further. It makes a small real request through the tunnel (it gets the public address, waiting at most 8 seconds) and answers in plain words:

- **The tunnel works.** A request came back through the VPN, with the address it came from and the last handshake.
- **The VPN server did not answer**, so the key may have expired or the address is wrong. Private Internet Access (PIA) keys expire, and a stale file looks connected but moves nothing. For PIA, run their script again and load the new file (remove the old connection and add the new one).
- **The VPN server answered earlier but has gone quiet**, or **the tunnel is up but the test request got no answer**. Try another server of your provider; some servers block traffic or are overloaded.

The test only runs when you press the button. Nothing is sent in the background, and no key or password is logged.

### If the server address is IPv6

Docker gives a container IPv4 only unless you turn IPv6 on. If the server address in your connection is an IPv6 address (it looks like `[2a03:1b20:6:f011::a09f]:51820`), Mediarium can't reach it from such a container and says right away: "This VPN server address is IPv6, and this computer or Docker container has no IPv6 connection." Either of these fixes it:

- Use the server's IPv4 address instead. Mullvad lists both for every server. If the address is a host name (for Mullvad, something like `us-den-wg-208.relays.mullvad.net`), use the name: Mediarium looks it up each time and picks the IPv4 address when your container has no IPv6.
- Turn on IPv6 for Docker. In `/etc/docker/daemon.json` set `{"ipv6": true, "fixed-cidr-v6": "fd00:db8:1::/64"}` and restart Docker. In a compose file, give the network `enable_ipv6: true` and an IPv6 subnet. Then recreate the container.

When a host name has both kinds of address and your container can reach IPv6, Mediarium tries IPv4 first and switches to IPv6 if the server doesn't answer within a few seconds.

## The kill switch

The switch is called **Kill switch (optional): only run torrents while a VPN is connected**.

While a VPN is connected, torrents always use it, whatever the switch says. The switch only decides what happens when **no** VPN is connected:

- **On:** a torrent download waits and then fails, with a message that says to connect a VPN, instead of connecting directly. There is no direct fallback. A [proxy](#a-socks5-or-http-proxy-instead) ticked for torrents counts as well: torrents then go through the proxy.
- **Off:** torrents connect directly, so your home address is visible to the swarm. This only applies when no connection is switched on. A connection that is switched on but not working holds torrents back, as described above.

Through the tunnel, uTP stays off. DHT and UDP trackers stay off unless [UDP trackers and DHT through the VPN](#udp-trackers-and-dht) is on; they then use the tunnel, not a socket on your network. The torrent listen port (58264) is not used, and nothing listens on your network, so no incoming connection is ever seen. Settings shows this as "Not used while your VPN is on". Port forwarding through a VPN provider is not supported.

## UDP trackers and DHT

**UDP trackers and DHT through the VPN** sits under the kill switch. It is off until you turn it on.

The built-in VPN is WireGuard inside the app. It does not create a network interface, so an ordinary UDP socket would leave from your home address. Mediarium does not open one while a VPN is connected. Peer connections, HTTP trackers, web seeds and magnet metadata already go through the tunnel. A `udp://` announce does not, unless this switch is on.

Many torrents list only UDP trackers. A private or semi-private torrent also sets the private flag, which turns DHT off, so the UDP announce is the only way to find peers. With the switch off, that torrent never announces while the VPN is connected: it stays at 0 B/s and starts only after you disconnect. Opening a UDP socket on the machine would find the peers, and would also show your home address. The switch sends the announces through the tunnel instead.

- **Off:** UDP trackers and DHT are skipped. HTTP trackers still work.
- **On:** announces and DHT queries leave from the VPN address. Peers and trackers see that address, not your home address. Tracker and DHT names are looked up through the VPN's DNS (the **DNS servers** field on the connection), the same way indexer names are when that switch is on. If the connection has no DNS servers, or a name cannot be looked up, the announce fails and nothing is asked of your home DNS. If the tunnel cannot open the UDP socket, nothing is sent from your home address either. A download that has already started keeps the previous choice until you retry it.
- **uTP stays off**, and nothing listens for incoming peers. The listen port stays unused while the VPN is on.
- **A proxy is not covered.** A SOCKS or HTTP proxy cannot carry this UDP, so with only a proxy the trackers and DHT stay off.

If you only use Usenet, you can also switch off **Use the built-in torrent client** under **Settings > Downloading > Torrents**.

## Indexers through the VPN

**Send indexer traffic through the VPN** (under the kill switch) sends everything Mediarium asks your indexers for through the tunnel: searches, RSS, downloading the `.torrent` or `.nzb`, IRC announce channels and your debrid service. The sites see the VPN's address, and their names are looked up through the tunnel, so your internet provider doesn't see which sites you use. That needs DNS servers on the connection (the **DNS servers** field); without them, public indexer names can't be found through the tunnel. It's off by default.

- **Your own network connects directly.** The tunnel can't reach your home network, so an indexer at a private address (like `192.168.1.20`), on this machine (`localhost`), at a Tailscale address, or at a name without a dot (a Docker container like `prowlarr`) or ending in `.local`, `.lan`, `.home.arpa` or `.internal` connects directly. A public name that points to your home network goes into the tunnel and fails; use the address or the container name instead. A local Prowlarr or Jackett still fetches the real sites from its own network, so they see your server address unless it's behind the VPN itself.
- **No direct fallback.** While the VPN is connecting or down, or no VPN is switched on, indexer searches fail with a message that says so. That doesn't count against the indexer, so it isn't paused. Turn the switch off to search without the VPN.
- **FlareSolverr isn't covered.** For sites behind a Cloudflare check, FlareSolverr fetches the page itself, from its own network, so those sites see that address, not the VPN's. For the built-in one in the `-full` image that's your server address. Cloudflare also ties its pass to the address it saw, so some of those sites may stop working unless FlareSolverr goes through the same VPN. While the switch is on, the page warns about this when FlareSolverr is set up, and an indexer test says when FlareSolverr was used.
- **With a proxy too**, the VPN wins for indexer traffic.

## A SOCKS5 or HTTP proxy instead

If you already run a proxy, such as the SOCKS5 port of a VPN container (Gluetun, for example), a seedbox or Tor, Mediarium can send traffic through it instead. The **Proxy** card is on the same page. OpenVPN isn't built in, because it needs the extra privileges this app doesn't ask for, but a VPN container with a SOCKS5 or HTTP proxy gives you the same result.

1. Pick **SOCKS5** or **HTTP**, and type the host (an IP address or a container name) and the port. Add a user name and password if the proxy wants them. The password is stored encrypted and never shown again.
2. Tick what goes through it:
   - **Torrents**: peers, trackers and web seeds. The rules are the same as for the VPN: no direct connections, no DHT or uTP, no UDP trackers and no incoming peers. A proxy cannot carry UDP trackers or DHT. A connected VPN still takes over for torrents.
   - **Indexers**: searches, and fetching NZB and torrent files from them.
   - **Usenet**: the connections to your Usenet servers. Usually not needed, since they're already encrypted.
3. Press **Test**. Mediarium connects through the proxy to TMDB's server (a secure connection is opened and closed, nothing is sent) and says whether it worked. Then press **Save**.

A change applies to new connections straight away. A download that's already running keeps the connection it has.

SOCKS5 is the better choice when you have both. Names are looked up by the proxy, not on your network. Some HTTP proxies only let secure web traffic through, so torrents and Usenet may be refused by them.

Scripts: `GET /api/proxy` (the password is never returned; `hasPassword` says whether there is one), `PUT /api/proxy` with `{type, host, port, username, password, torrents, indexers, usenet}` (`type` `""` switches it off, leaving `password` out keeps the saved one) and `POST /api/proxy/test` with the same fields. The settings are `network.proxy` (encrypted) and `network.proxy_for`.

## Checks on what you enter

The endpoint is `host:port` with a port from 1 to 65535, the two keys are 44-character WireGuard keys, and the tunnel address is an IP address with an optional `/prefix`. The page points out a problem under the field, and the server refuses the same mistakes from a script.

## For scripts

The routes are `GET/POST /api/vpn/configs`, `DELETE /api/vpn/configs/{id}`, `POST /api/vpn/configs/{id}/activate`, `POST /api/vpn/deactivate`, `GET /api/vpn/status` (`connected`, `state` of `off`, `connecting`, `connected` or `down`, `label`, `reason`, and while a tunnel exists `tunnelUp`, `lastHandshake`, `handshakeAgeSec`, `noHandshake`, `rxBytes`, `txBytes`), `POST /api/vpn/test` (`ok`, `ip`, `reason`, `handshakeAgeSec`, `rxBytes`, `txBytes`) and `GET /api/vpn/egress-ip`, all for administrators ([reference/api.md](./reference/api.md)). The kill switch is the setting `vpn.require_for_torrents`, indexers through the VPN is `vpn.route_indexers` (`routeIndexersThroughVpn` in `/api/settings`), and UDP trackers and DHT through the VPN is `vpn.discover_through_tunnel` (`discoverThroughTunnel`). The code is in `internal/vpn`.
