//go:build !linux

package l2tp

import (
	"context"
	"errors"
)

func startService(context.Context, string) error {
	return errors.New("systemctl support is only available on Linux")
}

func stopService(context.Context, string) error {
	return errors.New("systemctl support is only available on Linux")
}

func serviceActive(context.Context, string) (bool, error) {
	return false, errors.New("systemctl support is only available on Linux")
}
