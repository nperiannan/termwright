# Termwright team server

A team that shares a lab can see in Termwright **who uses which device**. Each team runs its own small server - usually a Docker container on a Linux host of its lab - and every member's Termwright connects to it. No cloud service and no company-wide administrator are involved: a team sets up its own server, and several teams can use the same host.

Teams are **optional**. Without one, Termwright works exactly as before and sends nothing anywhere.

| Teams | Set Up a Team Server |
|---|---|
| ![Teams](screenshots/teams.png) | ![Set Up a Team Server](screenshots/teams-setup.png) |

## What is shared

- Who is who: the name each person chose, their user name and computer name.
- Device status, from the next release: which devices someone has open, and reservations with their note and end time.

Passwords, keys, session files and terminal output are **never** sent. The server keeps only the team, its members and device status.

## Setting a team server up from Termwright (recommended)

**Options → Teams… → Set Up a Team Server…**

1. **Host**: pick an SSH session of a Linux host with Docker. Termwright logs in the way the session does - a key login is best.
2. **Team name**, e.g. *Switching Lab B*. The container is called `termwright-team-switching-lab-b`.
3. **Port**: leave it empty and Termwright picks the first free port from 7443. It never uses a port that is already taken (80, 8080 or anything else).
4. **Container image**:
   - *Download (GitHub)*: the host downloads `ghcr.io/nperiannan/termwright-team`.
   - *Copy over SSH*: this computer downloads the server program and copies it to the host, which builds the image itself. Use this for labs without internet. *Download* falls back to it by itself.
5. **Set Up**. Termwright checks Docker, starts the container with `--restart unless-stopped` and a data volume of its own, checks that this computer can reach it, and makes you the **owner**. The invite appears at the end: send it to your team.

When the user may not use Docker, Termwright says so. The fix on the host is `sudo usermod -aG docker <user>`, then log in again. Sudo without a password for docker works too.

When the host's firewall blocks the port, the server runs but cannot be reached. Open the port, then set up again or join with the invite:

- Ubuntu/Debian: `sudo ufw allow 7443/tcp`
- RHEL/Rocky: `sudo firewall-cmd --add-port=7443/tcp --permanent && sudo firewall-cmd --reload`

## Joining a team

**Options → Teams… → Join a Team…**, then paste the invite. It looks like `termwright-team://10.234.179.141:7443/?fp=…&k=…&n=…`.

The invite holds:

- the server's address;
- the team key;
- the **fingerprint** of the server's certificate. Termwright talks only to the server with that certificate, so a different machine at the same address is refused.

The key that proves your membership is kept in the system keychain, not in `settings.json`.

## Roles

| Role | Can |
|---|---|
| **Owner** (who set the server up) | everything: see and renew the invite, make members co-owners and back, remove anyone |
| **Co-owner** | see and renew the invite, make members co-owners, remove members |
| **Member** | see the team and device status; reserve devices (next release) |

**New Invite** stops the old invite; members who already joined stay in the team. A removed member is out at once and needs a new invite to come back. The owner can leave only when nobody else is in the team.

## Installing by hand

Each release has `termwright-team-linux-amd64.tar.gz` and `termwright-team-linux-arm64.tar.gz`. Each holds:

- the program (`termwright-team`, one static file);
- a `Dockerfile`, a `docker-compose.yml` and a systemd unit;
- this guide (`INSTALL.md`).

The server listens on 7443 inside the container (HTTPS with its own certificate) and keeps its data in `/data`.

**Docker, image from GitHub:**

```
docker run -d --name termwright-team-my-lab --restart unless-stopped \
  -p 7443:7443 -v termwright-team-my-lab:/data -e TW_TEAM_NAME="My Lab" \
  ghcr.io/nperiannan/termwright-team:latest
```

**Docker without internet** (from the unpacked package):

```
docker build -t termwright-team .
docker run -d --name termwright-team-my-lab --restart unless-stopped \
  -p 7443:7443 -v termwright-team-my-lab:/data -e TW_TEAM_NAME="My Lab" termwright-team
```

**Docker Compose**: edit the name and port in `docker-compose.yml`, then `docker compose up -d`.

**Without Docker (systemd):**

```
sudo cp termwright-team /usr/local/bin/
sudo cp termwright-team.service /etc/systemd/system/
sudo systemctl enable --now termwright-team
```

Use another port on the host by changing the left side of `-p` (e.g. `-p 7450:7443`), or `TW_LISTEN` for systemd.

**Becoming the owner after a manual install**: the server prints a one-time setup code at its first start:

```
docker logs termwright-team-my-lab | grep owner-code
```

The line reads `TERMWRIGHT-TEAM owner-code=… fingerprint=…`. Join with an invite you build from it:

```
termwright-team://HOST:PORT/?k=OWNER-CODE&fp=FINGERPRINT
```

(`k` is the owner code here.) Termwright then makes you the owner, and the code stops working.

## Settings of the program

| Flag | Variable | Default | |
|---|---|---|---|
| `-data` | `TW_DATA` | `/data` | team.json, cert.pem, key.pem |
| `-listen` | `TW_LISTEN` | `:7443` | address and port |
| `-name` | `TW_TEAM_NAME` | `Team` | the team's name (first start only) |

**Backup**: copy the data folder or volume (`docker run --rm -v termwright-team-my-lab:/d -v "$PWD":/b alpine tar czf /b/team-backup.tgz -C /d .`). The certificate is part of it: a server set up again with a new certificate needs a new invite for everyone.

**Update**: `docker pull ghcr.io/nperiannan/termwright-team:latest`, then remove the container and start it again with the same volume. The team and its members are kept.

**Remove**: `docker rm -f termwright-team-my-lab && docker volume rm termwright-team-my-lab`.
