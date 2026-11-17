<div align="center">

# neofreebox

**A fast, self-hosted control panel for the Freebox Pop.**

Everything the Freebox OS web interface does, over the official Freebox OS API,
in a lightweight web app you run on your own server.

[![CI](https://github.com/neotoxicfr/neofreebox/actions/workflows/pr.yml/badge.svg)](https://github.com/neotoxicfr/neofreebox/actions/workflows/pr.yml)
[![License: GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-blue.svg)](LICENSE)

</div>

> [!WARNING]
> **Work in progress.** Nothing is released yet; the roadmap below shows what
> is done.

## Why neofreebox

The Freebox OS interface is a heavy single-page app (several megabytes of
JavaScript) that gets slow and patchy once it is put behind a reverse proxy to
reach it from outside. neofreebox talks to the same official API, renders its
pages on the server and keeps the browser side to a minimum:

- **Fast anywhere**: plain HTML pages, live values streamed with Server-Sent
  Events, nothing to download up front.
- **Safe to expose**: its own login, made to sit behind an authenticating
  reverse proxy, and the box's app token never leaves the server.
- **Careful with the network**: anything that can cut your connection
  (Wi-Fi, DHCP, bridge mode, reboot) asks before it is sent.

## Roadmap

Network comes first, then the rest of Freebox OS:

- [x] Freebox OS API client: app pairing, sessions
- [x] App shell: login, box status, container image
- [ ] Live events from the box (WebSocket)
- [ ] Network: devices, DHCP, port forwarding, DMZ, UPnP, Wi-Fi, IPv6, DNS, remote access
- [ ] WireGuard VPN server: peers, config files and QR codes
- [ ] Monitoring: live bandwidth, history, events, fibre line
- [ ] System: reboot, updates, LEDs, parental control, Freeplugs
- [ ] Downloads
- [ ] Files and shares (SMB, FTP, UPnP AV, AirMedia)
- [ ] TV: channels, programme guide, recordings
- [ ] Phone: call log, contacts, voicemail

## Requirements

- A Freebox with the Freebox OS API (tested on a Freebox Pop, API v16).
- A server on the box's local network that runs Docker.
- A reverse proxy with HTTPS in front of it (Traefik, Caddy…), ideally with
  its own authentication (Authelia, Authentik…).

## Getting started

1. **Pair the app**, once, from a machine on the box's local network. The
   request shows on the screen on top of the box (turn the screen on if it is
   off) and must be accepted there within a minute or two:

   ```bash
   docker run --rm -it -v "$PWD/secrets:/out" ghcr.io/neotoxicfr/neofreebox:latest \
     pair -o /out/freebox_app_token
   ```

2. **Grant every right** in Freebox OS → Paramètres de la Freebox → Gestion
   des accès → Applications → neofreebox (new apps only get a few by default).
3. **Start it** with [`deploy/compose.yml.example`](deploy/compose.yml.example);
   `docker compose run --rm neofreebox check` prints the rights it got.
4. Open it: the first visit asks you to create the account.

| Variable | Default | |
| --- | --- | --- |
| `NEOFREEBOX_APP_TOKEN_FILE` | — | file holding the app token (required) |
| `NEOFREEBOX_BOX_URL` | `https://mafreebox.freebox.fr` | the box |
| `NEOFREEBOX_DATA_DIR` | `/data` | where the account is kept |
| `NEOFREEBOX_ADDR` | `:8080` | listen address |

## How it works

- A single Go binary with the HTML templates, stylesheet and scripts embedded;
  no database, since the state lives on the box.
- `internal/fbx` is the Freebox OS API client: app pairing, HMAC-SHA1 challenge
  sessions renewed on expiry, and the box's WebSocket event stream. The box is
  reached over HTTPS with Free's root certificates embedded.
- The image is `distroless/static`, non-root, read-only.

See [SECURITY.md](SECURITY.md) for the security model and
[CONTRIBUTING.md](CONTRIBUTING.md) to build and test it.

## License

[GPL-3.0](LICENSE). neofreebox is not affiliated with Free or Iliad.
