# MCsniperGO

<h3 align="center">
  <img src="https://i.imgur.com/ShMq72J.png" alt="MCsniperGO"></img>

  By Kqzz
</h3>

<p align="center">
    <a href="https://github.com/chezburg/mc-sniper-go/releases/"><img alt="downloads" src="https://img.shields.io/github/downloads/chezburg/mc-sniper-go/total?color=%233889c4" height="22"></a>
    <a href="https://discord.gg/mcsnipergo-734794891258757160"><img alt="Discord" src="https://img.shields.io/discord/734794891258757160?label=discord&color=%233889c4&logo=discord&logoColor=white" height="22"></a>
    <h3 align="center" > <a href="https://discord.gg/mcsnipergo-734794891258757160">Join Discord</a> </h3>
</p>

## Quick Start (Docker)

```bash
# Clone and configure
git clone https://github.com/chezburg/mc-sniper-go.git
cd mc-sniper-go

# Copy environment template
cp .env.example .env

# Edit .env with your credentials
nano .env

# Run with Docker Compose
docker compose up -d
```

## Environment Variables

### VPN Configuration (gluetun-style)

```bash
VPN_SERVICE_PROVIDER=mullvad    # mullvad, protonvpn, wireguard
VPN_TYPE=wireguard

# WireGuard (get from your VPN provider)
WIREGUARD_PRIVATE_KEY=
WIREGUARD_ADDRESSES=

# Server selection (comma-separated)
SERVER_COUNTRIES=Canada
SERVER_CITIES=Toronto,Montreal,Vancouver,Calgary
```

### Minecraft Accounts

```bash
# Gift Code accounts (no username)
GC_ACCOUNTS=email:password,email:password

# GamePass accounts
GP_ACCOUNTS=email:password

# Microsoft accounts (have usernames)
MS_ACCOUNTS=email:password
# Or use bearer tokens:
# MS_ACCOUNTS=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

### Proxies (optional)

```bash
PROXIES=http://user:pass@ip:port,http://ip:port
```

### Rotator Settings

```bash
VPN_MAX_REQUESTS_PER_REGION=25
VPN_MIN_ROTATION_INTERVAL=5s
VPN_DETECT_ON_429=true
VPN_PREDICTIVE_THRESHOLD=80
```

## Usage

- Run `docker compose up`
- Enter username when prompted
- Enter claim range (Unix timestamps from-to)

## Claim Range

Use the following bookmarklet on `namemc.com/search?q=<username>`:

```js
javascript:(function(){function parseIsoDatetime(dtstr) { return new Date(dtstr); };
startElement = document.getElementById('availability-time');
endElement = document.getElementById('availability-time2');
start = parseIsoDatetime(startElement.getAttribute('datetime'));
end = parseIsoDatetime(endElement.getAttribute('datetime'));
para = document.createElement("p");
para.innerText = Math.floor(start.getTime() / 1000) + '-' + Math.ceil(end.getTime() / 1000);
endElement.parentElement.appendChild(para);})();
```

## Understanding Logs

- 400/403: Failed (will retry)
- 401: Unauthorized (restart)
- 429: Rate limited (rotate VPN/proxies)