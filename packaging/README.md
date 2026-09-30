# Packaging

| Folder | What | Status |
|---|---|---|
| [`stores/`](./stores/) | Ready-to-submit kits for Unraid, CasaOS/ZimaOS, Umbrel, TrueNAS, Portainer, Synology, Runtipi and Cosmos | Prepared, not submitted: see [stores/README.md](./stores/README.md) |
| [`windows/`](./windows/) | Inno Setup installer script and a helper script for the native Windows program | **Coming soon.** Draft, not tested as an installer |
| [`macos/`](./macos/) | Install script and launchd agent for the native macOS program | **Coming soon.** Draft, never run on a Mac |
| [`systemd/`](./systemd/) | systemd unit and environment file for the native Linux program | **Coming soon.** Draft, not tested |
| `README-INSTALL.txt` | Notes packed into the native release archives | Explains that the native programs are a preview |

The supported install today is Docker: see [docs/INSTALL.md](../docs/INSTALL.md). The native Windows, macOS and Linux files are kept here so they can be finished and tested; they are not offered to users yet ([docs/PLATFORMS.md](../docs/PLATFORMS.md)).
