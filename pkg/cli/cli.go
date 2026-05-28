package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/w1ndy/zjunet-go/pkg/config"
	"github.com/w1ndy/zjunet-go/pkg/daemon"
)

func Run(args []string) error {
	if len(args) == 0 {
		Usage()
		return nil
	}

	switch args[0] {
	case "start":
		return cmdStart(args[1:])
	case "version", "--version", "-v":
		fmt.Println("zjunet-go 0.1.0")
		return nil
	case "help", "-h", "--help":
		Usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func Usage() {
	fmt.Print(`zjunet-go: single-user L2TP client for Zhejiang University network

Usage:
  zjunet-go start [--config /etc/zjunet-go/config.json] [--interval 15s] [--wait 60s] [--no-route]

Commands:
  start    Run continuously, reconnecting and repairing routes as needed
`)
}

func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	interval := fs.Duration("interval", 15*time.Second, "monitoring interval")
	wait := fs.Duration("wait", 60*time.Second, "time to wait for PPP interface")
	noRoute := fs.Bool("no-route", false, "skip route changes")
	configPath := fs.String("config", config.DefaultPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	err := daemon.Run(ctx, daemon.Options{
		ConfigPath: *configPath,
		Interval:   *interval,
		Wait:       *wait,
		NoRoute:    *noRoute,
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
