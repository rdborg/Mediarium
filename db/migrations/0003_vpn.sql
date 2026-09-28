-- Phase 2: VPN configs (PRD.md §4.7). "Provider" is a display label only —
-- the tunnel itself is always driven by a generic WireGuard config
-- (private key, peer public key, endpoint, allowed IPs); the "provider
-- picker" in the UI is just a friendly way to produce one.

CREATE TABLE vpn_configs (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    label                  TEXT NOT NULL,       -- e.g. "Mullvad - Sweden" or "Custom"
    provider               TEXT NOT NULL DEFAULT 'custom',
    private_key_encrypted  TEXT NOT NULL,
    peer_public_key        TEXT NOT NULL,
    preshared_key_encrypted TEXT,
    endpoint               TEXT NOT NULL,
    allowed_ips            TEXT NOT NULL DEFAULT '0.0.0.0/0,::/0',
    local_addresses        TEXT NOT NULL,       -- comma-separated
    dns                    TEXT,                -- comma-separated, optional
    active                 INTEGER NOT NULL DEFAULT 0,
    created_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_vpn_configs_active ON vpn_configs(active);
