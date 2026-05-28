package l2tp

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

type stringAddr string

func (a stringAddr) Network() string { return "ip+net" }

func (a stringAddr) String() string { return string(a) }

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

func TestContainsKnownPPPAddress(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  bool
	}{
		{
			name:  "zjunet ten net",
			addrs: []net.Addr{stringAddr("10.5.1.23/32")},
			want:  true,
		},
		{
			name:  "alternate ten net",
			addrs: []net.Addr{stringAddr("10.0.2.15/32")},
			want:  true,
		},
		{
			name:  "legacy ppp pool",
			addrs: []net.Addr{stringAddr("172.172.172.8/32")},
			want:  true,
		},
		{
			name:  "unrelated address",
			addrs: []net.Addr{stringAddr("192.168.1.10/24")},
			want:  false,
		},
		{
			name:  "invalid address",
			addrs: []net.Addr{stringAddr("not-an-address")},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsKnownPPPAddress(tt.addrs); got != tt.want {
				t.Fatalf("containsKnownPPPAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadyInterfaceRequiresPPPNetwork(t *testing.T) {
	oldInterfaceNetAddrs := interfaceNetAddrs
	t.Cleanup(func() {
		interfaceNetAddrs = oldInterfaceNetAddrs
	})

	iface := net.Interface{Name: "ppp0", Flags: net.FlagUp}
	interfaceNetAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{stringAddr("192.168.1.10/24")}, nil
	}

	if addrs, ok := readyInterface(iface); ok {
		t.Fatalf("readyInterface() = %v, true; want not ready", addrs)
	}

	interfaceNetAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{stringAddr("10.5.1.23/32")}, nil
	}

	addrs, ok := readyInterface(iface)
	if !ok {
		t.Fatal("readyInterface() reported not ready for known PPP address")
	}
	if len(addrs) != 1 || addrs[0] != "10.5.1.23/32" {
		t.Fatalf("readyInterface() addrs = %#v, want 10.5.1.23/32", addrs)
	}
}

func TestParsePIDFile(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		pid  int
		ok   bool
	}{
		{name: "valid", raw: []byte("1234\n"), pid: 1234, ok: true},
		{name: "extra fields ignored", raw: []byte("1234 ppp0\n"), pid: 1234, ok: true},
		{name: "empty", raw: nil},
		{name: "interface name is not a pid", raw: []byte("ppp0\n")},
		{name: "zero", raw: []byte("0\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid, ok := parsePIDFile(tt.raw)
			if pid != tt.pid || ok != tt.ok {
				t.Fatalf("parsePIDFile() = %d, %v; want %d, %v", pid, ok, tt.pid, tt.ok)
			}
		})
	}
}

func TestCurrentPPPUsesLinkPIDToSelectInterface(t *testing.T) {
	oldPIDDirs := pppdPIDDirs
	oldNetInterfaces := netInterfaces
	oldInterfaceNetAddrs := interfaceNetAddrs
	t.Cleanup(func() {
		pppdPIDDirs = oldPIDDirs
		netInterfaces = oldNetInterfaces
		interfaceNetAddrs = oldInterfaceNetAddrs
	})

	pppdPIDDirs = []string{t.TempDir()}
	if err := os.WriteFile(filepath.Join(pppdPIDDirs[0], "ppp-zjunet-go.pid"), []byte("1234\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pppdPIDDirs[0], "ppp0.pid"), []byte("9999\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pppdPIDDirs[0], "ppp1.pid"), []byte("1234\n"), 0644); err != nil {
		t.Fatal(err)
	}

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "ppp0", Flags: net.FlagUp},
			{Name: "ppp1", Flags: net.FlagUp},
		}, nil
	}
	interfaceNetAddrs = func(iface net.Interface) ([]net.Addr, error) {
		return []net.Addr{stringAddr("10.5.1." + iface.Name[len(iface.Name)-1:] + "/32")}, nil
	}

	dev, addrs, err := CurrentPPP(context.Background(), "zjunet-go")
	if err != nil {
		t.Fatalf("CurrentPPP() error = %v", err)
	}
	if dev != "ppp1" {
		t.Fatalf("CurrentPPP() dev = %q, want ppp1", dev)
	}
	if len(addrs) != 1 || addrs[0] != "10.5.1.1/32" {
		t.Fatalf("CurrentPPP() addrs = %#v, want 10.5.1.1/32", addrs)
	}
}

func TestCurrentPPPFallsBackWhenLinkPIDIsMissing(t *testing.T) {
	oldPIDDirs := pppdPIDDirs
	oldNetInterfaces := netInterfaces
	oldInterfaceNetAddrs := interfaceNetAddrs
	t.Cleanup(func() {
		pppdPIDDirs = oldPIDDirs
		netInterfaces = oldNetInterfaces
		interfaceNetAddrs = oldInterfaceNetAddrs
	})

	pppdPIDDirs = []string{t.TempDir()}
	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp},
			{Name: "ppp0", Flags: net.FlagUp},
		}, nil
	}
	interfaceNetAddrs = func(iface net.Interface) ([]net.Addr, error) {
		return []net.Addr{stringAddr("10.5.1.23/32")}, nil
	}

	dev, addrs, err := CurrentPPP(context.Background(), "zjunet-go")
	if err != nil {
		t.Fatalf("CurrentPPP() error = %v", err)
	}
	if dev != "ppp0" {
		t.Fatalf("CurrentPPP() dev = %q, want ppp0", dev)
	}
	if len(addrs) != 1 || addrs[0] != "10.5.1.23/32" {
		t.Fatalf("CurrentPPP() addrs = %#v, want 10.5.1.23/32", addrs)
	}
}

func TestCurrentPPPDoesNotFallbackWhenLinkPIDMismatches(t *testing.T) {
	oldPIDDirs := pppdPIDDirs
	oldNetInterfaces := netInterfaces
	oldInterfaceNetAddrs := interfaceNetAddrs
	t.Cleanup(func() {
		pppdPIDDirs = oldPIDDirs
		netInterfaces = oldNetInterfaces
		interfaceNetAddrs = oldInterfaceNetAddrs
	})

	pppdPIDDirs = []string{t.TempDir()}
	if err := os.WriteFile(filepath.Join(pppdPIDDirs[0], "ppp-zjunet-go.pid"), []byte("1234\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pppdPIDDirs[0], "ppp0.pid"), []byte("9999\n"), 0644); err != nil {
		t.Fatal(err)
	}

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "ppp0", Flags: net.FlagUp}}, nil
	}
	interfaceNetAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{stringAddr("10.5.1.23/32")}, nil
	}

	dev, addrs, err := CurrentPPP(context.Background(), "zjunet-go")
	if err != nil {
		t.Fatalf("CurrentPPP() error = %v", err)
	}
	if dev != "" || addrs != nil {
		t.Fatalf("CurrentPPP() = %q, %#v; want no interface for mismatched PID", dev, addrs)
	}
}
