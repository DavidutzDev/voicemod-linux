//go:build !windows

package capture

import (
	"context"
	"errors"
)

var errWindowsOnly = errors.New("capture: WASAPI capture is Windows-only")

// ListDevices is Windows-only.
func ListDevices() ([]string, error) { return nil, errWindowsOnly }

// Run is Windows-only.
func Run(ctx context.Context, cfg Config, send func([]byte)) error { return errWindowsOnly }

// DeviceName is Windows-only.
func DeviceName(substr string) (string, error) { return "", errWindowsOnly }
