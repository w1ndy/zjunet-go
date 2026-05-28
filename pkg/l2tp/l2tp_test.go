package l2tp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlOutputFailed(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{
			name: "empty output",
			out:  "",
			want: false,
		},
		{
			name: "unknown lac",
			out:  "Unknown LAC zjunet-go\n",
			want: true,
		},
		{
			name: "call already in progress",
			out:  "Call already in progress for LAC zjunet-go\n",
			want: true,
		},
		{
			name: "case insensitive",
			out:  "unknown lac zjunet-go\n",
			want: true,
		},
		{
			name: "non failure diagnostic",
			out:  "connecting LAC zjunet-go\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := controlOutputFailed(tt.out); got != tt.want {
				t.Fatalf("controlOutputFailed(%q) = %v, want %v", tt.out, got, tt.want)
			}
		})
	}
}

func TestReadInterfaceStats(t *testing.T) {
	oldSysClassNetPath := sysClassNetPath
	sysClassNetPath = t.TempDir()
	t.Cleanup(func() {
		sysClassNetPath = oldSysClassNetPath
	})

	statsDir := filepath.Join(sysClassNetPath, "ppp0", "statistics")
	if err := os.MkdirAll(statsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statsDir, "rx_bytes"), []byte("12345\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statsDir, "tx_bytes"), []byte("67890\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadInterfaceStats("ppp0")
	if err != nil {
		t.Fatalf("ReadInterfaceStats() error = %v", err)
	}
	if got.RXBytes != 12345 || got.TXBytes != 67890 {
		t.Fatalf("ReadInterfaceStats() = %#v, want rx=12345 tx=67890", got)
	}
}

func TestKnownPPPIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{
			name: "zjunet ten net",
			ip:   "10.5.1.23",
			want: true,
		},
		{
			name: "alternate ten net",
			ip:   "10.0.2.15",
			want: true,
		},
		{
			name: "legacy ppp pool",
			ip:   "172.172.172.8",
			want: true,
		},
		{
			name: "zju public ppp pool",
			ip:   "222.205.4.29",
			want: true,
		},
		{
			name: "unrelated address",
			ip:   "192.168.1.10",
			want: false,
		},
		{
			name: "invalid address",
			ip:   "not-an-address",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := knownPPPIP(tt.ip); got != tt.want {
				t.Fatalf("knownPPPIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestReachableUsesPingProbe(t *testing.T) {
	oldProbeOutputContext := probeOutputContext
	t.Cleanup(func() {
		probeOutputContext = oldProbeOutputContext
	})

	probeOutputContext = func(ctx context.Context, name string, args ...string) (string, error) {
		if name != "ping" {
			t.Fatalf("probe command = %q, want ping", name)
		}
		got := strings.Join(args, " ")
		want := "-n -c 1 -W 2 10.5.1.9"
		if got != want {
			t.Fatalf("probe args = %q, want %q", got, want)
		}
		return "", nil
	}

	if !Reachable(context.Background(), "10.5.1.9", 1500*time.Millisecond) {
		t.Fatal("Reachable() = false, want true")
	}
}

func TestReachableReturnsFalseWhenPingFails(t *testing.T) {
	oldProbeOutputContext := probeOutputContext
	t.Cleanup(func() {
		probeOutputContext = oldProbeOutputContext
	})

	probeOutputContext = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("no reply")
	}

	if Reachable(context.Background(), "10.5.1.9", 2*time.Second) {
		t.Fatal("Reachable() = true, want false")
	}
}

func TestCurrentPPPUsesPPPDLog(t *testing.T) {
	withPPPDLog(t, `pppd 2.5.2 started by root, uid 0
Using interface ppp2
Connect: ppp2 <-->
CHAP authentication succeeded
local  IP address 222.205.4.29
remote IP address 10.0.2.3
`)

	dev, addrs, err := CurrentPPP(context.Background(), "zjunet-go")
	if err != nil {
		t.Fatalf("CurrentPPP() error = %v", err)
	}
	if dev != "ppp2" {
		t.Fatalf("CurrentPPP() dev = %q, want ppp2", dev)
	}
	if len(addrs) != 1 || addrs[0] != "222.205.4.29/32" {
		t.Fatalf("CurrentPPP() addrs = %#v, want 222.205.4.29/32", addrs)
	}
}

func TestCurrentPPPIgnoresTerminatedPPPDLog(t *testing.T) {
	withPPPDLog(t, `pppd 2.5.2 started by root, uid 0
Using interface ppp0
local  IP address 222.205.4.29
Connection terminated.
Modem hangup
Exit.
`)

	dev, addrs, err := CurrentPPP(context.Background(), "zjunet-go")
	if err != nil {
		t.Fatalf("CurrentPPP() error = %v", err)
	}
	if dev != "" || addrs != nil {
		t.Fatalf("CurrentPPP() = %q, %#v; want no active PPP", dev, addrs)
	}
}

func TestCurrentPPPIgnoresUnknownPPPDLogAddress(t *testing.T) {
	withPPPDLog(t, `pppd 2.5.2 started by root, uid 0
Using interface ppp0
local  IP address 192.168.1.10
`)

	dev, addrs, err := CurrentPPP(context.Background(), "zjunet-go")
	if err != nil {
		t.Fatalf("CurrentPPP() error = %v", err)
	}
	if dev != "" || addrs != nil {
		t.Fatalf("CurrentPPP() = %q, %#v; want no active PPP", dev, addrs)
	}
}

func withPPPDLog(t *testing.T, content string) {
	t.Helper()
	oldPPPDLogPath := pppdLogPath
	pppdLogPath = filepath.Join(t.TempDir(), "pppd.log")
	t.Cleanup(func() {
		pppdLogPath = oldPPPDLogPath
	})
	if err := os.WriteFile(pppdLogPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
