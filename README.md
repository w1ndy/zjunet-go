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
- `ping` from `iputils`
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

## Packages

Release artifacts include `.deb` and `.rpm` packages for Linux. The packages
install:

- `/usr/bin/zjunet-go`
- `/usr/lib/systemd/system/zjunet-go.service`
- `/etc/zjunet-go/`
- `/usr/share/doc/zjunet-go/config.example.json`

Package metadata declares the runtime dependencies needed by the daemon,
including `xl2tpd`, `ppp`, `iproute2`/`iproute`, `procps`/`procps-ng`,
`iputils-ping`/`iputils`, `systemd`, and `iptables`.

The package does not install a live `/etc/zjunet-go/config.json` with placeholder
credentials. Create it from the packaged example:

```bash
sudo install -m 600 /usr/share/doc/zjunet-go/config.example.json /etc/zjunet-go/config.json
sudoedit /etc/zjunet-go/config.json
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

When installed from a `.deb` or `.rpm`, the package provides
`zjunet-go.service`. After creating `/etc/zjunet-go/config.json`, enable it with:

```bash
sudo systemctl enable --now zjunet-go.service
sudo journalctl -u zjunet-go.service -f
```

For a manual install, copy the packaged unit into `/etc/systemd/system` and point
`ExecStart` at your installed binary if needed:

```ini
[Unit]
Description=Zhejiang University Campus Network VPN client
Documentation=https://github.com/w1ndy/zjunet-go
After=network-online.target xl2tpd.service
Wants=network-online.target

[Service]
Type=simple
Environment=ZJUNET_GO_ARGS=
EnvironmentFile=-/etc/default/zjunet-go
EnvironmentFile=-/etc/sysconfig/zjunet-go
ExecStart=/usr/bin/zjunet-go start $ZJUNET_GO_ARGS
Restart=always
RestartSec=10s
RuntimeDirectory=zjunet-go

[Install]
WantedBy=multi-user.target
```

After writing a manual unit:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now zjunet-go.service
sudo journalctl -u zjunet-go.service -f
```

## Development

```bash
go test ./...
go build -o zjunet-go ./cmd/zjunet-go
goreleaser check
```

Most package tests avoid touching the real host network by replacing command and
filesystem hooks. Do not run `zjunet-go start` on a development machine unless
you are ready for it to update `xl2tpd`, PPP, DNS, routes, and optional firewall
state.

Releases are built by GitHub Actions when a GitHub Release is published. The
workflow runs tests, then uses GoReleaser to upload Linux binary archives,
checksums, `.deb` packages, and `.rpm` packages to the release.
