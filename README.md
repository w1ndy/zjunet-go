# zjunet-go

`zjunet-go` is a Go VPN client for the Zhejiang University Campus Network on
Linux. It manages a single L2TP account, generates `xl2tpd`/`pppd`
configuration, runs as a foreground daemon, and keeps the tunnel and routes in
shape for unattended use.

This project is adapted from [QSCTech/zjunet](https://github.com/QSCTech/zjunet)
and narrows the original shell client to a Linux, single-user Go daemon. It does
not include WLAN login, interactive prompts, or multi-user load balancing.

## Features

- Single-account JSON configuration.
- Deterministic `xl2tpd` and `pppd` config generation.
- Foreground daemon mode suitable for `systemd`.
- Continuous monitoring of `xl2tpd`, PPP interfaces, and routes.
- Automatic reconnect and route repair when the tunnel drops or drifts.
- Optional DNS management and NAT/MASQUERADE setup for router use cases.

## Requirements

- Linux with `systemd`
- Root privileges for `start`
- Go 1.22 or newer to build from source
- `xl2tpd`
- `pppd`
- `xl2tpd-control`
- `iproute2`
- `procps` tools such as `pgrep` and `pkill`
- `iptables` and `sysctl` when `manage_nat` is enabled

## Build

```bash
go build -o zjunet-go ./cmd/zjunet-go
```

Install the binary somewhere root-managed if you plan to run it through
`systemd`:

```bash
sudo install -m 755 zjunet-go /usr/local/bin/zjunet-go
```

## Configuration

By default, `zjunet-go` reads `/etc/zjunet-go/config.json`.

```json
{
  "user": "YOUR_NETID",
  "password": "YOUR_PASSWORD",
  "lns": "10.5.1.9",
  "lac_name": "zjunet-go",
  "mtu": 1428,
  "dns": ["10.10.0.21"],
  "manage_dns": false,
  "manage_route": true,
  "manage_nat": false
}
```

Only `user` and `password` are required. Missing optional fields use the
defaults shown above.

Keep the config readable only by root:

```bash
sudo install -d -m 700 /etc/zjunet-go
sudo install -m 600 config.example.json /etc/zjunet-go/config.json
sudoedit /etc/zjunet-go/config.json
```

## Usage

Run the daemon in the foreground:

```bash
sudo zjunet-go start
```

Useful flags:

```bash
sudo zjunet-go start --config /etc/zjunet-go/config.json --interval 15s --wait 60s
sudo zjunet-go start --no-route
```

- `--config` selects a different config file.
- `--interval` controls the monitoring loop interval.
- `--wait` controls how long to wait for a PPP interface after dialing.
- `--no-route` skips route changes even when `manage_route` is true.

Generated and runtime-managed paths:

- `/etc/ppp/peers/zjunet-go`
- `/etc/xl2tpd/xl2tpd.conf`
- `/run/zjunet-go/state.json`

`zjunet-go` inserts a managed LAC block into `xl2tpd.conf` and rewrites only its
own managed block on subsequent runs.

## NAT Mode

If this Linux host also routes traffic for LAN clients, enable NAT explicitly:

```json
{
  "manage_nat": true
}
```

NAT mode enables IPv4 forwarding and installs idempotent `iptables`
MASQUERADE/FORWARD rules through the active PPP interface. Make sure your
`iptables` alternative matches the rest of your firewall stack, such as
`iptables-legacy` or `iptables-nft`.

## Systemd

Example unit:

```ini
[Unit]
Description=ZJU L2TP client
After=network-online.target xl2tpd.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/zjunet-go start
Restart=always
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

After writing the unit:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now zjunet-go.service
sudo journalctl -u zjunet-go.service -f
```

## Development

```bash
go test ./...
go build -o zjunet-go ./cmd/zjunet-go
```

Most package tests avoid touching the real host network by replacing command and
filesystem hooks. Do not run `zjunet-go start` on a development machine unless
you are ready for it to update `xl2tpd`, PPP, DNS, routes, and optional firewall
state.
