//go:build linux

package l2tp

import (
	"context"
	"fmt"
	"time"

	systemctl "github.com/taigrr/systemctl"
	"github.com/w1ndy/zjunet-go/pkg/system"
)

func startService(ctx context.Context, unit string) error {
	if err := systemctl.Start(ctx, unit, systemctl.Options{UserMode: false}); err != nil {
		return serviceError("start", unit, err)
	}
	return nil
}

func stopService(ctx context.Context, unit string) error {
	if err := systemctl.Stop(ctx, unit, systemctl.Options{UserMode: false}); err != nil {
		return serviceError("stop", unit, err)
	}
	return nil
}

func serviceActive(ctx context.Context, unit string) (bool, error) {
	return systemctl.IsActive(ctx, unit, systemctl.Options{UserMode: false})
}

func serviceError(action, unit string, err error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, _ := system.CombinedOutputContext(ctx, "systemctl", "status", unit, "--no-pager", "-l")
	journal, _ := system.CombinedOutputContext(ctx, "journalctl", "-u", unit, "-n", "30", "--no-pager")
	return fmt.Errorf("systemctl %s %s failed: %w\n\nsystemctl status:\n%s\njournal:\n%s", action, unit, err, status, journal)
}
