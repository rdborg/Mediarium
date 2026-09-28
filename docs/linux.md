# Deploying on generic Linux

> The full, current guide is [INSTALL.md](./INSTALL.md). Note its **folder layout** section: the four separate volumes in the commands below import by copying, not hardlinking (a hardlink cannot cross a container mount point). Use one `/data` mount with `DOWNLOADS_DIR`/`MOVIES_DIR`/`TV_DIR` for hardlinks. A native (non-Docker) binary with a systemd unit is covered there too.

## Docker Compose (recommended)
```bash
git clone <this repo>
cd <this repo>
cp .env.example .env   # adjust PUID/PGID/TZ
docker compose up -d
```

## Plain `docker run`
```bash
docker run -d \
  --name mediarium \
  -p 8080:8080 \
  -e PUID=1000 -e PGID=1000 -e TZ=Etc/UTC \
  -v ./config:/config \
  -v ./downloads:/downloads \
  -v ./movies:/movies \
  -v ./tv:/tv \
  --restart unless-stopped \
  ghcr.io/rdborg/mediarium:latest
```

## systemd (non-Docker-native setups)

For a host that manages services via systemd rather than Docker's own restart policies, wrap `docker compose` in a unit so `systemctl start/stop/status mediarium` works normally and it comes up on boot:

```ini
# /etc/systemd/system/mediarium.service
[Unit]
Description=Mediarium (docker compose)
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/mediarium
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
```

Adjust `WorkingDirectory` to wherever you cloned this repo (it needs `docker-compose.yml` and your `.env` alongside it). Then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now mediarium
```

## Troubleshooting

See the root [README](../README.md#troubleshooting) for the common permission/hardlinking issues — they apply the same way here as on Synology/Unraid, just with whatever host paths you chose instead of NAS-specific ones.
