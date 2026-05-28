package route

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/w1ndy/zjunet-go/pkg/config"
	"github.com/w1ndy/zjunet-go/pkg/system"
)

const RuntimeDir = "/run/zjunet-go"

const (
	iptablesBinary      = "iptables"
	iptablesWaitSeconds = "5"
)

var outputContext = system.OutputContext

var Prefixes = []string{
	"10.0.0.0/8",
	"10.50.200.245",
	"58.196.192.0/19",
	"58.196.224.0/20",
	"58.200.100.0/24",
	"210.32.0.0/20",
	"210.32.128.0/19",
	"210.32.160.0/21",
	"210.32.168.0/22",
	"210.32.172.0/23",
	"210.32.174.0/24",
	"210.32.176.0/20",
	"222.205.0.0/17",
}

type State struct {
	Gateway   string    `json:"gateway,omitempty"`
	Device    string    `json:"device,omitempty"`
	PPPDevice string    `json:"ppp_device,omitempty"`
	SavedAt   time.Time `json:"saved_at"`
}

type Info struct {
	Gateway string
	Device  string
}

func Setup(ctx context.Context, cfg config.Config, state State, ppp string) error {
	dns := config.FirstDNS(cfg)
	if state.Gateway != "" {
		if err := system.RunContext(ctx, "ip", "route", "replace", cfg.LNS, "via", state.Gateway); err != nil {
			return err
		}
		if err := system.RunContext(ctx, "ip", "route", "replace", dns, "via", state.Gateway); err != nil {
			return err
		}
		for _, prefix := range Prefixes {
			if err := system.RunContext(ctx, "ip", "route", "replace", prefix, "via", state.Gateway); err != nil {
				return err
			}
		}
	}
	if err := system.RunContext(ctx, "ip", "route", "replace", "default", "dev", ppp); err != nil {
		return err
	}
	_ = system.RunContext(ctx, "ip", "route", "flush", "cache")
	return nil
}

func SetupNAT(ctx context.Context, cfg config.Config, ppp string) error {
	if !cfg.ManageNAT {
		return nil
	}
	if _, err := outputContext(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return err
	}
	if err := ensureRule(ctx, "-t", "nat", "-A", "POSTROUTING", "-o", ppp, "-j", "MASQUERADE"); err != nil {
		return err
	}
	if err := ensureRule(ctx, "-A", "FORWARD", "-o", ppp, "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := ensureRule(ctx, "-A", "FORWARD", "-i", ppp, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err != nil {
		return err
	}
	return nil
}

func RestoreNAT(ctx context.Context, cfg config.Config, ppp string) {
	if !cfg.ManageNAT || ppp == "" {
		return
	}
	deleteRule(ctx, "-t", "nat", "-D", "POSTROUTING", "-o", ppp, "-j", "MASQUERADE")
	deleteRule(ctx, "-D", "FORWARD", "-o", ppp, "-j", "ACCEPT")
	deleteRule(ctx, "-D", "FORWARD", "-i", ppp, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
}

func NATHealthy(ctx context.Context, cfg config.Config, ppp string) bool {
	if !cfg.ManageNAT {
		return true
	}
	if err := checkRule(ctx, "-t", "nat", "-C", "POSTROUTING", "-o", ppp, "-j", "MASQUERADE"); err != nil {
		return false
	}
	if err := checkRule(ctx, "-C", "FORWARD", "-o", ppp, "-j", "ACCEPT"); err != nil {
		return false
	}
	return checkRule(ctx, "-C", "FORWARD", "-i", ppp, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT") == nil
}

func Healthy(ctx context.Context, cfg config.Config, state State, ppp string) bool {
	if ppp == "" {
		return false
	}
	if out, err := outputContext(ctx, "ip", "route", "show", "default"); err != nil || !routeOutputHasDevice(out, ppp) {
		return false
	}
	if state.Gateway == "" {
		return true
	}
	for _, target := range []string{cfg.LNS, config.FirstDNS(cfg)} {
		out, err := outputContext(ctx, "ip", "route", "get", target)
		info, parseErr := parseRouteInfo(target, out)
		if err != nil || parseErr != nil || info.Gateway != state.Gateway {
			return false
		}
	}
	return true
}

func Restore(ctx context.Context, cfg config.Config, state State) error {
	for _, prefix := range append([]string{cfg.LNS, config.FirstDNS(cfg), "10.5.1.0/24", "10.10.0.0/24"}, Prefixes...) {
		_ = system.RunContext(ctx, "ip", "route", "del", prefix)
	}
	if state.Gateway != "" && state.Device != "" {
		if err := system.RunContext(ctx, "ip", "route", "replace", "default", "via", state.Gateway, "dev", state.Device); err != nil {
			return err
		}
	}
	_ = system.RunContext(ctx, "ip", "route", "flush", "cache")
	return nil
}

func ensureRule(ctx context.Context, args ...string) error {
	checkArgs := append([]string{}, args...)
	for i, arg := range checkArgs {
		if arg == "-A" {
			checkArgs[i] = "-C"
			break
		}
	}
	if err := checkRule(ctx, checkArgs...); err == nil {
		return nil
	}
	return runIPTables(ctx, args...)
}

func checkRule(ctx context.Context, args ...string) error {
	_, err := outputIPTables(ctx, args...)
	return err
}

func deleteRule(ctx context.Context, args ...string) {
	_ = runIPTables(ctx, args...)
}

func runIPTables(ctx context.Context, args ...string) error {
	return system.RunContext(ctx, iptablesBinary, iptablesArgs(args...)...)
}

func outputIPTables(ctx context.Context, args ...string) (string, error) {
	return outputContext(ctx, iptablesBinary, iptablesArgs(args...)...)
}

func iptablesArgs(args ...string) []string {
	out := make([]string, 0, len(args)+2)
	out = append(out, "-w", iptablesWaitSeconds)
	out = append(out, args...)
	return out
}

func To(ctx context.Context, target string) (Info, error) {
	out, err := outputContext(ctx, "ip", "route", "get", target)
	if err != nil {
		return Info{}, err
	}
	return parseRouteInfo(target, out)
}

func parseRouteInfo(target, out string) (Info, error) {
	fields := strings.Fields(out)
	info := Info{}
	for i, field := range fields {
		if field == "via" && i+1 < len(fields) {
			info.Gateway = fields[i+1]
		}
		if field == "dev" && i+1 < len(fields) {
			info.Device = fields[i+1]
		}
	}
	if info.Gateway == "" && info.Device == "" {
		return Info{}, fmt.Errorf("could not parse route to %s from %q", target, strings.TrimSpace(out))
	}
	return info, nil
}

func routeOutputHasDevice(out, dev string) bool {
	for _, line := range strings.Split(out, "\n") {
		if routeFieldsHaveValue(strings.Fields(line), "dev", dev) {
			return true
		}
	}
	return false
}

func routeFieldsHaveValue(fields []string, key, value string) bool {
	for i, field := range fields {
		if field == key && i+1 < len(fields) && fields[i+1] == value {
			return true
		}
	}
	return false
}

func WriteResolvConf(servers []string) error {
	var b strings.Builder
	for _, server := range servers {
		if server != "" {
			fmt.Fprintf(&b, "nameserver %s\n", server)
		}
	}
	if b.Len() == 0 {
		return nil
	}
	return system.AtomicWrite("/etc/resolv.conf", []byte(b.String()), 0644)
}

func SaveState(state State) error {
	if err := os.MkdirAll(RuntimeDir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return system.AtomicWrite(filepath.Join(RuntimeDir, "state.json"), append(b, '\n'), 0644)
}

func LoadState() (State, error) {
	b, err := os.ReadFile(filepath.Join(RuntimeDir, "state.json"))
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(b, &state); err != nil {
		return State{}, err
	}
	return state, nil
}
