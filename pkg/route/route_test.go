package route

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/w1ndy/zjunet-go/pkg/config"
)

func TestIPTablesArgsAddsWait(t *testing.T) {
	got := iptablesArgs("-t", "nat", "-C", "POSTROUTING")
	want := []string{"-w", "5", "-t", "nat", "-C", "POSTROUTING"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("iptablesArgs() = %#v, want %#v", got, want)
	}
}

func TestIPTablesArgsDoesNotMutateInput(t *testing.T) {
	in := []string{"-A", "FORWARD"}
	got := iptablesArgs(in...)
	in[0] = "-C"

	want := []string{"-w", "5", "-A", "FORWARD"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("iptablesArgs() = %#v, want %#v", got, want)
	}
}

func TestParseRouteInfo(t *testing.T) {
	out := "10.10.0.21 via 192.0.2.1 dev eth0 src 192.0.2.100 uid 1000\n    cache\n"

	got, err := parseRouteInfo("10.10.0.21", out)
	if err != nil {
		t.Fatalf("parseRouteInfo() error = %v", err)
	}
	want := Info{Gateway: "192.0.2.1", Device: "eth0"}
	if got != want {
		t.Fatalf("parseRouteInfo() = %#v, want %#v", got, want)
	}
}

func TestRouteOutputHasDeviceUsesExactFieldMatch(t *testing.T) {
	out := "default dev ppp01 scope link\ndefault via 192.0.2.1 dev eth0\n"
	if routeOutputHasDevice(out, "ppp0") {
		t.Fatal("routeOutputHasDevice() matched ppp0 inside ppp01")
	}
	if !routeOutputHasDevice(out, "ppp01") {
		t.Fatal("routeOutputHasDevice() did not match exact ppp01 device")
	}
}

func TestSetupNATCapturesSysctlOutput(t *testing.T) {
	oldOutputContext := outputContext
	t.Cleanup(func() {
		outputContext = oldOutputContext
	})

	var calls []string
	outputContext = func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch name {
		case "sysctl":
			return "net.ipv4.ip_forward = 1\n", nil
		case iptablesBinary:
			return "", nil
		default:
			t.Fatalf("unexpected command %q", name)
		}
		return "", nil
	}

	cfg := config.Default()
	cfg.ManageNAT = true
	if err := SetupNAT(context.Background(), cfg, "ppp1"); err != nil {
		t.Fatalf("SetupNAT() error = %v", err)
	}
	if len(calls) == 0 || calls[0] != "sysctl -w net.ipv4.ip_forward=1" {
		t.Fatalf("first command = %#v, want sysctl probe first", calls)
	}
}

func TestHealthyUsesExactRouteFields(t *testing.T) {
	oldOutputContext := outputContext
	t.Cleanup(func() {
		outputContext = oldOutputContext
	})

	cfg := config.Default()
	state := State{Gateway: "192.0.2.1"}
	outputContext = func(_ context.Context, name string, args ...string) (string, error) {
		if name != "ip" {
			t.Fatalf("unexpected command %q", name)
		}
		switch strings.Join(args, " ") {
		case "route show default":
			return "default dev ppp0 scope link\n", nil
		case "route get " + cfg.LNS:
			return cfg.LNS + " via 192.0.2.1 dev eth0 src 192.0.2.100\n", nil
		case "route get " + config.FirstDNS(cfg):
			return config.FirstDNS(cfg) + " via 192.0.2.1 dev eth0 src 192.0.2.100\n", nil
		default:
			t.Fatalf("unexpected args %q", strings.Join(args, " "))
		}
		return "", nil
	}

	if !Healthy(context.Background(), cfg, state, "ppp0") {
		t.Fatal("Healthy() = false, want true")
	}
}

func TestHealthyRejectsDevicePrefixMatch(t *testing.T) {
	oldOutputContext := outputContext
	t.Cleanup(func() {
		outputContext = oldOutputContext
	})

	outputContext = func(_ context.Context, name string, args ...string) (string, error) {
		if name != "ip" || strings.Join(args, " ") != "route show default" {
			t.Fatalf("unexpected command %s %q", name, strings.Join(args, " "))
		}
		return "default dev ppp01 scope link\n", nil
	}

	if Healthy(context.Background(), config.Default(), State{}, "ppp0") {
		t.Fatal("Healthy() = true for ppp0 when default route uses ppp01")
	}
}

func TestHealthyRejectsGatewayPrefixMatch(t *testing.T) {
	oldOutputContext := outputContext
	t.Cleanup(func() {
		outputContext = oldOutputContext
	})

	cfg := config.Default()
	state := State{Gateway: "192.0.2.1"}
	outputContext = func(_ context.Context, name string, args ...string) (string, error) {
		if name != "ip" {
			t.Fatalf("unexpected command %q", name)
		}
		switch strings.Join(args, " ") {
		case "route show default":
			return "default dev ppp0 scope link\n", nil
		case "route get " + cfg.LNS:
			return cfg.LNS + " via 192.0.2.10 dev eth0 src 192.0.2.100\n", nil
		case "route get " + config.FirstDNS(cfg):
			return config.FirstDNS(cfg) + " via 192.0.2.1 dev eth0 src 192.0.2.100\n", nil
		default:
			t.Fatalf("unexpected args %q", strings.Join(args, " "))
		}
		return "", nil
	}

	if Healthy(context.Background(), cfg, state, "ppp0") {
		t.Fatal("Healthy() = true when route gateway only has a string-prefix match")
	}
}
