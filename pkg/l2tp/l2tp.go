package l2tp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/w1ndy/zjunet-go/pkg/config"
	"github.com/w1ndy/zjunet-go/pkg/system"
)

const (
	AppName       = "zjunet-go"
	PPPPeerPath   = "/etc/ppp/peers/zjunet-go"
	PPPDLogPath   = "/run/zjunet-go/pppd.log"
	XL2TPDPath    = "/etc/xl2tpd/xl2tpd.conf"
	ControlFile   = "/var/run/xl2tpd/l2tp-control"
	ReadyStatus   = "ready"
	UnknownStatus = "unknown"
)

var (
	pppdLogPath        = PPPDLogPath
	sysClassNetPath    = "/sys/class/net"
	probeOutputContext = system.OutputContext
	knownPPPNetworks   = mustParseCIDRs(
		"10.0.0.0/8",
		"172.172.172.0/24",
		"222.205.0.0/17",
	)
)

type InterfaceStats struct {
	RXBytes uint64
	TXBytes uint64
}

func WritePPPOptions(cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(PPPPeerPath), 0755); err != nil {
		return err
	}
	body := fmt.Sprintf(`noauth
linkname %s
logfile %s
name %s
password %s
mtu %d
`, cfg.LACName, pppdLogPath, cfg.User, cfg.Password, cfg.MTU)
	return system.AtomicWrite(PPPPeerPath, []byte(body), 0600)
}

func UpdateXL2TPDConfig(cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(XL2TPDPath), 0755); err != nil {
		return err
	}
	old, _ := os.ReadFile(XL2TPDPath)
	begin := fmt.Sprintf("; BEGIN %s managed LAC\n", AppName)
	end := fmt.Sprintf("; END %s managed LAC\n", AppName)
	block := fmt.Sprintf(`%s[lac %s]
lns = %s
redial = no
redial timeout = 5
require chap = yes
require authentication = no
ppp debug = no
pppoptfile = %s
require pap = no
autodial = no

%s`, begin, cfg.LACName, cfg.LNS, PPPPeerPath, end)

	content, err := removeManagedBlocks(string(old))
	if err != nil {
		return err
	}
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "\n" + block
	return system.AtomicWrite(XL2TPDPath, []byte(content), 0644)
}

func Start(ctx context.Context) error {
	if controlReady() {
		return nil
	}

	if err := startService(ctx, "xl2tpd.service"); err != nil {
		return fmt.Errorf("start xl2tpd.service: %w", err)
	}
	if err := waitControl(ctx, 10*time.Second); err != nil {
		return fmt.Errorf("xl2tpd.service started but control socket is not ready: %w", err)
	}
	return nil
}

func Restart(ctx context.Context) error {
	if err := stopService(ctx, "xl2tpd.service"); err != nil {
		if running, probeErr := processRunning(ctx); probeErr != nil || running {
			return fmt.Errorf("stop xl2tpd.service before restart: %w", err)
		}
	}
	if err := stopProcesses(ctx); err != nil {
		return fmt.Errorf("stop stray xl2tpd processes before restart: %w", err)
	}
	cleanupRuntimeFiles()
	if err := startService(ctx, "xl2tpd.service"); err != nil {
		return fmt.Errorf("start xl2tpd.service after config update: %w", err)
	}
	if err := waitControl(ctx, 10*time.Second); err != nil {
		return fmt.Errorf("xl2tpd.service restarted but control socket is not ready: %w", err)
	}
	return nil
}

func EnsureStarted(ctx context.Context) error {
	active, err := serviceActive(ctx, "xl2tpd.service")
	if err != nil || !active {
		return Start(ctx)
	}
	if _, err := os.Stat(ControlFile); err != nil {
		return Start(ctx)
	}
	return nil
}

func removeManagedBlocks(content string) (string, error) {
	markers := [][2]string{
		{
			fmt.Sprintf("; BEGIN %s managed LAC\n", AppName),
			fmt.Sprintf("; END %s managed LAC\n", AppName),
		},
		{
			fmt.Sprintf("# BEGIN %s managed LAC\n", AppName),
			fmt.Sprintf("# END %s managed LAC\n", AppName),
		},
	}
	for _, marker := range markers {
		begin, end := marker[0], marker[1]
		for {
			start := strings.Index(content, begin)
			if start < 0 {
				break
			}
			stop := strings.Index(content[start:], end)
			if stop < 0 {
				return "", errors.New("xl2tpd.conf contains an unterminated zjunet-go managed block")
			}
			stop += start + len(end)
			content = content[:start] + content[stop:]
		}
	}
	return strings.TrimRight(content, "\n") + "\n", nil
}

func Connect(ctx context.Context, lac string) error {
	if err := resetPPPDLog(); err != nil {
		return fmt.Errorf("reset pppd log %s: %w", pppdLogPath, err)
	}
	if err := controlLAC(ctx, "connect-lac", lac); err != nil {
		return fmt.Errorf("connect LAC %q: %w", lac, err)
	}
	return nil
}

func Disconnect(ctx context.Context, lac string) error {
	if err := controlLAC(ctx, "disconnect-lac", lac); err != nil {
		return fmt.Errorf("disconnect LAC %q: %w", lac, err)
	}
	return nil
}

func WaitPPP(ctx context.Context, lac string, timeout time.Duration) (string, []string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if dev, addrs, err := CurrentPPP(ctx, lac); err == nil && dev != "" {
			return dev, addrs, nil
		}
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", nil, fmt.Errorf("timed out waiting for PPP interface after %s", timeout)
}

func Reachable(ctx context.Context, host string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := probeOutputContext(probeCtx, "ping", "-n", "-c", "1", "-W", pingWaitSeconds(timeout), host)
	return err == nil
}

func pingWaitSeconds(timeout time.Duration) string {
	seconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

func CurrentPPP(ctx context.Context, lac string) (string, []string, error) {
	_ = ctx
	_ = lac
	raw, err := os.ReadFile(pppdLogPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	dev, ip, ok := parsePPPDLog(raw)
	if !ok {
		return "", nil, nil
	}
	return dev, []string{ip + "/32"}, nil
}

func ReadInterfaceStats(dev string) (InterfaceStats, error) {
	if dev == "" || strings.ContainsRune(dev, rune(os.PathSeparator)) {
		return InterfaceStats{}, fmt.Errorf("invalid interface name %q", dev)
	}
	statsDir := filepath.Join(sysClassNetPath, dev, "statistics")
	rx, err := readUint64File(filepath.Join(statsDir, "rx_bytes"))
	if err != nil {
		return InterfaceStats{}, fmt.Errorf("read rx bytes for %s: %w", dev, err)
	}
	tx, err := readUint64File(filepath.Join(statsDir, "tx_bytes"))
	if err != nil {
		return InterfaceStats{}, fmt.Errorf("read tx bytes for %s: %w", dev, err)
	}
	return InterfaceStats{RXBytes: rx, TXBytes: tx}, nil
}

func Status() string {
	if _, err := os.Stat(ControlFile); err == nil {
		return ReadyStatus
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	active, err := serviceActive(ctx, "xl2tpd.service")
	if err == nil && active {
		return "active"
	}
	return UnknownStatus
}

func controlLAC(ctx context.Context, action, lac string) error {
	out, err := system.CombinedOutputContext(ctx, "xl2tpd-control", action, lac)
	msg := strings.TrimSpace(out)
	if err != nil {
		if msg != "" {
			return fmt.Errorf("xl2tpd-control %s %s failed: %w: %s", action, lac, err, msg)
		}
		return err
	}
	if controlOutputFailed(msg) {
		return fmt.Errorf("xl2tpd-control %s %s reported failure: %s", action, lac, msg)
	}
	return nil
}

func controlOutputFailed(output string) bool {
	msg := strings.ToLower(output)
	failures := []string{
		"unknown lac",
		"call already in progress",
	}
	for _, failure := range failures {
		if strings.Contains(msg, failure) {
			return true
		}
	}
	return false
}

func waitControl(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if controlReady() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("xl2tpd control file %s did not appear", ControlFile)
}

func waitStopped(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := processRunning(ctx)
		if err == nil && !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("xl2tpd process did not stop within %s", timeout)
}

func controlReady() bool {
	_, err := os.Stat(ControlFile)
	return err == nil
}

func processRunning(ctx context.Context) (bool, error) {
	_, err := system.OutputContext(ctx, "pgrep", "-x", "xl2tpd")
	if err == nil {
		return true, nil
	}
	return false, nil
}

func stopProcesses(ctx context.Context) error {
	running, err := processRunning(ctx)
	if err != nil || !running {
		return err
	}
	_ = system.RunContext(ctx, "pkill", "-TERM", "-x", "xl2tpd")
	if err := waitStopped(ctx, 5*time.Second); err == nil {
		return nil
	}
	_ = system.RunContext(ctx, "pkill", "-KILL", "-x", "xl2tpd")
	return waitStopped(ctx, 5*time.Second)
}

func cleanupRuntimeFiles() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if running, _ := processRunning(ctx); !running {
		for _, path := range []string{
			ControlFile,
			"/var/run/xl2tpd.pid",
			"/run/xl2tpd.pid",
			"/var/run/xl2tpd/xl2tpd.pid",
			"/run/xl2tpd/xl2tpd.pid",
		} {
			_ = os.Remove(path)
		}
	}
}

func resetPPPDLog() error {
	if err := os.MkdirAll(filepath.Dir(pppdLogPath), 0755); err != nil {
		return err
	}
	if err := os.Remove(pppdLogPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(pppdLogPath, nil, 0600)
}

func isPPPDeviceName(dev string) bool {
	if !strings.HasPrefix(dev, "ppp") || len(dev) == len("ppp") {
		return false
	}
	for _, r := range dev[len("ppp"):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parsePPPDLog(raw []byte) (string, string, bool) {
	var dev, localIP string
	active := false
	for _, line := range strings.Split(string(raw), "\n") {
		msg := strings.TrimSpace(line)
		if msg == "" {
			continue
		}
		if isPPPDStartLine(msg) {
			dev = ""
			localIP = ""
			active = true
			continue
		}
		if parsedDev, ok := parsePPPDInterfaceLine(msg); ok {
			dev = parsedDev
			active = true
		}
		if parsedIP, ok := parsePPPDLocalIPLine(msg); ok {
			localIP = parsedIP
			active = true
		}
		if isPPPDTerminalLine(msg) {
			dev = ""
			localIP = ""
			active = false
		}
	}
	if !active || dev == "" || localIP == "" || !knownPPPIP(localIP) {
		return "", "", false
	}
	return dev, localIP, true
}

func isPPPDStartLine(line string) bool {
	return strings.Contains(line, "pppd ") && strings.Contains(line, " started ")
}

func parsePPPDInterfaceLine(line string) (string, bool) {
	const marker = "Using interface "
	idx := strings.Index(line, marker)
	if idx < 0 {
		return "", false
	}
	fields := strings.Fields(line[idx+len(marker):])
	if len(fields) == 0 || !isPPPDeviceName(fields[0]) {
		return "", false
	}
	return fields[0], true
}

func parsePPPDLocalIPLine(line string) (string, bool) {
	fields := strings.Fields(line)
	for i := 0; i+3 < len(fields); i++ {
		if fields[i] == "local" && fields[i+1] == "IP" && fields[i+2] == "address" {
			ip := net.ParseIP(fields[i+3])
			if ip == nil || ip.To4() == nil {
				return "", false
			}
			return fields[i+3], true
		}
	}
	return "", false
}

func isPPPDTerminalLine(line string) bool {
	terminalMarkers := []string{
		"Terminating on signal",
		"Connection terminated",
		"Modem hangup",
		"Exit.",
		"LCP terminated",
	}
	for _, marker := range terminalMarkers {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func knownPPPIP(raw string) bool {
	ip := net.ParseIP(raw)
	if ip == nil {
		return false
	}
	for _, network := range knownPPPNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}

func readUint64File(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
}
