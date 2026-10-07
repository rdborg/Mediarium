# VPN protection

Mediarium has a WireGuard VPN client built in, for torrent traffic. It runs inside the app, so the container needs no extra privileges: no `NET_ADMIN` and no `/dev/net/tun`.

Find it under **Settings > Downloading > VPN protection**. It's optional: nothing goes through a VPN until you add a connection and activate it. From then on, all torrent traffic goes through it while it's connected.

## What goes through the VPN

- **Torrent traffic** (talking to peers, to HTTP trackers, to web seeds and fetching the details a magnet link points to), whenever a VPN is connected. UDP trackers (`udp://`) cannot go through the tunnel, so they are not used while the VPN is on; HTTP trackers, the other peers and the swarm still work.
- **Nothing else.** Searching your indexers, TMDB, subtitles, media servers and your Usenet provider all connect directly. Usenet downloads are encrypted between you and your provider, and do not show your address to other people the way a torrent swarm does.

## Add a connection

1. Pick your provider in **Add a VPN connection**: Mullvad, ProtonVPN, Private Internet Access, Surfshark, NordVPN, Windscribe, or **Custom / other provider**. The list only changes the hint and the link shown. Every provider ends up with the same fields, and a WireGuard server of your own works too.
2. Get your WireGuard details from your provider, using the link and short notes on the page. Mediarium never asks for your VPN account login. You copy the values from your provider's own page.
3. The quickest way: paste your whole WireGuard `.conf` file into **Paste your .conf file**, or press **Or choose the file**. Every field below is filled in from it, including the preshared key, DNS servers and allowed IPs. Only the first `[Peer]` is used. Or fill in the form by hand:

| Field | What to enter |
|---|---|
| Label | A name for you, for example "My VPN". |
| Endpoint | The server's address and port, like `vpn.example.com:51820`. |
| Private key | Your WireGuard private key (44 characters). |
| Peer public key | The server's public key (44 characters). |
| Preshared key | Optional. Only if your config has a `PresharedKey` line (Windscribe's do). Without it the tunnel never connects. |
| Local tunnel address | The address your provider gives you, like `10.2.0.2/32`. Several can be separated by commas. |
| DNS servers | Optional. The `DNS` line of your config, like `10.2.0.1`. |
| Allowed IPs | Optional. Leave empty to send everything through the tunnel (`0.0.0.0/0, ::/0`). |

4. Press **Add connection**, then **Activate** on its row. The status at the top says **Connected** followed by the connection's label in brackets, once the VPN server has answered. **Disconnect** turns the tunnel off again.

You can store several connections; one is active at a time. Private and preshared keys are stored encrypted (with the key in `secret.key`) and are never shown again or written to the log.

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

**Check my IP address** appears while connected. It makes a real request through the tunnel and shows the public address the internet sees, so you can check that it is your VPN's address and not your own.

## The kill switch

The switch is called **Kill switch (optional): only run torrents while a VPN is connected**.

While a VPN is connected, torrents always use it, whatever the switch says. The switch only decides what happens when **no** VPN is connected:

- **On:** a torrent download waits and then fails, with a message that says to connect a VPN, instead of connecting directly. There is no direct fallback.
- **Off:** torrents connect directly, so your home address is visible to the swarm. This only applies when no connection is switched on. A connection that is switched on but not working holds torrents back, as described above.

Through the tunnel, DHT and uTP are turned off (they need a direct UDP connection, which would show your address), so magnet links rely on the trackers they carry. The torrent listen port (58264) is not used, and nothing listens on your network, so no incoming connection is ever seen. Settings shows this as "Not used while your VPN is on". Port forwarding through a VPN provider is not supported.

If you only use Usenet, you can also switch off **Use the built-in torrent client** under **Usenet and torrents**.

## Checks on what you enter

The endpoint is `host:port` with a port from 1 to 65535, the two keys are 44-character WireGuard keys, and the tunnel address is an IP address with an optional `/prefix`. The page points out a problem under the field, and the server refuses the same mistakes from a script.

## For scripts

The routes are `GET/POST /api/vpn/configs`, `DELETE /api/vpn/configs/{id}`, `POST /api/vpn/configs/{id}/activate`, `POST /api/vpn/deactivate`, `GET /api/vpn/status` (`connected`, `state` of `off`, `connecting`, `connected` or `down`, `label`, `reason`) and `GET /api/vpn/egress-ip`, all for administrators ([reference/api.md](./reference/api.md)). The kill switch is the setting `vpn.require_for_torrents`. The code is in `internal/vpn`.
