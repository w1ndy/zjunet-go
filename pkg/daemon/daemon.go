package daemon

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/w1ndy/zjunet-go/pkg/config"
	"github.com/w1ndy/zjunet-go/pkg/l2tp"
	"github.com/w1ndy/zjunet-go/pkg/route"
	"github.com/w1ndy/zjunet-go/pkg/system"
)

type Options struct {
	ConfigPath string
	Interval   time.Duration
	Wait       time.Duration
	NoRoute    bool
}

type runtimeState struct {
	route       route.State
	connected   bool
	needDial    bool
	lastPPPName string
	connectedAt time.Time
}

func Run(ctx context.Context, opts Options) error {
	if opts.Interval <= 0 {
		opts.Interval = 15 * time.Second
	}
	if opts.Wait <= 0 {
		opts.Wait = 60 * time.Second
	}
	if err := system.RequireRoot(); err != nil {
		return err
	}
	log.Printf("loading config from %s", opts.ConfigPath)
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	deps := []string{"xl2tpd", "xl2tpd-control", "ip", "ping", "pgrep", "pkill", "systemctl"}
	if cfg.ManageNAT {
		deps = append(deps, "iptables", "sysctl")
	}
	if err := system.CheckDeps(deps...); err != nil {
		return err
	}
	log.Printf("writing PPP options to %s", l2tp.PPPPeerPath)
	if err := l2tp.WritePPPOptions(cfg); err != nil {
		return err
	}
	log.Printf("updating xl2tpd config %s with LAC %q", l2tp.XL2TPDPath, cfg.LACName)
	if err := l2tp.UpdateXL2TPDConfig(cfg); err != nil {
		return err
	}
	log.Print("reloading xl2tpd.service")
	serviceCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	if err := l2tp.Restart(serviceCtx); err != nil {
		cancel()
		return fmt.Errorf("reload xl2tpd with generated LAC %q from %s: %w", cfg.LACName, l2tp.XL2TPDPath, err)
	}
	cancel()

	state := route.State{SavedAt: time.Now()}
	if !opts.NoRoute && cfg.ManageRoute {
		log.Printf("capturing current route to %s before VPN routing", config.FirstDNS(cfg))
		routeCtx, routeCancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := route.To(routeCtx, config.FirstDNS(cfg))
		routeCancel()
		if err == nil {
			state.Gateway = info.Gateway
			state.Device = info.Device
			_ = route.SaveState(state)
			log.Printf("saved route context: gateway=%s device=%s", state.Gateway, state.Device)
		} else {
			log.Printf("could not capture route context: %v", err)
		}
	}
	rt := runtimeState{route: state, needDial: true}

	if err := reconcile(ctx, cfg, &rt, opts); err != nil {
		log.Printf("initial reconcile failed: %v", err)
	}

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	defer cleanup(cfg, rt.route, opts)
	log.Printf("daemon started; monitoring every %s", opts.Interval)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := reconcile(ctx, cfg, &rt, opts); err != nil {
				log.Printf("reconcile failed: %v", err)
			}
		}
	}
}

func cleanup(cfg config.Config, state route.State, opts Options) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	log.Print("cleaning up L2TP session before exit")
	if err := l2tp.Disconnect(ctx, cfg.LACName); err != nil {
		log.Printf("disconnect during cleanup failed: %v", err)
	}
	waitPPPDown(ctx, cfg.LACName, 10*time.Second)
	if !opts.NoRoute && cfg.ManageRoute {
		if saved, err := route.LoadState(); err == nil {
			state = saved
		}
		route.RestoreNAT(ctx, cfg, state.PPPDevice)
		if err := route.Restore(ctx, cfg, state); err != nil {
			log.Printf("route restore during cleanup failed: %v", err)
		}
	}
}

func waitPPPDown(ctx context.Context, lac string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dev, _, err := l2tp.CurrentPPP(ctx, lac)
		if err != nil || dev == "" {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	log.Printf("PPP interface for LAC %q still present after cleanup wait", lac)
}

func reconcile(ctx context.Context, cfg config.Config, rt *runtimeState, opts Options) error {
	timeout := 15 * time.Second
	if opts.Wait+15*time.Second > timeout {
		timeout = opts.Wait + 15*time.Second
	}
	serviceCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := l2tp.EnsureStarted(serviceCtx); err != nil {
		return fmt.Errorf("ensure xl2tpd: %w", err)
	}

	ppp, addrs, err := l2tp.CurrentPPP(serviceCtx, cfg.LACName)
	if err != nil {
		return fmt.Errorf("inspect ppp: %w", err)
	}
	if ppp == "" {
		rt.connectedAt = time.Time{}
		if !l2tp.Reachable(serviceCtx, cfg.LNS, 2*time.Second) {
			if rt.connected {
				log.Printf("PPP interface disappeared, but LNS %s is unreachable; waiting", cfg.LNS)
			} else {
				log.Printf("waiting for LNS %s to become reachable", cfg.LNS)
			}
			return nil
		}
		if rt.connected {
			log.Printf("PPP interface %s disappeared; reconnecting", rt.lastPPPName)
		}
		if err := reconnect(serviceCtx, cfg, rt, opts); err != nil {
			return err
		}
		return nil
	}

	now := time.Now()
	if rt.connectedAt.IsZero() || rt.lastPPPName != ppp {
		rt.connectedAt = now
	}
	rt.connected = true
	rt.needDial = false
	rt.lastPPPName = ppp
	rt.route.PPPDevice = ppp
	rt.route.SavedAt = now
	_ = route.SaveState(rt.route)
	if !opts.NoRoute && cfg.ManageRoute && !route.Healthy(serviceCtx, cfg, rt.route, ppp) {
		log.Printf("route state drifted; reinstalling routes for %s", ppp)
		if err := route.Setup(serviceCtx, cfg, rt.route, ppp); err != nil {
			return fmt.Errorf("setup routes: %w", err)
		}
	}
	if cfg.ManageNAT && !route.NATHealthy(serviceCtx, cfg, ppp) {
		log.Printf("NAT state drifted; reinstalling MASQUERADE through %s", ppp)
		if err := route.SetupNAT(serviceCtx, cfg, ppp); err != nil {
			return fmt.Errorf("setup nat: %w", err)
		}
	}
	log.Printf("healthy: %s %v %s", ppp, addrs, interfaceHealth(ppp, rt.connectedAt, now))
	return nil
}

func reconnect(ctx context.Context, cfg config.Config, rt *runtimeState, opts Options) error {
	if !rt.needDial {
		log.Printf("disconnecting stale LAC %q before reconnect", cfg.LACName)
		_ = l2tp.Disconnect(ctx, cfg.LACName)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	log.Printf("connecting LAC %q to LNS %s", cfg.LACName, cfg.LNS)
	if err := l2tp.Connect(ctx, cfg.LACName); err != nil {
		return fmt.Errorf("connect lac: %w", err)
	}
	rt.needDial = false
	log.Printf("waiting up to %s for PPP interface", opts.Wait)
	ppp, addrs, err := l2tp.WaitPPP(ctx, cfg.LACName, opts.Wait)
	if err != nil {
		return err
	}
	rt.connected = true
	rt.needDial = false
	rt.lastPPPName = ppp
	now := time.Now()
	rt.connectedAt = now
	rt.route.PPPDevice = ppp
	rt.route.SavedAt = now
	_ = route.SaveState(rt.route)
	if !opts.NoRoute && cfg.ManageRoute {
		log.Printf("installing VPN routes through %s", ppp)
		if err := route.Setup(ctx, cfg, rt.route, ppp); err != nil {
			return fmt.Errorf("setup routes: %w", err)
		}
	}
	if cfg.ManageNAT {
		log.Printf("installing generic MASQUERADE through %s", ppp)
		if err := route.SetupNAT(ctx, cfg, ppp); err != nil {
			return fmt.Errorf("setup nat: %w", err)
		}
	}
	if cfg.ManageDNS {
		log.Printf("writing DNS servers %v", cfg.DNS)
		if err := route.WriteResolvConf(cfg.DNS); err != nil {
			return fmt.Errorf("write resolv.conf: %w", err)
		}
	}
	log.Printf("connected: %s %v", ppp, addrs)
	return nil
}

func interfaceHealth(ppp string, connectedAt, now time.Time) string {
	uptime := formatUptime(connectedAt, now)
	stats, err := l2tp.ReadInterfaceStats(ppp)
	if err != nil {
		return fmt.Sprintf("uptime=%s rx_bytes=unknown tx_bytes=unknown stats_error=%q", uptime, err.Error())
	}
	return fmt.Sprintf("uptime=%s rx_bytes=%d tx_bytes=%d", uptime, stats.RXBytes, stats.TXBytes)
}

func formatUptime(connectedAt, now time.Time) string {
	if connectedAt.IsZero() {
		return "unknown"
	}
	uptime := now.Sub(connectedAt)
	if uptime < 0 {
		return "0s"
	}
	return uptime.Truncate(time.Second).String()
}
