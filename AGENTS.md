# AGENTS.md

## Scope

These instructions apply to the `zjunet-go` project.

## Project Overview

`zjunet-go` is a Go 1.22 Linux daemon for Zhejiang University Campus Network
L2TP access. It loads a root-readable JSON config, writes `pppd` and `xl2tpd`
configuration, starts or restarts `xl2tpd.service`, connects a configured LAC,
then monitors PPP, route, DNS, and optional NAT state.

The code is intentionally narrower than the original `QSCTech/zjunet` shell
client: single user, no WLAN login, no interactive prompting, and no multi-user
load balancing.

## Common Commands

```bash
go test ./...
go build -o zjunet-go ./cmd/zjunet-go
gofmt -w cmd pkg
```

Use package-level tests while iterating on a focused change, for example:

```bash
go test ./pkg/route
go test ./pkg/l2tp
```

## Layout

- `cmd/zjunet-go/main.go`: command entry point.
- `pkg/cli`: command parsing and daemon invocation.
- `pkg/config`: JSON config defaults and validation.
- `pkg/daemon`: top-level lifecycle and reconciliation loop.
- `pkg/l2tp`: `xl2tpd`, `pppd`, PPP detection, and service control.
- `pkg/route`: route, DNS, runtime state, and NAT management.
- `pkg/system`: root checks, dependency checks, command helpers, and atomic writes.

## Coding Guidelines

- Preserve the small-package structure and keep Linux side effects behind narrow
  helper functions that tests can replace.
- Prefer `context.Context` and bounded timeouts for external commands and network
  probes.
- Use `exec.CommandContext` with explicit argument slices; do not build shell
  command strings for system operations.
- Keep generated system files deterministic and owned by a single managed block
  where possible.
- Treat `/etc`, `/run`, routes, DNS, and firewall changes as high-risk behavior;
  add tests around parsing, idempotency, and cleanup logic when changing them.
- Do not commit real NetID credentials, generated local config files, logs, or
  host-specific route/firewall output.
- Run `gofmt` before finishing Go code changes.

## Testing Notes

Unit tests should avoid requiring root or mutating the host. Existing packages
use replaceable variables for filesystem paths, command runners, and interface
lookups; follow that pattern for new tests.

Only run `sudo zjunet-go start` manually on a Linux host where modifying
`xl2tpd`, PPP, DNS, routes, and optional `iptables` state is expected.
