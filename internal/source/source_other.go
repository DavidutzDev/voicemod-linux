//go:build !linux

package source

import "errors"

// Register is only implemented on Linux. The stub keeps the package buildable
// (and `go build ./...`) on other platforms.
func Register(cfg Config) (Sink, error) {
	return nil, errors.New("source: virtual microphone registration is Linux-only")
}
